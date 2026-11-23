package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// Issue #49 adds failure-position and forced-order evidence to the existing
// suspension workflow. Its own semester range avoids other suites' calendars.
func (f *suspensionFixture) testSuspensionAtomicity(t *testing.T) {
	oldStart, oldClock := f.start, f.clock.Load()
	defer func() { f.start = oldStart; f.clock.Store(oldClock) }()
	f.start = time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)
	f.setTime(0)
	f.exec(t, "INSERT INTO semesters VALUES($1,'Atomic suspension first term','2050-01-01T00:00Z','2050-02-01T00:00Z'),($2,'Atomic suspension second term','2050-02-01T00:00Z','2050-03-01T00:00Z')", fixture(7131), fixture(7132))
	for i, tutor := range []int{suspTutor, suspOtherTutor} {
		f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,'2050-01-01T00:00Z','2050-02-01T00:00Z','BOTH'),($3,$2,'2050-02-01T00:00Z','2050-03-01T00:00Z','BOTH')", fixture(7331+i), fixture(tutor), fixture(7333+i))
	}
	next := 8 * time.Hour
	book := func(t *testing.T, asTutor, accepted bool) (string, time.Duration) {
		t.Helper()
		f.setTime(0)
		student, tutor, offer, slot := suspTutor, suspOtherTutor, 5322, 7332
		if asTutor {
			student, tutor, offer, slot = suspStudent, suspTutor, 5321, 7331
		}
		at := next
		next += 2 * time.Hour
		id := f.request(t, 201, student, at, offer, slot)
		if accepted {
			f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, tutor)
		}
		return id, at
	}
	t.Run("suspension microsecond boundaries preserve excluded session evidence", func(t *testing.T) {
		var terminal []string
		for _, delta := range []time.Duration{-time.Microsecond, 0, time.Microsecond} {
			id, at := book(t, true, true)
			f.setTime(at - delta)
			before := f.sessionEvidence(t, id)
			want := 1
			if delta < 0 {
				want = 0
			}
			got := f.decide(t, suspCoordinator, suspTutor, true, 200)
			if got["cancelled_sessions"] != float64(want) {
				t.Fatal(delta, got)
			}
			f.assertDecision(t, got["decision_id"].(string), suspTutor, suspCoordinator, true, want)
			if delta < 0 {
				if before != f.sessionEvidence(t, id) {
					t.Fatal("already-started session or its evidence changed")
				}
				f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
			}
			f.decide(t, suspCoordinator, suspTutor, false, 200)
			if delta < 0 {
				f.setTime(at + time.Hour)
				f.session(t, 200, "POST", "/tutors/me/sessions/"+id+"/complete", nil, suspTutor)
				terminal = append(terminal, id)
			}
		}
		// Include retained no-show evidence from the existing #25 scenario, plus
		// terminal future sessions that the active-status predicate must exclude.
		var noShow string
		if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM tutoring_sessions WHERE tutor_id=$1 AND status='NO_SHOW' ORDER BY id LIMIT 1", fixture(suspTutor)).Scan(&noShow); err != nil {
			t.Fatal(err)
		}
		terminal = append(terminal, noShow)
		id, _ := book(t, true, false)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/decline", testReason, suspTutor)
		terminal = append(terminal, id)
		id, _ = book(t, false, true)
		f.session(t, 200, "POST", policyPath(id), policyReason, suspOtherCoordinator)
		terminal = append(terminal, id)
		id, _ = book(t, true, false)
		var deadline time.Time
		if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", id).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); err != nil {
			t.Fatal(err)
		}
		terminal = append(terminal, id)
		// Still REQUESTED after its deadline remains cancellable by suspension.
		overdue, _ := book(t, false, false)
		if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", overdue).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		f.clock.Store(deadline.Add(time.Microsecond).UnixMicro())
		before := f.sessionEvidence(t, terminal...)
		got := f.decide(t, suspCoordinator, suspTutor, true, 200)
		f.assertDecision(t, got["decision_id"].(string), suspTutor, suspCoordinator, true, 1)
		if before != f.sessionEvidence(t, terminal...) {
			t.Fatal("terminal session/history/notification changed")
		}
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED'", overdue)
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
	t.Run("suspension failures at first middle and last session and after first page", func(t *testing.T) {
		ids := make([]string, 0, 105)
		for i := 0; i < 105; i++ {
			id, _ := book(t, i%2 == 0, i%3 == 0)
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, v := range []struct{ name, table, event, condition string }{
			{"first update", "tutoring_sessions", "UPDATE", "NEW.status='CANCELLED' AND NEW.id='" + ids[0] + "'"},
			{"middle update", "tutoring_sessions", "UPDATE", "NEW.status='CANCELLED' AND NEW.id='" + ids[52] + "'"},
			{"final update", "tutoring_sessions", "UPDATE", "NEW.status='CANCELLED' AND NEW.id='" + ids[104] + "'"},
			{"final history", "session_status_history", "INSERT", "NEW.cancellation_origin='SUSPENSION' AND NEW.session_id='" + ids[104] + "'"},
			{"final outbox", "notifications", "INSERT", "NEW.type='SESSION_CANCELLED' AND NEW.session_id='" + ids[104] + "'"},
		} {
			t.Run(v.name, func(t *testing.T) {
				before := f.snapshot(t, suspensionStateTables...)
				f.exec(t, `CREATE FUNCTION reject_atomic_suspension_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+v.condition+` THEN RAISE EXCEPTION 'atomic suspension test failure'; END IF; RETURN NEW; END; $$;
 CREATE TRIGGER reject_atomic_suspension_test BEFORE `+v.event+` ON `+v.table+` FOR EACH ROW EXECUTE FUNCTION reject_atomic_suspension_test()`)
				defer f.exec(t, "DROP TRIGGER reject_atomic_suspension_test ON "+v.table+"; DROP FUNCTION reject_atomic_suspension_test()")
				f.decide(t, suspCoordinator, suspTutor, true, 500)
				if before != f.snapshot(t, suspensionStateTables...) {
					t.Fatal("partial suspension after failed write", v.table)
				}
			})
		}
		for _, databaseCancel := range []bool{false, true} {
			t.Run(fmt.Sprintf("cancel_after_100_sessions_database_%t", databaseCancel), func(t *testing.T) {
				// Block UPDATE 101 inside the real transaction, after page 1 has
				// released reservations and written all 100 histories/outbox pairs.
				release := f.blockSuspensionWrite(t, "tutoring_sessions", "UPDATE", "NEW.status='CANCELLED' AND NEW.id='"+ids[100]+"'")
				before := f.snapshot(t, suspensionStateTables...)
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				r := httptest.NewRequest("POST", origin+"/api/v1"+suspensionPath(fixture(suspTutor), true), bytes.NewBufferString(`{"reason":"Cancel after first page"}`)).WithContext(ctx)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", origin)
				r.AddCookie(f.cookie[suspCoordinator])
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() { w := httptest.NewRecorder(); f.a.handler.ServeHTTP(w, r); result <- w }()
				f.waitBlocked(t, 1, "%UPDATE tutoring_sessions SET status%")
				if databaseCancel {
					var cancelled bool
					if err := f.db.QueryRow(f.ctx, `SELECT pg_cancel_backend(pid) FROM pg_stat_activity
 WHERE application_name='tutti_suspension_integration' AND query LIKE '%UPDATE tutoring_sessions SET status%'
 AND cardinality(pg_blocking_pids(pid))>0`).Scan(&cancelled); err != nil || !cancelled {
						t.Fatal(cancelled, err)
					}
				} else {
					cancel()
				}
				if w := <-result; w.Code == 200 {
					t.Fatal("aborted transaction returned success")
				}
				release()
				if before != f.snapshot(t, suspensionStateTables...) {
					t.Fatal("aborted second page left persistent effects")
				}
			})
		}
		got := f.decide(t, suspCoordinator, suspTutor, true, 200)
		if got["cancelled_sessions"] != float64(105) {
			t.Fatal(got)
		}
		f.assertDecision(t, got["decision_id"].(string), suspTutor, suspCoordinator, true, 105)
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
	t.Run("suspension preserves one strike across future semesters", func(t *testing.T) {
		// A late incident as student gives the previously unflagged second tutor
		// exactly one violation. Suspension must not turn it into a threshold flag.
		f.setTime(0)
		id := f.request(t, 201, suspOtherTutor, next, 5321, 7331)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, suspTutor)
		f.setTime(next - time.Hour)
		f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, suspOtherTutor)
		next += 2 * time.Hour
		f.count(t, 1, "SELECT count(*) FROM user_violations WHERE user_id=$1 AND semester_id=$2", fixture(suspOtherTutor), fixture(7131))
		for i, at := range []time.Duration{next, 752 * time.Hour} {
			f.setTime(0)
			id := f.request(t, 201, suspTutor, at, 5322, 7332+2*i)
			f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, suspOtherTutor)
		}
		f.setTime(next - time.Hour)
		evidence := f.snapshot(t, "user_violations", "user_semester_flags")
		got := f.decide(t, suspOtherCoordinator, suspOtherTutor, true, 200)
		f.assertDecision(t, got["decision_id"].(string), suspOtherTutor, suspOtherCoordinator, true, 2)
		if evidence != f.snapshot(t, "user_violations", "user_semester_flags") {
			t.Fatal("administrative strike or lost evidence")
		}
		f.decide(t, suspOtherCoordinator, suspOtherTutor, false, 200)
		next += 2 * time.Hour
	})
	f.testSuspensionOrderedRaces(t, book, &next)
}

func (f *suspensionFixture) sessionEvidence(t *testing.T, ids ...string) string {
	t.Helper()
	var result string
	if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'sessions',(SELECT jsonb_agg(to_jsonb(s) ORDER BY s.id) FROM tutoring_sessions s WHERE id=ANY($1::uuid[])),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY h.id) FROM session_status_history h WHERE session_id=ANY($1::uuid[])),
 'reservations',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.session_id,b.user_id) FROM session_participant_blocks b WHERE session_id=ANY($1::uuid[])),
 'notifications',(SELECT jsonb_agg(to_jsonb(n) ORDER BY n.id) FROM notifications n WHERE session_id=ANY($1::uuid[])))::text`, ids).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

// An isolated-test advisory lock pauses an actual write at a known position.
// Cleanup always releases the barrier; no timing sleeps choose the winning actor.
func (f *suspensionFixture) blockSuspensionWrite(t *testing.T, table, event, condition string) func() {
	t.Helper()
	f.exec(t, `CREATE FUNCTION block_atomic_suspension_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+condition+` THEN PERFORM pg_advisory_xact_lock(490049); END IF; RETURN NEW; END; $$;
 CREATE TRIGGER block_atomic_suspension_test BEFORE `+event+` ON `+table+` FOR EACH ROW EXECUTE FUNCTION block_atomic_suspension_test()`)
	t.Cleanup(func() {
		f.exec(t, "DROP TRIGGER block_atomic_suspension_test ON "+table+"; DROP FUNCTION block_atomic_suspension_test()")
	})
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(f.ctx) })
	if _, err := tx.Exec(f.ctx, "SELECT pg_advisory_xact_lock(490049)"); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
}
