// Package bootstrap wires the Tutti API's infrastructure and modules.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func duration(key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(env(key, fallback))
	if err != nil || value < time.Second || value > 30*24*time.Hour || value%time.Second != 0 {
		return 0, fmt.Errorf("%s must be a whole-second duration between 1s and 720h", key)
	}
	return value, nil
}

func number(key, fallback string, bits int) (uint64, error) {
	value, err := strconv.ParseUint(env(key, fallback), 10, bits)
	if err != nil {
		return 0, fmt.Errorf("%s must be an unsigned integer", key)
	}
	return value, nil
}

func browserOrigin(value string) (bool, error) {
	u, err := url.Parse(value)
	if err == nil && u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" &&
		!u.ForceQuery && u.Fragment == "" && value == u.Scheme+"://"+u.Host {
		if u.Scheme == "https" {
			return true, nil
		}
		host := u.Hostname()
		if u.Scheme == "http" && (host == "localhost" || net.ParseIP(host).IsLoopback()) {
			return false, nil
		}
	}
	return false, fmt.Errorf("PUBLIC_ORIGIN must be an HTTPS origin or loopback HTTP origin without a path")
}

func Run(ctx context.Context, logger *slog.Logger) error {
	origin := env("PUBLIC_ORIGIN", "http://localhost:8080")
	secure, err := browserOrigin(origin)
	if err != nil {
		return err
	}
	sessionTTL, err := duration("SESSION_TTL", "8h")
	if err != nil {
		return err
	}
	activationTTL, err := duration("ACTIVATION_TTL", "24h")
	if err != nil {
		return err
	}
	memory, err := number("ARGON2_MEMORY_KIB", "65536", 32)
	if err != nil {
		return err
	}
	iterations, err := number("ARGON2_ITERATIONS", "3", 32)
	if err != nil {
		return err
	}
	threads, err := number("ARGON2_THREADS", "2", 8)
	if err != nil {
		return err
	}
	concurrency, err := number("ARGON2_CONCURRENCY", "2", 8)
	if err != nil {
		return err
	}
	passwords, err := security.NewPasswords(security.Cost{
		Memory: uint32(memory), Time: uint32(iterations), Threads: uint8(threads),
	}, int(concurrency))
	if err != nil {
		return err
	}
	config := application.Config{
		Domains:    strings.Split(os.Getenv("UNIVERSITY_EMAIL_DOMAINS"), ","),
		SessionTTL: sessionTTL, ActivationTTL: activationTTL,
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(startup, databaseURL)
	if err != nil {
		return errors.New("invalid PostgreSQL connection configuration")
	}
	defer pool.Close()
	if err := pool.Ping(startup); err != nil {
		return errors.New("could not connect to PostgreSQL")
	}
	options, err := redis.ParseURL(env("REDIS_URL", "redis://127.0.0.1:6379/0"))
	if err != nil {
		return errors.New("invalid Redis connection configuration")
	}
	options.ContextTimeoutEnabled = true
	client := redis.NewClient(options)
	defer client.Close()
	if err := client.Ping(startup).Err(); err != nil {
		return errors.New("could not connect to Redis")
	}
	service, err := application.New(startup, postgres.New(pool), sessionredis.New(client), passwords, config)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	identityhttp.New(service, origin, secure, logger).RegisterRoutes(mux)
	server := &http.Server{
		Addr:              env("HTTP_ADDR", "127.0.0.1:8080"),
		Handler:           http.TimeoutHandler(mux, 10*time.Second, `{"error":"request timed out"}`),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	result := make(chan error, 1)
	go func() { result <- server.ListenAndServe() }()
	logger.Info("Tutti API listening", "address", server.Addr)
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
