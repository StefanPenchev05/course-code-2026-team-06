package integration_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/realtime"
	workers "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/worker"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	outboxpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/postgres"
	fanout "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/redis"
	realtimehttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Reuses the real booking fixture without colliding with the rest of the suite.
// Only the randomly named database CREATED by this helper is removed.
func isolatedRealtimeDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("TUTTI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("run disposable integration runner")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	e, _ := app.NewEvent(app.AccountAccessChanged, "", time.Now())
	name := "tutti_rt_" + strings.ReplaceAll(e.ID, "-", "")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		sql, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Fatal(name, err)
		}
	}
	conn.Close(ctx)
	t.Setenv("TUTTI_TEST_DATABASE_URL", u.String())
}

func TestRealtimeSchedulingOutbox(t *testing.T) {
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t)
	// Remove only initial synthetic setup invalidations in this isolated DB.
	f.exec(t, "DELETE FROM realtime_events")
	store := outboxpg.New(f.db)
	t.Run("rollback visibility coalescing and immutable identity", func(t *testing.T) {
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		for _, day := range []string{"2027-01-12", "2027-01-13"} {
			if _, err := tx.Exec(f.ctx, `INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2::date+time '09:00',$2::date+time '10:00','ONLINE')`, fixture(bookingTutor), day); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := tx.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events").Scan(&count); err != nil || count != 1 {
			t.Fatal("not coalesced", count, err)
		}
		if _, ok, err := store.Claim(f.ctx, 30*time.Second); err != nil || ok {
			t.Fatal("uncommitted event claimable", err)
		}
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		f.count(t, 0, "SELECT count(*) FROM realtime_events")
	})
	t.Run("concurrent claims reclaim and stale-token fencing", func(t *testing.T) {
		for n := 0; n < 12; n++ {
			f.exec(t, "SELECT enqueue_realtime_event('availability.changed',$1)", fixture(bookingTutor))
		}
		ids := make(chan string, 12)
		errs := make(chan error, 12)
		var tasks sync.WaitGroup
		for n := 0; n < 12; n++ {
			tasks.Add(1)
			go func() {
				defer tasks.Done()
				c, ok, err := store.Claim(f.ctx, time.Second)
				if err != nil || !ok {
					errs <- errors.New("claim unavailable")
					return
				}
				ids <- c.ID
				if err := store.Dispatched(f.ctx, c); err != nil {
					errs <- err
				}
			}()
		}
		tasks.Wait()
		close(ids)
		close(errs)
		for err := range errs {
			t.Error(err)
		}
		seen := map[string]bool{}
		for id := range ids {
			if seen[id] {
				t.Fatal("double claim")
			}
			seen[id] = true
		}
		if len(seen) != 12 {
			t.Fatal("lost claim")
		}
		f.exec(t, "SELECT enqueue_realtime_event('availability.changed',$1)", fixture(bookingTutor))
		old, ok, err := store.Claim(f.ctx, time.Second)
		if err != nil || !ok {
			t.Fatal(err)
		}
		f.exec(t, "UPDATE realtime_events SET lease_until=statement_timestamp()-interval '1 second' WHERE id=$1", old.ID)
		fresh, ok, err := store.Claim(f.ctx, time.Second)
		if err != nil || !ok || fresh.ID != old.ID || fresh.Token == old.Token {
			t.Fatal("lease not recovered", err)
		}
		if err := store.Dispatched(f.ctx, old); !errors.Is(err, app.ErrClaimLost) {
			t.Fatal("stale ack accepted", err)
		}
		if err := store.Retry(f.ctx, old, time.Second); !errors.Is(err, app.ErrClaimLost) {
			t.Fatal("stale retry accepted", err)
		}
		if err := store.Dispatched(f.ctx, fresh); err != nil {
			t.Fatal(err)
		}
	})

	tutors := tutoring.New(tutoringpg.New(f.db, tutoringpg.Dependencies{}), catalog.New(catalogpg.New(f.db)), nil)
	queries := schedulingpg.NewBooking(f.db, schedulingpg.BookingDependencies{})
	watches := fanout.NewWatches(f.redis)
	routing := app.NewRouting(queries, tutors, watches, domainRouting(f))
	mux := f.a.handler.(*http.ServeMux)
	realtimehttp.RegisterWatches(mux, f.auth, routing)
	schedulinghttp.New(scheduling.New(schedulingpg.New(f.db, identitypg.LockStudent), tutors, func() time.Time { return time.UnixMicro(f.clock.Load()) }), slog.Default()).RegisterRoutes(mux, f.auth)
	var hubs []*realtime.Hub
	var servers []*httptest.Server
	bus := fanout.New(f.redis)
	run, cancelRun := context.WithCancel(f.ctx)
	var tasks sync.WaitGroup
	t.Cleanup(func() {
		cancelRun()
		tasks.Wait()
		for _, h := range hubs {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if err := h.Shutdown(ctx); err != nil {
				t.Error(err)
			}
			cancel()
		}
		for _, s := range servers {
			s.Close()
		}
	})
	for n := 0; n < 2; n++ {
		cfg := realtime.DefaultConfig()
		cfg.AuthInterval = 50 * time.Millisecond
		h, err := realtime.NewHub(cfg)
		if err != nil {
			t.Fatal(err)
		}
		hubs = append(hubs, h)
		root := http.NewServeMux()
		root.Handle("/ws", realtime.NewHandler(h, f.auth, origin))
		root.Handle("/", mux)
		servers = append(servers, httptest.NewServer(root))
		sub, err := bus.Subscribe(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		receiver := app.NewReceiver(store, routing, h)
		tasks.Add(1)
		go func() { defer tasks.Done(); sub.Run(run, receiver, h.Resync, func() {}) }()
	}
	connect := func(apiIndex, actor int) (*websocket.Conn, realtimeReader) {
		t.Helper()
		cookie := f.cookie[actor]
		c, _, err := websocket.Dial(f.ctx, servers[apiIndex].URL+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.CloseNow() })
		return c, realtimeRead(c)
	}
	student, studentEvents := connect(0, bookingStudent)
	defer student.CloseNow()
	tutor, tutorEvents := connect(1, bookingTutor)
	defer tutor.CloseNow()
	viewer, viewerEvents := connect(1, bookingOtherStudent)
	defer viewer.CloseNow()
	coord, coordEvents := connect(0, bookingCoordinator)
	defer coord.CloseNow()
	wrong, wrongEvents := connect(1, bookingHomeCoordinator)
	defer wrong.CloseNow()
	f.a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingOtherStudent])
	f.a.call(t, 403, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingPending])
	f.a.call(t, 400, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor), "user_id": fixture(bookingStudent)}, f.cookie[bookingOtherStudent])
	dispatch := app.NewDispatcher(store, bus)
	drain := func() {
		t.Helper()
		for n := 0; n < 20; n++ {
			count, err := dispatch.RunBatch(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if count == 0 {
				return
			}
		}
		t.Fatal("outbox did not drain")
	}
	wait := func(s realtimeReader, kind app.Type, entity string) app.Event {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case e := <-s.events:
				if e.Type == kind && e.EntityID == entity {
					return e
				}
			case <-s.done:
				t.Fatal("socket closed unexpectedly")
			case <-timer.C:
				t.Fatalf("missing %s %s", kind, entity)
			}
		}
	}
	bookingID := ""
	t.Run("committed request reaches both APIs with public watcher isolation", func(t *testing.T) {
		bookingID = f.request(t, 201, bookingStudent, 24*time.Hour, bookingOffer, bookingSlot)
		drain()
		e := wait(studentEvents, app.BookingChanged, bookingID)
		wait(tutorEvents, app.BookingChanged, bookingID)
		wait(coordEvents, app.BookingChanged, bookingID)
		wait(viewerEvents, app.AvailabilityChanged, fixture(bookingTutor))
		if err := bus.Publish(f.ctx, e.ID); err != nil {
			t.Fatal(err)
		}
		select {
		case <-studentEvents.events:
			t.Fatal("duplicate event redelivered locally")
		case <-wrongEvents.events:
			t.Fatal("home faculty leaked tutor-faculty event")
		case leaked := <-viewerEvents.events:
			if leaked.Type == app.BookingChanged {
				t.Fatal("watcher received private booking ID")
			}
		case <-time.After(100 * time.Millisecond):
		}
	})
	t.Run("accept cancel and reservation release", func(t *testing.T) {
		f.session(t, 200, "POST", tutorRequestsPath+bookingID+"/accept", map[string]string{"meeting_link": "https://meet.example/secret"}, bookingTutor)
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
		wait(tutorEvents, app.BookingChanged, bookingID)
		f.session(t, 200, "POST", "/sessions/"+bookingID+"/cancel", map[string]string{"reason": "test cancellation"}, bookingStudent)
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
		wait(viewerEvents, app.AvailabilityChanged, fixture(bookingTutor))
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", bookingID)
	})
	t.Run("failed business transaction and failed outbox insert roll back together", func(t *testing.T) {
		for _, table := range []string{"notifications", "realtime_events"} {
			var before int
			_ = f.db.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events").Scan(&before)
			f.exec(t, "CREATE FUNCTION rt_force_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test rollback'; END $$")
			f.exec(t, "CREATE TRIGGER rt_force_failure BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION rt_force_failure()")
			f.request(t, 500, bookingStudent, 30*time.Hour, bookingOffer, bookingSlot)
			f.exec(t, "DROP TRIGGER rt_force_failure ON "+table)
			f.exec(t, "DROP FUNCTION rt_force_failure()")
			f.count(t, before, "SELECT count(*) FROM realtime_events")
		}
	})
	t.Run("real Redis failure retries the stable event without affecting booking", func(t *testing.T) {
		bookingID = f.request(t, 201, bookingStudent, 32*time.Hour, bookingOffer, bookingSlot)
		opts, _ := redis.ParseURL(os.Getenv("TUTTI_TEST_REDIS_URL"))
		broken := redis.NewClient(opts)
		_ = broken.Close()
		bad := app.NewDispatcher(store, fanout.New(broken))
		if _, err := bad.RunBatch(f.ctx); err == nil {
			t.Fatal("failed Redis publish acknowledged")
		}
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE last_error='fanout_failed' AND dispatched_at IS NULL")
		f.exec(t, "UPDATE realtime_events SET next_attempt_at=statement_timestamp() WHERE dispatched_at IS NULL")
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
		f.session(t, 200, "POST", tutorRequestsPath+bookingID+"/decline", map[string]string{"reason": "unavailable"}, bookingTutor)
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
	})
	t.Run("separate expiration process produces committed events", func(t *testing.T) {
		bookingID = f.request(t, 201, bookingStudent, 50*time.Hour, bookingOffer, bookingSlot)
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
		child := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestRealtimeExpirationProcess$")
		child.Env = append(os.Environ(), "TUTTI_REALTIME_EXPIRATION_CHILD=1", "TUTTI_REALTIME_EXPIRATION_AT="+f.start.Add(49*time.Hour).Format(time.RFC3339))
		if out, err := child.CombinedOutput(); err != nil {
			t.Fatalf("expiration child failed: %s %v", out, err)
		}
		drain()
		wait(studentEvents, app.BookingChanged, bookingID)
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='EXPIRED'", bookingID)
	})
	t.Run("concurrent booking loser emits nothing", func(t *testing.T) {
		codes := make(chan int, 2)
		var writers sync.WaitGroup
		for _, actor := range []int{bookingStudent, bookingOtherStudent} {
			writers.Add(1)
			go func(actor int) {
				defer writers.Done()
				codes <- f.a.request("POST", "/sessions", origin, f.payload(60*time.Hour, bookingOffer, bookingSlot), f.cookie[actor]).Code
			}(actor)
		}
		writers.Wait()
		close(codes)
		success, conflict := 0, 0
		for code := range codes {
			if code == 201 {
				success++
			} else if code == 409 {
				conflict++
			} else {
				t.Fatal(code)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatal("booking race incorrect")
		}
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND event_type='booking.changed'")
		if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM tutoring_sessions WHERE status='REQUESTED' AND starts_at=$1", f.start.Add(60*time.Hour)).Scan(&bookingID); err != nil {
			t.Fatal(err)
		}
		drain()
		wait(tutorEvents, app.BookingChanged, bookingID)
		f.session(t, 200, "POST", tutorRequestsPath+bookingID+"/decline", map[string]string{"reason": "declined"}, bookingTutor)
		drain()
	})
	t.Run("student role of a tutor also invalidates their own free windows", func(t *testing.T) {
		bookingID = f.request(t, 201, bookingTutor, 70*time.Hour, bookingOtherOffer, bookingOtherSlot)
		f.count(t, 2, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND event_type='availability.changed'")
		drain()
		wait(tutorEvents, app.BookingChanged, bookingID)
		wait(viewerEvents, app.AvailabilityChanged, fixture(bookingTutor))
		f.session(t, 200, "POST", "/sessions/"+bookingID+"/cancel", map[string]string{"reason": "cancel"}, bookingTutor)
		drain()
	})
	t.Run("completion and confirmed attendance correction", func(t *testing.T) {
		f.setTime(0)
		defer f.setTime(0)
		id := f.request(t, 201, bookingStudent, 90*time.Hour, bookingOffer, bookingSlot)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
		drain()
		f.setTime(91 * time.Hour)
		f.session(t, 200, "POST", "/tutors/me/sessions/"+id+"/complete", nil, bookingTutor)
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND event_type='booking.changed'")
		drain()
		wait(studentEvents, app.BookingChanged, id)
		report := f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", map[string]string{"description": "Attendance allegation"}, bookingStudent)["id"].(string)
		f.session(t, 200, "POST", "/coordinator/attendance-reports/"+report+"/confirm", map[string]string{"resolution_note": "Evidence reviewed"}, bookingCoordinator)
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND event_type='booking.changed'")
		drain()
		wait(tutorEvents, app.BookingChanged, id)
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='NO_SHOW'", id)
	})
	t.Run("recurrence edits deletion exceptions and one-off windows", func(t *testing.T) {
		f.exec(t, "INSERT INTO semesters(id,name,starts_at,ends_at) VALUES($1,'Realtime calendar','2027-01-11T00:00Z','2027-02-01T00:00Z')", fixture(87001))
		input := map[string]any{"semester_id": fixture(87001), "iso_weekday": 1, "local_start_time": "09:00", "local_end_time": "10:00", "time_zone": "Europe/Luxembourg", "allowed_mode": "ONLINE"}
		res := fields(t, f.a.call(t, 201, "POST", "/tutors/me/availability/rules", input, f.cookie[bookingTutor]))
		rule := res["data"].(map[string]any)["id"].(string)
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL")
		drain()
		wait(viewerEvents, app.AvailabilityChanged, fixture(bookingTutor))
		input["local_end_time"] = "11:00"
		delete(input, "semester_id")
		f.a.call(t, 200, "PATCH", "/tutors/me/availability/rules/"+rule, input, f.cookie[bookingTutor])
		drain()
		var slot string
		if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 ORDER BY starts_at LIMIT 1", rule).Scan(&slot); err != nil {
			t.Fatal(err)
		}
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/slots/"+slot, nil, f.cookie[bookingTutor])
		drain()
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/rules/"+rule+"/exceptions/2027-01-11", nil, f.cookie[bookingTutor])
		drain()
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/rules/"+rule, nil, f.cookie[bookingTutor])
		drain()
		res = fields(t, f.a.call(t, 201, "POST", "/tutors/me/availability/slots", map[string]string{"starts_at": "2027-01-12T09:00:00Z", "ends_at": "2027-01-12T10:00:00Z", "allowed_mode": "ONLINE"}, f.cookie[bookingTutor]))
		drain()
		slot = res["data"].(map[string]any)["id"].(string)
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/slots/"+slot, nil, f.cookie[bookingTutor])
		drain()
	})
	t.Run("reconnect refetch recovers offline committed changes", func(t *testing.T) {
		student.CloseNow()
		f.setTime(0)
		bookingID = f.request(t, 201, bookingStudent, 80*time.Hour, bookingOffer, bookingSlot)
		drain()
		fresh, events := connect(1, bookingStudent)
		defer fresh.CloseNow()
		state := f.session(t, 200, "GET", "/sessions/"+bookingID, nil, bookingStudent)
		if state["status"] != "REQUESTED" {
			t.Fatal("refetch stale")
		}
		f.session(t, 200, "POST", tutorRequestsPath+bookingID+"/decline", map[string]string{"reason": "declined"}, bookingTutor)
		drain()
		wait(events, app.BookingChanged, bookingID)
	})
	t.Run("watch bounds expiry and current visibility", func(t *testing.T) {
		for n := 0; n < 32; n++ {
			if _, err := watches.Register(f.ctx, fixture(87900), fixture(88000+n)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := watches.Register(f.ctx, fixture(87900), fixture(88999)); !errors.Is(err, app.ErrWatch) {
			t.Fatal("per-user watch limit missing")
		}
		prefix := "tutti:{realtime}:watches:"
		// Advance this synthetic lease's score rather than waiting ten minutes.
		if err := f.redis.ZAdd(f.ctx, prefix+"user:"+fixture(bookingOtherStudent), redis.Z{Score: 1, Member: fixture(bookingTutor)}).Err(); err != nil {
			t.Fatal(err)
		}
		if ok, err := watches.Contains(f.ctx, fixture(bookingOtherStudent), fixture(bookingTutor)); err != nil || ok {
			t.Fatal("expired lease accepted", err)
		}
		f.a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingOtherStudent])
		f.exec(t, "UPDATE tutor_offerings SET is_active=false,deactivated_at=now(),deactivation_reason='Test withdrawal' WHERE id=$1", fixture(bookingOffer))
		f.a.call(t, 403, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingOtherStudent])
	})
	t.Run("subscriber loss forces resync instead of silent stale sockets", func(t *testing.T) {
		// Only the disposable runner's Redis is targeted; this command is never
		// used by application code or against a development/shared instance.
		if err := f.redis.Do(f.ctx, "CLIENT", "KILL", "TYPE", "pubsub").Err(); err != nil {
			t.Fatal(err)
		}
		realtimeClosed(t, tutorEvents)
		realtimeClosed(t, viewerEvents)
		deadline := time.Now().Add(3 * time.Second)
		for {
			counts, err := f.redis.PubSubNumSub(f.ctx, fanout.Channel).Result()
			if err != nil {
				t.Fatal(err)
			}
			if counts[fanout.Channel] == 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("fanout did not resubscribe")
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
}

// Runs in a separate executable process against the isolated migrated database.
// Its operation is exactly the Expiration.RunBatch used by cmd/worker; only the
// trusted clock is deterministic, without starting SMTP for unrelated tests.
func TestRealtimeExpirationProcess(t *testing.T) {
	if os.Getenv("TUTTI_REALTIME_EXPIRATION_CHILD") != "1" {
		t.Skip("subprocess helper")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, os.Getenv("TUTTI_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	at, err := time.Parse(time.RFC3339, os.Getenv("TUTTI_REALTIME_EXPIRATION_AT"))
	if err != nil {
		t.Fatal(err)
	}
	service := scheduling.NewBooking(schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{}), time.Now)
	op := workers.Expiration{Service: service, BatchSize: 50, Now: func() time.Time { return at }}
	if n, err := op.RunBatch(ctx); err != nil || n != 1 {
		t.Fatal("expiration failed", n, err)
	}
}
