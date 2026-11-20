package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"
)

func (f *suspensionFixture) waitBlocked(t *testing.T, n int, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var got int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name='tutti_suspension_integration' AND query LIKE $1 AND cardinality(pg_blocking_pids(pid))>0`, query).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got >= n {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("lock barrier not reached")
		case <-tick.C:
		}
	}
}
func (f *suspensionFixture) testSuspensionRaces(t *testing.T) {
	t.Run("cancelled request rolls back account and earlier writes", func(t *testing.T) {
		id := f.book(t, true, true)
		before := f.snapshot(t, suspensionStateTables...)
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err := tx.Exec(f.ctx, "SELECT id FROM tutoring_sessions WHERE id=$1 FOR UPDATE", id); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		r := httptest.NewRequest("POST", origin+"/api/v1"+suspensionPath(fixture(suspTutor), true), bytes.NewBufferString(`{"reason":"Cancellation rollback fixture"}`)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.AddCookie(f.cookie[suspCoordinator])
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { w := httptest.NewRecorder(); f.a.handler.ServeHTTP(w, r); result <- w }()
		f.waitBlocked(t, 1, "%FOR UPDATE OF s%")
		cancel()
		if w := <-result; w.Code == 200 {
			t.Fatal("cancelled request succeeded")
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if before != f.snapshot(t, suspensionStateTables...) {
			t.Fatal("cancelled transaction left effects")
		}
		f.decide(t, suspCoordinator, suspTutor, true, 200)
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
	for _, kind := range []string{"booking", "acceptance", "participant cancellation", "completion", "duplicate suspension", "reinstatement"} {
		t.Run("race with "+kind, func(t *testing.T) {
			id := ""
			path := "/sessions"
			actor := suspStudent
			var body any
			if kind == "booking" {
				f.setTime(0)
				body = f.payload(time.Duration(f.next)*time.Hour, 5321, 5331)
				f.next += 2
			} else {
				id = f.book(t, true, kind == "completion" || kind == "participant cancellation")
			}
			switch kind {
			case "acceptance":
				path, actor, body = tutorRequestsPath+id+"/accept", suspTutor, testMeeting
			case "participant cancellation":
				path, actor, body = "/sessions/"+id+"/cancel", suspStudent, testReason
				f.setTime(time.Duration(f.next-3) * time.Hour)
			case "completion":
				path, actor = "/tutors/me/sessions/"+id+"/complete", suspTutor
				f.setTime(time.Duration(f.next-1) * time.Hour)
			case "duplicate suspension":
				path, actor, body = suspensionPath(fixture(suspTutor), true), suspCoordinator, suspensionReason
			case "reinstatement":
				path, actor, body = suspensionPath(fixture(suspTutor), false), suspCoordinator, suspensionReason
			}
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, "SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE", fixture(suspTutor)); err != nil {
				t.Fatal(err)
			}
			admin, other := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() {
				admin <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
			}()
			go func() { other <- f.a.request("POST", path, origin, body, f.cookie[actor]) }()
			f.waitBlocked(t, 2, "%FOR NO KEY UPDATE%")
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			a, b := <-admin, <-other
			if kind == "duplicate suspension" {
				if !((a.Code == 200 && b.Code == 409) || (a.Code == 409 && b.Code == 200)) {
					t.Fatal(a.Code, b.Code, a.Body.String(), b.Body.String())
				}
			} else if a.Code != 200 {
				t.Fatal(a.Code, a.Body.String())
			}
			switch kind {
			case "booking":
				if b.Code != 201 && b.Code != 403 {
					t.Fatal(b.Code, b.Body.String())
				}
				if b.Code == 201 {
					id = fields(t, b)["data"].(map[string]any)["id"].(string)
				}
			case "acceptance", "participant cancellation", "completion":
				if b.Code != 200 && b.Code != 403 && b.Code != 409 {
					t.Fatal(b.Code, b.Body.String())
				}
			case "reinstatement":
				if b.Code != 200 && b.Code != 409 {
					t.Fatal(b.Code, b.Body.String())
				}
			}
			if id != "" {
				f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1", id)
				if kind != "completion" && kind != "reinstatement" {
					f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
				}
				if kind == "participant cancellation" {
					want := 0
					if b.Code == 200 {
						want = 1
					}
					f.count(t, want, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
					f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='CANCELLED'", id)
				}
				if kind == "completion" && b.Code == 200 {
					f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='COMPLETED'", id)
				}
			}
			var suspended bool
			if err := f.db.QueryRow(f.ctx, "SELECT is_suspended FROM users WHERE id=$1", fixture(suspTutor)).Scan(&suspended); err != nil {
				t.Fatal(err)
			}
			if suspended {
				f.count(t, 0, "SELECT count(*) FROM tutoring_sessions WHERE (student_id=$1 OR tutor_id=$1) AND starts_at >= $2 AND status IN ('REQUESTED','ACCEPTED')", fixture(suspTutor), time.UnixMicro(f.clock.Load()))
				f.decide(t, suspCoordinator, suspTutor, false, 200)
			}
		})
	}
	for _, policyFirst := range []bool{true, false} {
		t.Run("race with policy cancellation", func(t *testing.T) {
			id := f.book(t, true, true)
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, "SELECT id FROM tutoring_sessions WHERE id=$1 FOR UPDATE", id); err != nil {
				t.Fatal(err)
			}
			admin, policy := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			startAdmin := func() {
				go func() {
					admin <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
				}()
			}
			startPolicy := func() {
				go func() { policy <- f.a.request("POST", policyPath(id), origin, policyReason, f.cookie[suspCoordinator]) }()
			}
			if policyFirst {
				startPolicy()
				f.waitBlocked(t, 1, "%FOR UPDATE OF s%")
				startAdmin()
			} else {
				startAdmin()
				f.waitBlocked(t, 1, "%FOR UPDATE OF s%")
				startPolicy()
				f.waitBlocked(t, 2, "%")
			}
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			a, p := <-admin, <-policy
			if a.Code != 200 || (policyFirst && p.Code != 200) || (!policyFirst && p.Code != 409) {
				t.Fatal(a.Code, p.Code, a.Body.String(), p.Body.String())
			}
			f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='CANCELLED'", id)
			f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED'", id)
			f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
			f.decide(t, suspCoordinator, suspTutor, false, 200)
		})
	}
	t.Run("race with expiration", func(t *testing.T) {
		id := f.book(t, true, false)
		var deadline time.Time
		if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", id).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		f.clock.Store(deadline.UnixMicro())
		start := make(chan struct{})
		admin := make(chan *httptest.ResponseRecorder, 1)
		worker := make(chan error, 1)
		go func() {
			<-start
			admin <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
		}()
		go func() { <-start; _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); worker <- err }()
		close(start)
		if a, err := <-admin, <-worker; a.Code != 200 || err != nil {
			t.Fatal(a.Code, a.Body.String(), err)
		}
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status IN ('CANCELLED','EXPIRED')", id)
		f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status IN ('CANCELLED','EXPIRED')", id)
		f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type IN ('SESSION_CANCELLED','REQUEST_EXPIRED')", id)
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
}

func (f *suspensionFixture) testSuspensionAuthority(t *testing.T) {
	t.Run("target program change while authority is pending", func(t *testing.T) {
		f.book(t, true, true)
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err := tx.Exec(f.ctx, "SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE", fixture(suspTutor)); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(f.ctx, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(suspTutor), fixture(5112)); err != nil {
			t.Fatal(err)
		}
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			result <- f.a.request("POST", suspensionPath(fixture(suspTutor), true), origin, suspensionReason, f.cookie[suspCoordinator])
		}()
		f.waitBlocked(t, 1, "%FOR NO KEY UPDATE%")
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if w := <-result; w.Code != 404 {
			t.Fatal(w.Code, w.Body.String())
		}
		f.decide(t, suspOtherCoordinator, suspTutor, true, 200)
		f.decide(t, suspCoordinator, suspTutor, false, 404)
		f.decide(t, suspOtherCoordinator, suspTutor, false, 200)
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(suspTutor), fixture(5111))
	})
	t.Run("coordinator authority changes under lock", func(t *testing.T) {
		for _, v := range []struct {
			name, change, restore, barrier string
			code                           int
		}{
			{"reassignment", "UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000005104' WHERE user_id=$1", "UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000005103' WHERE user_id=$1", "%FOR SHARE OF c, u%", 404},
			{"suspension", "UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='Fixture' WHERE id=$1", "UPDATE users SET is_suspended=false WHERE id=$1", "%FOR NO KEY UPDATE%", 403},
			{"closure", "UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1", "%FOR NO KEY UPDATE%", 403},
			{"removal", "DELETE FROM coordinators WHERE user_id=$1", "INSERT INTO coordinators VALUES($1,'SUSP-restored','00000000-0000-0000-0000-000000005103')", "%FOR SHARE OF c, u%", 403},
		} {
			t.Run(v.name, func(t *testing.T) {
				// Keep direct account-state fixtures free of a current coordinator FK
				// before the removal scenario; no audit rows are fabricated.
				f.exec(t, "UPDATE users SET suspended_by_coordinator_id=NULL,suspended_at=NULL,suspension_reason=NULL WHERE id=$1 AND NOT is_suspended", fixture(suspMutableCoordinator))
				tx, err := f.db.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.ctx)
				if _, err := tx.Exec(f.ctx, v.change, fixture(suspMutableCoordinator)); err != nil {
					t.Fatal(err)
				}
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					result <- f.a.request("POST", suspensionPath(fixture(suspMutableTarget), true), origin, suspensionReason, f.cookie[suspMutableCoordinator])
				}()
				f.waitBlocked(t, 1, v.barrier)
				if err := tx.Commit(f.ctx); err != nil {
					t.Fatal(err)
				}
				if w := <-result; w.Code != v.code {
					t.Fatal(w.Code, w.Body.String())
				}
				f.count(t, 0, "SELECT count(*) FROM user_suspension_history WHERE user_id=$1", fixture(suspMutableTarget))
				f.count(t, 0, "SELECT count(*) FROM users WHERE id=$1 AND is_suspended", fixture(suspMutableTarget))
				f.exec(t, v.restore, fixture(suspMutableCoordinator))
			})
		}
	})
	t.Run("multi role coordinator account and expired login", func(t *testing.T) {
		f.exec(t, "INSERT INTO students VALUES($1,'SUSP-coordinator-student',$2)", fixture(suspCoordinator), fixture(5112))
		f.decide(t, suspOtherCoordinator, suspCoordinator, true, 200)
		for _, path := range []string{"/coordinator/statistics", "/coordinator/verifications", "/coordinator/issues"} {
			f.a.call(t, 403, "GET", path, nil, f.cookie[suspCoordinator])
		}
		f.decide(t, suspCoordinator, suspPending, true, 403)
		f.decide(t, suspOtherCoordinator, suspCoordinator, false, 200)
		f.a.call(t, 200, "GET", "/coordinator/statistics", nil, f.cookie[suspCoordinator])
		f.a.call(t, 403, "POST", "/sessions", map[string]any{}, f.cookie[suspCoordinator]) // pending student stays pending
		fresh := f.login(t, suspCoordinator)
		digest := sha256.Sum256([]byte(fresh.Value))
		key := "tutti:identity:session:" + base64.RawURLEncoding.EncodeToString(digest[:])
		if expired, err := f.redis.PExpireAt(f.ctx, key, time.Unix(1, 0)).Result(); err != nil || !expired {
			t.Fatal(err)
		}
		f.a.call(t, 401, "POST", suspensionPath(fixture(suspPending), true), suspensionReason, fresh)
	})
}
