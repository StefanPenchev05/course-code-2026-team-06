package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/realtime"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/worker"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	outboxpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/postgres"
	fanoutredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/redis"
	realtimehttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/interfaces/http"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Only the exact WebSocket path bypasses TimeoutHandler, which does not expose
// http.Hijacker. All other paths, including unknown ones, keep REST timeouts.
func apiHandler(rest, socket http.Handler, timeout time.Duration) http.Handler {
	bounded := http.TimeoutHandler(rest, timeout, `{"error":"request timed out"}`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" && r.URL.RawPath == "" {
			socket.ServeHTTP(w, r)
			return
		}
		bounded.ServeHTTP(w, r)
	})
}

// Every API participates in dispatch and has its own fanout subscription. The
// separate expiration worker needs no Redis client: its PostgreSQL writes are
// captured transactionally, including while all API processes are stopped.
func startRealtime(ctx context.Context, pool *pgxpool.Pool, client *redis.Client, hub *realtime.Hub,
	visibility app.Visibility, mux *http.ServeMux, identity *identityhttp.Handler, logger *slog.Logger) (func(), error) {
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	store := outboxpg.New(pool)
	if err := store.Check(startup); err != nil {
		return nil, errors.New("realtime outbox unavailable; apply migration 17 before API startup")
	}
	bus := fanoutredis.New(client)
	subscription, err := bus.Subscribe(startup)
	if err != nil {
		return nil, errors.New("could not subscribe to realtime fanout")
	}
	routing := app.NewRouting(schedulingpg.NewBooking(pool, schedulingpg.BookingDependencies{}), visibility, fanoutredis.NewWatches(client))
	realtimehttp.RegisterWatches(mux, identity, routing)
	receiver := app.NewReceiver(store, routing, hub)
	dispatcher := app.NewDispatcher(store, bus)
	run, cancelRun := context.WithCancel(ctx)
	var tasks sync.WaitGroup
	tasks.Add(2)
	go func() {
		defer tasks.Done()
		subscription.Run(run, receiver, hub.Resync, func() {
			logger.Warn("Realtime fanout interrupted; clients must resynchronize")
		})
	}()
	go func() {
		defer tasks.Done()
		worker.Poll(run, time.Second, func(ctx context.Context) {
			if _, err := dispatcher.RunBatch(ctx); err != nil {
				reportWorkerError(ctx, logger, "realtime_dispatch", err)
			}
		})
	}()
	return func() { cancelRun(); _ = subscription.Close(); tasks.Wait() }, nil
}
