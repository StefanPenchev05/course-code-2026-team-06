package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	email "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/application"
	emailpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/infrastructure/smtp"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/worker"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
)

type workerConfig struct {
	expirationInterval, emailInterval time.Duration
	expirationBatch                   int
	email                             email.Config
	smtp                              smtp.Config
}

func workerSettings() (workerConfig, error) {
	var c workerConfig
	for _, item := range []struct {
		key, fallback string
		dst           *time.Duration
	}{
		{"WORKER_EXPIRATION_INTERVAL", "10s", &c.expirationInterval},
		{"WORKER_EMAIL_INTERVAL", "5s", &c.emailInterval},
		{"WORKER_EMAIL_LEASE", "90s", &c.email.Lease},
		{"WORKER_EMAIL_RETRY_BASE", "30s", &c.email.RetryBase},
		{"WORKER_EMAIL_RETRY_MAX", "30m", &c.email.RetryMax},
		{"SMTP_SEND_TIMEOUT", "20s", &c.email.SendTimeout},
		{"SMTP_CONNECT_TIMEOUT", "5s", &c.smtp.ConnectTimeout},
	} {
		value, err := duration(item.key, item.fallback)
		if err != nil {
			return c, err
		}
		*item.dst = value
	}
	for _, item := range []struct {
		key, fallback string
		max           int
		dst           *int
	}{
		{"WORKER_EXPIRATION_BATCH_SIZE", "50", scheduling.MaxExpirationBatchSize, &c.expirationBatch},
		{"WORKER_EMAIL_BATCH_SIZE", "20", 100, &c.email.BatchSize},
		{"WORKER_EMAIL_MAX_ATTEMPTS", "5", 20, &c.email.MaxAttempts},
		{"SMTP_PORT", "587", 65535, &c.smtp.Port},
	} {
		value, err := number(item.key, item.fallback, 16)
		if err != nil || value < 1 || value > uint64(item.max) {
			return c, fmt.Errorf("%s must be between 1 and %d", item.key, item.max)
		}
		*item.dst = int(value)
	}
	c.smtp.Host, c.smtp.From = os.Getenv("SMTP_HOST"), os.Getenv("SMTP_FROM")
	c.smtp.Username, c.smtp.Password = os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD")
	c.smtp.TLSMode = env("SMTP_TLS_MODE", "starttls")
	c.smtp.SendTimeout = c.email.SendTimeout
	if err := c.email.Validate(); err != nil {
		return c, errors.New("check SMTP_SEND_TIMEOUT, WORKER_EMAIL_LEASE and WORKER_EMAIL_RETRY settings")
	}
	return c, nil
}

// RunWorker uses a separate process and only connects to PostgreSQL and SMTP.
func RunWorker(ctx context.Context, logger *slog.Logger) error {
	c, err := workerSettings()
	if err != nil {
		return err
	}
	transport, err := smtp.New(c.smtp)
	if err != nil {
		return err
	}
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := openDatabase(startup)
	if err != nil {
		return err
	}
	defer pool.Close()
	store := emailpg.New(pool)
	if err := store.Check(startup); err != nil {
		return errors.New("notification delivery schema unavailable; apply PostgreSQL migrations and check the worker database role")
	}
	delivery, err := email.New(store, transport, c.email)
	if err != nil {
		return err
	}
	expiration := worker.Expiration{Service: newBookingService(pool), BatchSize: c.expirationBatch, Now: time.Now}

	logger.Info("Tutti worker started", "expiration_interval", c.expirationInterval.String(), "email_interval", c.emailInterval.String())
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		worker.Poll(ctx, c.expirationInterval, func(ctx context.Context) {
			n, err := expiration.RunBatch(ctx)
			if err != nil {
				reportWorkerError(ctx, logger, "expiration", err)
				return
			}
			if n > 0 {
				logger.Info("requests expired", "count", n)
			}
		})
	}()
	go func() {
		defer wg.Done()
		worker.Poll(ctx, c.emailInterval, func(ctx context.Context) {
			stats, err := delivery.RunBatch(ctx)
			if stats != (email.Stats{}) {
				logger.Info("email batch processed", "sent", stats.Sent, "retried", stats.Retried, "failed", stats.Failed, "lost_claims", stats.Lost)
			}
			if err != nil {
				reportWorkerError(ctx, logger, "email", err)
			}
		})
	}()
	wg.Wait()
	logger.Info("Tutti worker stopped")
	return nil
}

func reportWorkerError(ctx context.Context, logger *slog.Logger, job string, err error) {
	if ctx.Err() != nil {
		return
	}
	// SQL error details may include recipient data. Only log error classes.
	kind := "operation_error"
	if errors.Is(err, context.DeadlineExceeded) {
		kind = "timeout"
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		kind = pgErr.Code
	}
	logger.Error("background iteration failed", "job", job, "category", kind)
}
