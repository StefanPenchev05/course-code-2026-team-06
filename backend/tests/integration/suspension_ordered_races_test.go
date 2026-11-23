package integration_test

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func (f *suspensionFixture) testSuspensionOrderedRaces(t *testing.T, book func(*testing.T, bool, bool) (string, time.Duration), next *time.Duration) {
	for _, bookingFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("suspension ordered booking race booking_first_%t", bookingFirst), func(t *testing.T) {
			// A separate accepted session proves the other cancellations also commit.
			companion, _ := book(t, false, true)
			f.setTime(0)
			at := *next
			*next += 2 * time.Hour
			payload := f.payload(at, 5321, 7331)
			table, event, condition := "users", "UPDATE", "NEW.id='"+fixture(suspTutor)+"' AND NEW.is_suspended AND NOT OLD.is_suspended"
			if bookingFirst {
				table, event, condition = "tutoring_sessions", "INSERT", "NEW.tutor_id='"+fixture(suspTutor)+"'"
			}
			release := f.blockSuspensionWrite(t, table, event, condition)
			admin, booking := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			startAdmin := func() {
				go func() {
					admin <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
				}()
			}
			startBooking := func() {
				go func() { booking <- f.a.request("POST", "/sessions", origin, payload, f.cookie[suspStudent]) }()
			}
			if bookingFirst {
				startBooking()
				f.waitBlocked(t, 1, "%INSERT INTO tutoring_sessions%")
				startAdmin()
				f.waitBlocked(t, 1, "%FOR NO KEY UPDATE%")
			} else {
				startAdmin()
				f.waitBlocked(t, 1, "%UPDATE users SET is_suspended%")
				startBooking()
				f.waitBlocked(t, 1, "%FOR NO KEY UPDATE%")
			}
			release()
			a, b := <-admin, <-booking
			if a.Code != 200 {
				t.Fatal(a.Code, a.Body.String())
			}
			wantCode, cancelled := 403, 1
			if bookingFirst {
				wantCode, cancelled = 201, 2
			}
			if b.Code != wantCode {
				t.Fatal("unexpected serialization", b.Code, b.Body.String())
			}
			decision := fields(t, a)["data"].(map[string]any)
			if decision["cancelled_sessions"] != float64(cancelled) {
				t.Fatal(decision)
			}
			f.assertDecision(t, decision["decision_id"].(string), suspTutor, suspCoordinator, true, cancelled)
			f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED'", companion)
			if bookingFirst {
				id := fields(t, b)["data"].(map[string]any)["id"].(string)
				f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED'", id)
			} else {
				f.count(t, 0, "SELECT count(*) FROM tutoring_sessions WHERE tutor_id=$1 AND starts_at=$2", fixture(suspTutor), f.start.Add(at))
			}
			f.decide(t, suspCoordinator, suspTutor, false, 200)
		})
	}
	for _, expiryFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("suspension ordered expiry race expiry_first_%t", expiryFirst), func(t *testing.T) {
			id, _ := book(t, true, false)
			companion, _ := book(t, false, true)
			var deadline time.Time
			if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", id).Scan(&deadline); err != nil {
				t.Fatal(err)
			}
			f.clock.Store(deadline.UnixMicro())
			terminal := "CANCELLED"
			if expiryFirst {
				terminal = "EXPIRED"
			}
			release := f.blockSuspensionWrite(t, "tutoring_sessions", "UPDATE", "NEW.id='"+id+"' AND NEW.status='"+terminal+"'")
			admin := make(chan *httptest.ResponseRecorder, 1)
			worker := make(chan error, 1)
			startAdmin := func() {
				go func() {
					admin <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
				}()
			}
			startWorker := func() {
				go func() { _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); worker <- err }()
			}
			if expiryFirst {
				startWorker()
				f.waitBlocked(t, 1, "%UPDATE tutoring_sessions SET status%")
				startAdmin()
				f.waitBlocked(t, 1, "%FOR UPDATE OF s%")
				release()
				if err := <-worker; err != nil {
					t.Fatal(err)
				}
			} else {
				startAdmin()
				f.waitBlocked(t, 1, "%UPDATE tutoring_sessions SET status%")
				startWorker()
				// SKIP LOCKED must allow the worker to finish while suspension
				// still owns the candidate, without taking its terminal transition.
				select {
				case err := <-worker:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("expiry did not skip suspension's row lock")
				}
				release()
			}
			a := <-admin
			if a.Code != 200 {
				t.Fatal(a.Code, a.Body.String())
			}
			cancelled := 2
			if expiryFirst {
				cancelled = 1
			}
			decision := fields(t, a)["data"].(map[string]any)
			if decision["cancelled_sessions"] != float64(cancelled) {
				t.Fatal(decision)
			}
			f.assertDecision(t, decision["decision_id"].(string), suspTutor, suspCoordinator, true, cancelled)
			f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status::text=$2", id, terminal)
			f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED'", companion)
			f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status IN ('CANCELLED','EXPIRED')", id)
			f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type IN ('SESSION_CANCELLED','REQUEST_EXPIRED')", id)
			f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
			f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
			before := f.sessionEvidence(t, id)
			if _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); err != nil {
				t.Fatal(err)
			}
			if before != f.sessionEvidence(t, id) {
				t.Fatal("repeated expiry altered terminal evidence")
			}
			f.decide(t, suspCoordinator, suspTutor, false, 200)
		})
	}
}
