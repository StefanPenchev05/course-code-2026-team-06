package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	outboxpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/postgres"
	fanout "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/redis"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type realtimeConfig struct{ namespace, monitorAddress string }

func realtimeSettings() (realtimeConfig, error) {
	c := realtimeConfig{os.Getenv("REALTIME_NAMESPACE"), os.Getenv("REALTIME_MONITOR_ADDR")}
	if err := fanout.ValidateNamespace(c.namespace); err != nil {
		return c, err
	}
	if c.monitorAddress != "" {
		host, port, err := net.SplitHostPort(c.monitorAddress)
		n, parseErr := strconv.Atoi(port)
		if err != nil || parseErr != nil || n < 1 || n > 65535 || !net.ParseIP(host).IsLoopback() {
			return c, errors.New("REALTIME_MONITOR_ADDR must be a literal loopback IP and port 1-65535")
		}
	}
	return c, nil
}
func monitorHandler(stats *observability.Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		s := stats.Snapshot()
		w.Header().Set("Cache-Control", "no-store")
		if !s.Postgres || !s.Redis || !s.OutboxReady || !s.Subscribed || time.Since(s.SampledAt) > 15*time.Second {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(stats.Snapshot())
	})
	return mux
}

// Only loopback is accepted. Never mount this handler on the public API mux.
func startRealtimeMonitor(ctx context.Context, address string, pool *pgxpool.Pool, client *redis.Client, store *outboxpg.Outbox, stats *observability.Metrics, logger *slog.Logger) (func(), error) {
	var server *http.Server
	var serving chan struct{}
	if address != "" {
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, errors.New("could not bind realtime loopback monitor")
		}
		server = &http.Server{Handler: monitorHandler(stats), ReadHeaderTimeout: time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 4096}
		serving = make(chan struct{})
		go func() {
			defer close(serving)
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("Realtime monitoring listener stopped")
			}
		}()
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer stats.LogFailures(logger)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			op, stop := context.WithTimeout(run, time.Second)
			pgOK := pool.Ping(op) == nil
			stop()
			op, stop = context.WithTimeout(run, time.Second)
			redisOK := client.Ping(op).Err() == nil
			stop()
			op, stop = context.WithTimeout(run, 3*time.Second)
			outbox, err := store.Health(op)
			stop()
			stats.Health(pgOK, redisOK, err == nil, outbox)
			stats.LogFailures(logger)
			select {
			case <-run.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		if server != nil {
			_ = server.Close()
			<-serving
		}
		<-done
	}, nil
}
