package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/realtime"
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
	"github.com/redis/go-redis/v9"
)

// Observe completion of real Redis receiver -> routing -> hub queueing. This
// decorator does not choose recipients or replace any production delivery logic.
type publicationProbe struct {
	hub       *realtime.Hub
	processed chan string
}

func (p publicationProbe) Publish(ctx context.Context, event app.Event, users []string, policy app.Authorize) error {
	if err := p.hub.Publish(ctx, event, users, policy); err != nil {
		return err
	}
	p.processed <- event.ID
	return nil
}

type publicationObserver struct {
	hub    *realtime.Hub
	reader realtimeReader
}

type publicationFixture struct {
	*bookingFixture
	store     *outboxpg.Outbox
	bus       *fanout.Fanout
	hubs      []*realtime.Hub
	servers   []*httptest.Server
	processed []chan string
	observers map[int]publicationObserver
}

type publicationWant struct {
	kind       app.Type
	entity     string
	recipients []int
}

func availabilityPublication(tutor int) publicationWant {
	return publicationWant{app.AvailabilityChanged, fixture(tutor), []int{tutor, bookingOtherStudent}}
}

func bookingPublication(id string, student, tutor, coordinator int) publicationWant {
	return publicationWant{app.BookingChanged, id, []int{student, tutor, coordinator}}
}

func newPublicationFixture(t *testing.T) *publicationFixture {
	t.Helper()
	isolatedRealtimeDatabase(t)
	f := &publicationFixture{bookingFixture: newBookingFixture(t), observers: map[int]publicationObserver{}}
	f.store, f.bus = outboxpg.New(f.db), fanout.New(f.redis)
	tutors := tutoring.New(tutoringpg.New(f.db, tutoringpg.Dependencies{}), catalog.New(catalogpg.New(f.db)), nil)
	routing := app.NewRouting(schedulingpg.NewBooking(f.db, schedulingpg.BookingDependencies{}), tutors, fanout.NewWatches(f.redis))
	mux := f.a.handler.(*http.ServeMux)
	now := func() time.Time { return time.UnixMicro(f.clock.Load()) }
	schedulinghttp.New(scheduling.New(schedulingpg.New(f.db, identitypg.LockStudent), tutors, now), slog.Default()).RegisterRoutes(mux, f.auth)
	realtimehttp.RegisterWatches(mux, f.auth, routing)
	policy := coordpg.NewPolicyCancellation(f.db, coordpg.PolicyCancellationDependencies{
		LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, Sessions: schedulingpg.PolicySessions,
	})
	coordhttp.NewPolicyCancellation(coordination.NewPolicyCancellation(policy, now), slog.Default()).RegisterRoutes(mux, f.auth)
	suspension := coordpg.NewSuspension(f.db, coordpg.SuspensionDependencies{
		Accounts: identitypg.SuspensionAccounts, Sessions: schedulingpg.SuspensionSessions,
		Catalog: func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
	})
	coordhttp.NewSuspension(coordination.NewSuspension(suspension, now), slog.Default()).RegisterRoutes(mux, f.auth)
	// Discard only fixture-creation events in this helper's new disposable DB.
	f.exec(t, "DELETE FROM realtime_events")
	run, cancel := context.WithCancel(f.ctx)
	var tasks sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		tasks.Wait()
		for i, hub := range f.hubs {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			if err := hub.Shutdown(ctx); err != nil {
				t.Error(err)
			}
			stop()
			f.servers[i].Close()
		}
	})
	for i := 0; i < 2; i++ {
		cfg := realtime.DefaultConfig()
		cfg.AuthInterval = 50 * time.Millisecond
		hub, err := realtime.NewHub(cfg)
		if err != nil {
			t.Fatal(err)
		}
		root := http.NewServeMux()
		root.Handle("/ws", realtime.NewHandler(hub, f.auth, origin))
		root.Handle("/", mux)
		f.hubs = append(f.hubs, hub)
		f.servers = append(f.servers, httptest.NewServer(root))
		processed := make(chan string, 64)
		f.processed = append(f.processed, processed)
		receiver := app.NewReceiver(f.store, routing, publicationProbe{hub, processed})
		sub, err := f.bus.Subscribe(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		tasks.Add(1)
		go func() { defer tasks.Done(); sub.Run(run, receiver, hub.Resync, func() {}) }()
	}
	for i, actor := range []int{bookingStudent, bookingTutor, bookingOtherTutor, bookingOtherStudent, bookingCoordinator, bookingHomeCoordinator, bookingPending} {
		f.connect(t, i%2, actor)
	}
	for _, tutor := range []int{bookingTutor, bookingOtherTutor} {
		f.a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(tutor)}, f.cookie[bookingOtherStudent])
	}
	f.a.call(t, 403, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingPending])
	return f
}

func (f *publicationFixture) connect(t *testing.T, apiIndex, actor int) {
	t.Helper()
	cookie := f.cookie[actor]
	c, _, err := websocket.Dial(f.ctx, f.servers[apiIndex].URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	f.observers[actor] = publicationObserver{f.hubs[apiIndex], realtimeRead(c)}
}

func (f *publicationFixture) disconnected(t *testing.T, actor int) {
	t.Helper()
	s := f.observers[actor].reader
	realtimeClosed(t, s)
	if len(s.events) != 0 {
		t.Fatal("lost access but received a protected event", actor)
	}
	delete(f.observers, actor)
}

func (f *publicationFixture) pending(t *testing.T) map[string]app.Event {
	t.Helper()
	rows, err := f.db.Query(f.ctx, "SELECT id::text FROM realtime_events WHERE dispatched_at IS NULL ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	events := map[string]app.Event{}
	for _, id := range ids {
		event, err := f.store.Get(f.ctx, id)
		if err != nil || event.Validate() != nil {
			t.Fatal("invalid committed event", event, err)
		}
		events[id] = event
	}
	return events
}

func (f *publicationFixture) expect(t *testing.T, wants ...publicationWant) map[string]app.Event {
	t.Helper()
	events := f.pending(t)
	if len(events) != len(wants) {
		t.Fatalf("committed events=%v; want exactly %v (including coalescing)", events, wants)
	}
	if len(wants) > 0 {
		f.count(t, 1, "SELECT count(DISTINCT producer_xid) FROM realtime_events WHERE dispatched_at IS NULL")
	}
	for _, want := range wants {
		matches := 0
		for _, event := range events {
			if event.Type == want.kind && event.EntityID == want.entity {
				matches++
			}
		}
		if matches != 1 {
			t.Fatalf("want one %s for %s, found %d", want.kind, want.entity, matches)
		}
	}
	n, err := app.NewDispatcher(f.store, f.bus).RunBatch(f.ctx)
	if err != nil || n != len(events) {
		t.Fatalf("dispatch count=%d error=%v, want %d", n, err, len(events))
	}
	for _, processed := range f.processed {
		seen := map[string]bool{}
		for range events {
			select {
			case id := <-processed:
				if _, ok := events[id]; !ok || seen[id] {
					t.Fatal("unexpected or duplicate fanout event", id)
				}
				seen[id] = true
			case <-time.After(3 * time.Second):
				t.Fatal("API subscriber did not process all committed events")
			}
		}
	}
	f.expectSockets(t, events, wants)
	f.count(t, 0, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL")
	return events
}

func (f *publicationFixture) expectSockets(t *testing.T, events map[string]app.Event, wants []publicationWant) {
	t.Helper()
	for actor, observer := range f.observers {
		expected := map[string]app.Event{}
		for _, want := range wants {
			for _, recipient := range want.recipients {
				if actor == recipient {
					for id, event := range events {
						if event.Type == want.kind && event.EntityID == want.entity {
							expected[id] = event
						}
					}
				}
			}
		}
		// Both real subscribers have queued every event. A test-only marker in
		// each socket's FIFO proves all earlier deliveries/policy drops finished;
		// negative assertions need no sleeps and cannot skip unexpected events.
		marker, err := app.NewEvent(app.AccountAccessChanged, "", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := observer.hub.Publish(f.ctx, marker, []string{fixture(actor)}, func(context.Context, identity.Principal, app.Event) (bool, error) { return true, nil }); err != nil {
			t.Fatal(err)
		}
		timer := time.NewTimer(3 * time.Second)
	read:
		for {
			select {
			case event := <-observer.reader.events:
				if event == marker {
					break read
				}
				if want, ok := expected[event.ID]; !ok || want != event {
					t.Fatalf("actor %d received unexpected/duplicate/private event %+v; expected %v", actor, event, expected)
				}
				delete(expected, event.ID)
			case <-observer.reader.done:
				t.Fatal("authorized socket closed", actor)
			case <-timer.C:
				t.Fatal("socket did not reach delivery barrier", actor)
			}
		}
		timer.Stop()
		if len(expected) != 0 {
			t.Fatalf("actor %d missed committed events %v", actor, expected)
		}
	}
}

func TestRealtimeMutationPublication(t *testing.T) {
	f := newPublicationFixture(t)
	availability := availabilityPublication(bookingTutor)
	const own = "/tutors/me/availability"
	var rule, generated, oneOff, reserved string
	f.exec(t, "INSERT INTO semesters(id,name,starts_at,ends_at) VALUES($1,'Publication calendar','2027-01-11T00:00Z','2027-02-01T00:00Z')", fixture(87010))
	t.Run("recurring rule creation coalesces generated slots", func(t *testing.T) {
		body := map[string]any{"semester_id": fixture(87010), "iso_weekday": 1, "local_start_time": "09:00", "local_end_time": "10:00", "time_zone": "Europe/Luxembourg", "allowed_mode": "ONLINE"}
		rule = fields(t, f.a.call(t, 201, "POST", own+"/rules", body, f.cookie[bookingTutor]))["data"].(map[string]any)["id"].(string)
		f.count(t, 3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", rule)
		f.expect(t, availability)
	})
	t.Run("recurring rule update replaces slots with one invalidation", func(t *testing.T) {
		f.a.call(t, 200, "PATCH", own+"/rules/"+rule, map[string]string{"local_end_time": "11:00"}, f.cookie[bookingTutor])
		f.count(t, 3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1 AND ends_at-starts_at=interval '2 hours'", rule)
		f.expect(t, availability)
	})
	t.Run("generated slot deletion creates exception and coalesces", func(t *testing.T) {
		if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 ORDER BY starts_at LIMIT 1", rule).Scan(&generated); err != nil {
			t.Fatal(err)
		}
		f.a.call(t, 204, "DELETE", own+"/slots/"+generated, nil, f.cookie[bookingTutor])
		f.count(t, 0, "SELECT count(*) FROM availability_slots WHERE id=$1", generated)
		f.count(t, 1, "SELECT count(*) FROM availability_exceptions WHERE rule_id=$1 AND local_date='2027-01-11'", rule)
		f.expect(t, availability)
	})
	t.Run("exception restoration coalesces regenerated slot", func(t *testing.T) {
		f.a.call(t, 204, "DELETE", own+"/rules/"+rule+"/exceptions/2027-01-11", nil, f.cookie[bookingTutor])
		f.count(t, 0, "SELECT count(*) FROM availability_exceptions WHERE rule_id=$1", rule)
		f.count(t, 3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", rule)
		f.expect(t, availability)
	})
	t.Run("one-off creation", func(t *testing.T) {
		body := map[string]string{"starts_at": "2027-01-18T10:00:00Z", "ends_at": "2027-01-18T11:00:00Z", "allowed_mode": "ONLINE"}
		oneOff = fields(t, f.a.call(t, 201, "POST", own+"/slots", body, f.cookie[bookingTutor]))["data"].(map[string]any)["id"].(string)
		f.expect(t, availability)
	})
	t.Run("rejected replacement rolls back earlier deletes and captures", func(t *testing.T) {
		// This edit deletes old occurrences before trying to insert overlapping
		// replacements. The SQL exclusion constraint rolls back the entire edit.
		const snapshot = `SELECT jsonb_build_array(to_jsonb(r),(SELECT jsonb_agg(to_jsonb(s) ORDER BY s.id) FROM availability_slots s WHERE s.rule_id=r.id))::text FROM availability_rules r WHERE id=$1`
		var before, after string
		if err := f.db.QueryRow(f.ctx, snapshot, rule).Scan(&before); err != nil {
			t.Fatal(err)
		}
		f.a.call(t, 409, "PATCH", own+"/rules/"+rule, map[string]string{"local_end_time": "12:00"}, f.cookie[bookingTutor])
		if err := f.db.QueryRow(f.ctx, snapshot, rule).Scan(&after); err != nil || after != before {
			t.Fatal("rejected edit changed the rule or its slots", err)
		}
		f.expect(t)
	})
	t.Run("one-off deletion", func(t *testing.T) {
		f.a.call(t, 204, "DELETE", own+"/slots/"+oneOff, nil, f.cookie[bookingTutor])
		f.count(t, 0, "SELECT count(*) FROM availability_slots WHERE id=$1", oneOff)
		f.expect(t, availability)
	})
	t.Run("reservation creation", func(t *testing.T) {
		if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 ORDER BY starts_at LIMIT 1", rule).Scan(&generated); err != nil {
			t.Fatal(err)
		}
		body := f.payload(248*time.Hour, bookingOffer, bookingSlot)
		body["availability_slot_id"] = generated
		reserved = f.session(t, 201, "POST", "/sessions", body, bookingStudent)["id"].(string)
		f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", reserved)
		f.expect(t, bookingPublication(reserved, bookingStudent, bookingTutor, bookingCoordinator), availability)
	})
	t.Run("reserved changes and overlapping booking are rejected without events", func(t *testing.T) {
		f.a.call(t, 409, "DELETE", own+"/slots/"+generated, nil, f.cookie[bookingTutor])
		f.expect(t)
		f.a.call(t, 409, "DELETE", own+"/rules/"+rule, nil, f.cookie[bookingTutor])
		f.expect(t)
		f.a.call(t, 409, "PATCH", own+"/rules/"+rule, map[string]string{"local_end_time": "12:00"}, f.cookie[bookingTutor])
		f.expect(t)
		body := f.payload(248*time.Hour, bookingOffer, bookingSlot)
		body["availability_slot_id"] = generated
		f.a.call(t, 409, "POST", "/sessions", body, f.cookie[bookingOtherStudent])
		f.expect(t)
		f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", reserved)
		f.count(t, 3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", rule)
	})
	t.Run("participant cancellation releases reservations", func(t *testing.T) {
		f.session(t, 200, "POST", "/sessions/"+reserved+"/cancel", testReason, bookingStudent)
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", reserved)
		f.expect(t, bookingPublication(reserved, bookingStudent, bookingTutor, bookingCoordinator), availability)
	})
	t.Run("recurring rule deletion coalesces all slot removals", func(t *testing.T) {
		f.a.call(t, 204, "DELETE", own+"/rules/"+rule, nil, f.cookie[bookingTutor])
		f.count(t, 0, "SELECT count(*) FROM availability_rules WHERE id=$1", rule)
		f.count(t, 0, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", rule)
		f.expect(t, availability)
	})
	t.Run("uncommitted and rolled-back changes cannot be dispatched", func(t *testing.T) {
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		for _, day := range []string{"2027-01-14", "2027-01-15"} {
			if _, err := tx.Exec(f.ctx, `INSERT INTO availability_slots(tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2::date+time '09:00',$2::date+time '10:00','ONLINE')`, fixture(bookingTutor), day); err != nil {
				t.Fatal(err)
			}
		}
		var captured int
		if err := tx.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events WHERE producer_xid=pg_current_xact_id()").Scan(&captured); err != nil || captured != 1 {
			t.Fatal("transaction did not capture one coalesced event", captured, err)
		}
		f.expect(t) // a different connection/dispatcher cannot observe the capture
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal(err)
		}
		f.expect(t)
		f.count(t, 0, "SELECT count(*) FROM availability_slots WHERE tutor_id=$1 AND starts_at >= '2027-01-14'", fixture(bookingTutor))
	})
	t.Run("Redis failure preserves committed availability and stable retry identity", func(t *testing.T) {
		body := map[string]string{"starts_at": "2027-01-13T09:00:00Z", "ends_at": "2027-01-13T10:00:00Z", "allowed_mode": "ONLINE"}
		id := fields(t, f.a.call(t, 201, "POST", own+"/slots", body, f.cookie[bookingTutor]))["data"].(map[string]any)["id"].(string)
		before := f.pending(t)
		options := *f.redis.Options()
		broken := redis.NewClient(&options)
		_ = broken.Close()
		if n, err := app.NewDispatcher(f.store, fanout.New(broken)).RunBatch(f.ctx); err == nil || n != 0 {
			t.Fatal("failed Redis publication acknowledged", n, err)
		}
		f.count(t, 1, "SELECT count(*) FROM availability_slots WHERE id=$1", id)
		f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND last_error='fanout_failed' AND attempt_count=1 AND claim_token IS NULL AND lease_until IS NULL AND next_attempt_at>occurred_at")
		f.expectSockets(t, nil, nil)
		f.exec(t, "UPDATE realtime_events SET next_attempt_at=statement_timestamp() WHERE dispatched_at IS NULL")
		after := f.expect(t, availability)
		for id, event := range before {
			if after[id] != event {
				t.Fatal("retry changed the stable event ID or payload")
			}
			f.count(t, 1, "SELECT count(*) FROM realtime_events WHERE id=$1 AND attempt_count=2 AND dispatched_at IS NOT NULL AND last_error IS NULL", id)
		}
	})
	t.Run("student who tutors invalidates both owners on reservation and release", func(t *testing.T) {
		id := f.request(t, 201, bookingTutor, 72*time.Hour, bookingOtherOffer, bookingOtherSlot)
		wants := []publicationWant{bookingPublication(id, bookingTutor, bookingOtherTutor, bookingCoordinator), availability, availabilityPublication(bookingOtherTutor)}
		f.expect(t, wants...)
		f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, bookingTutor)
		f.expect(t, wants...)
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	})

	t.Run("policy cancellation reaches participants and session-faculty coordinator", func(t *testing.T) {
		id := f.request(t, 201, bookingStudent, 24*time.Hour, bookingOffer, bookingSlot)
		f.expect(t, bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
		f.expect(t, bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability)
		f.a.call(t, 404, "POST", policyPath(id), policyReason, f.cookie[bookingHomeCoordinator])
		f.expect(t)
		f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[bookingCoordinator])
		f.expect(t, bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability)
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		f.count(t, 1, `SELECT count(*) FROM session_status_history h JOIN tutoring_sessions s ON s.id=h.session_id WHERE s.id=$1
			AND h.from_status='ACCEPTED' AND h.to_status='CANCELLED' AND h.cancellation_origin='COORDINATOR_POLICY'
			AND h.changed_by_user_id=$2 AND h.reason=s.cancellation_reason AND h.reason=btrim($3)
			AND s.cancelled_by_user_id=$2 AND NOT s.is_late_cancellation`, id, fixture(bookingCoordinator), policyReason["reason"])
		f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED' AND email_status='PENDING' AND strpos(body,btrim($2))=0", id, policyReason["reason"])
	})
	t.Run("suspension cancellation excludes suspended user and home-faculty coordinator", func(t *testing.T) {
		var ids []string
		for i, at := range []time.Duration{30 * time.Hour, 36 * time.Hour} {
			id := f.request(t, 201, bookingStudent, at, bookingOffer, bookingSlot)
			ids = append(ids, id)
			f.expect(t, bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability)
			if i == 0 {
				f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
				f.expect(t, bookingPublication(id, bookingStudent, bookingTutor, bookingCoordinator), availability)
			}
		}
		response := fields(t, f.a.call(t, 200, "POST", "/coordinator/users/"+fixture(bookingStudent)+"/suspend", suspensionReason, f.cookie[bookingHomeCoordinator]))["data"].(map[string]any)
		if response["cancelled_sessions"] != float64(2) {
			t.Fatal("suspension did not cancel both sessions", response)
		}
		f.disconnected(t, bookingStudent)
		wants := []publicationWant{availability}
		for _, id := range ids {
			wants = append(wants, publicationWant{app.BookingChanged, id, []int{bookingTutor, bookingCoordinator}})
			f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
			f.count(t, 1, `SELECT count(*) FROM session_status_history h JOIN tutoring_sessions s ON s.id=h.session_id
				WHERE s.id=$1 AND s.status='CANCELLED' AND NOT s.is_late_cancellation AND h.to_status='CANCELLED'
				AND h.cancellation_origin='SUSPENSION' AND h.suspension_history_id=$2 AND h.changed_by_user_id=$3
				AND h.reason=s.cancellation_reason AND s.cancellation_reason='Session cancelled following an account suspension.'`, id, response["decision_id"], fixture(bookingHomeCoordinator))
			f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED' AND email_status='PENDING' AND strpos(body,'private suspension evidence')=0", id)
		}
		// Both cancellations and both reservation releases share one transaction:
		// two private booking events, exactly one public tutor invalidation.
		f.expect(t, wants...)
		f.count(t, 1, "SELECT count(*) FROM user_suspension_history WHERE id=$1 AND user_id=$2 AND is_suspended AND reason=btrim($3)", response["decision_id"], fixture(bookingStudent), suspensionReason["reason"])
		f.count(t, 1, "SELECT count(*) FROM notifications WHERE recipient_user_id=$1 AND type='USER_SUSPENDED' AND deduplication_key=$2 AND email_status='PENDING' AND strpos(body,'private suspension evidence')=0", fixture(bookingStudent), "account-decision:"+response["decision_id"].(string))
	})
}
