package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"
)

func (f *policyFixture) waitBlocks(t *testing.T, n int, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name='tutti_policy_integration'
			AND query LIKE $1 AND cardinality(pg_blocking_pids(pid))>0`, query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= n {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("requests did not reach lock barrier")
		}
	}
}

func (f *policyFixture) testPolicyRaces(t *testing.T) {
	for _, kind := range []string{"duplicate", "acceptance", "participant cancellation", "completion"} {
		t.Run("race with "+kind, func(t *testing.T) {
			accepted := kind == "participant cancellation" || kind == "completion"
			id := f.book(t, accepted)
			path, actor, body := policyPath(id), policyCoordinator, any(policyReason)
			switch kind {
			case "acceptance":
				path, actor, body = tutorRequestsPath+id+"/accept", policyTutor, testMeeting
			case "participant cancellation":
				path, actor, body = "/sessions/"+id+"/cancel", policyStudent, testReason
				f.setTime(time.Duration(f.next-4) * time.Hour)
			case "completion":
				path, actor, body = "/tutors/me/sessions/"+id+"/complete", policyTutor, nil
				f.setTime(time.Duration(f.next-1) * time.Hour)
			}
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err := tx.Exec(f.ctx, "SELECT id FROM tutoring_sessions WHERE id=$1 FOR UPDATE", id); err != nil {
				t.Fatal(err)
			}
			policy, other := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
			go func() {
				policy <- f.a.request("POST", policyPath(id), origin, policyReason, f.cookie[policyCoordinator])
			}()
			go func() { other <- f.a.request("POST", path, origin, body, f.cookie[actor]) }()
			f.waitBlocks(t, 2, "%FOR UPDATE OF s%")
			if err := tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			x, y := <-policy, <-other
			if kind == "acceptance" {
				if x.Code != 200 || (y.Code != 200 && y.Code != 409) {
					t.Fatal(x.Code, y.Code, x.Body.String(), y.Body.String())
				}
				from, count := "REQUESTED", 2
				if y.Code == 200 {
					from, count = "ACCEPTED", 3
				}
				f.cancelled(t, id, policyCoordinator, policyReason["reason"], from)
				f.count(t, count, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
			} else {
				if !((x.Code == 200 && y.Code == 409) || (x.Code == 409 && y.Code == 200)) {
					t.Fatal(x.Code, y.Code, x.Body.String(), y.Body.String())
				}
				if x.Code == 200 || kind == "duplicate" {
					from := "REQUESTED"
					if accepted {
						from = "ACCEPTED"
					}
					f.cancelled(t, id, policyCoordinator, policyReason["reason"], from)
				} else if kind == "participant cancellation" {
					f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED' AND is_late_cancellation AND cancelled_by_user_id=$2", id, fixture(policyStudent))
					f.count(t, 1, "SELECT count(*) FROM user_violations WHERE session_id=$1 AND user_id=$2", id, fixture(policyStudent))
					f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED'", id)
					f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND cancellation_origin='PARTICIPANT'", id)
				} else {
					f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='COMPLETED'", id)
					f.count(t, 0, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED'", id)
				}
				count := 2
				if accepted {
					count = 3
				}
				f.count(t, count, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
			}
			f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		})
	}
	t.Run("expiration race and terminal conflicts", func(t *testing.T) {
		id := f.book(t, false)
		var deadline time.Time
		if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", id).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		f.clock.Store(deadline.UnixMicro())
		start := make(chan struct{})
		policy := make(chan *httptest.ResponseRecorder, 1)
		worker := make(chan error, 1)
		go func() {
			<-start
			policy <- f.a.request("POST", policyPath(id), origin, policyReason, f.cookie[policyCoordinator])
		}()
		go func() { <-start; _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); worker <- err }()
		close(start)
		w, err := <-policy, <-worker
		if err != nil {
			t.Fatal(err)
		}
		if w.Code == 200 {
			f.cancelled(t, id, policyCoordinator, policyReason["reason"], "REQUESTED")
		} else if w.Code == 409 {
			f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='EXPIRED'", id)
			f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='REQUEST_EXPIRED'", id)
		} else {
			t.Fatal(w.Code, w.Body.String())
		}
		f.count(t, 2, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
		f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		for _, terminal := range []string{"DECLINED", "EXPIRED", "COMPLETED", "NO_SHOW"} {
			id := f.book(t, terminal == "COMPLETED" || terminal == "NO_SHOW")
			switch terminal {
			case "DECLINED":
				f.session(t, 200, "POST", tutorRequestsPath+id+"/decline", testReason, policyTutor)
			case "EXPIRED":
				if err := f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", id).Scan(&deadline); err != nil {
					t.Fatal(err)
				}
				if _, err := f.service.ExpireDueRequestsBatch(f.ctx, deadline, 500); err != nil {
					t.Fatal(err)
				}
			case "COMPLETED", "NO_SHOW":
				f.setTime(time.Duration(f.next-1) * time.Hour)
				f.session(t, 200, "POST", "/tutors/me/sessions/"+id+"/complete", nil, policyTutor)
				if terminal == "NO_SHOW" {
					report := f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", map[string]string{"description": "Absent tutor"}, policyStudent)["id"].(string)
					f.session(t, 200, "POST", "/coordinator/attendance-reports/"+report+"/confirm", map[string]string{"resolution_note": "Confirmed absence"}, policyCoordinator)
				}
			}
			before := f.state(t, id)
			f.a.call(t, 409, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
			if f.state(t, id) != before {
				t.Fatal("terminal history overwritten", terminal)
			}
		}
	})
}

func (f *policyFixture) testPolicyAuthority(t *testing.T) {
	t.Run("stored faculty survives tutor program change", func(t *testing.T) {
		id := f.book(t, true)
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(policyTutor), fixture(4112))
		defer f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(policyTutor), fixture(4111))
		f.a.call(t, 404, "POST", policyPath(id), policyReason, f.cookie[policyOtherCoordinator])
		f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
		f.cancelled(t, id, policyCoordinator, policyReason["reason"], "ACCEPTED")
	})
	t.Run("authority changes before transaction locks", func(t *testing.T) {
		// Book while the tutor is in faculty C, then move them back. The mutable
		// coordinator has no historical approval FK preventing reassignment.
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(policyTutor), fixture(4113))
		id := f.book(t, true)
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(policyTutor), fixture(4111))
		for _, v := range []struct {
			name, change, restore string
			code                  int
		}{
			{"reassignment", "UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000004104' WHERE user_id=$1", "UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000004103' WHERE user_id=$1", 404},
			{"suspension", "UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='Fixture' WHERE id=$1", "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1", 403},
			{"closure", "UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1", 403},
			{"removal", "DELETE FROM coordinators WHERE user_id=$1", "INSERT INTO coordinators VALUES($1,'POLICY-restored','00000000-0000-0000-0000-000000004103')", 403},
		} {
			t.Run(v.name, func(t *testing.T) {
				before := f.state(t, id)
				tx, err := f.db.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(f.ctx)
				if _, err := tx.Exec(f.ctx, v.change, fixture(policyMutableCoordinator)); err != nil {
					t.Fatal(err)
				}
				result := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					result <- f.a.request("POST", policyPath(id), origin, policyReason, f.cookie[policyMutableCoordinator])
				}()
				f.waitBlocks(t, 1, "%SELECT c.faculty_id::text FROM coordinators%FOR SHARE OF c, u%")
				if err := tx.Commit(f.ctx); err != nil {
					t.Fatal(err)
				}
				w := <-result
				if w.Code != v.code {
					t.Fatal(w.Code, w.Body.String())
				}
				if f.state(t, id) != before {
					t.Fatal("authority change left effects")
				}
				f.a.call(t, v.code, "POST", policyPath(id), policyReason, f.cookie[policyMutableCoordinator])
				f.exec(t, v.restore, fixture(policyMutableCoordinator))
			})
		}
		f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyMutableCoordinator])
		f.cancelled(t, id, policyMutableCoordinator, policyReason["reason"], "ACCEPTED")
		f.exec(t, "DELETE FROM coordinators WHERE user_id=$1", fixture(policyMutableCoordinator))
		f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND cancellation_origin='COORDINATOR_POLICY' AND changed_by_user_id=$2", id, fixture(policyMutableCoordinator))
	})
	t.Run("multi-role actor capacity and revoked or expired login", func(t *testing.T) {
		f.exec(t, "INSERT INTO students VALUES($1,'POLICY-dual',$2)", fixture(policyCoordinator), fixture(4111))
		id := f.book(t, false) // coordinator has a pending student role
		f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
		f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$1)", fixture(policyCoordinator), fixture(4101))
		for _, policy := range []bool{true, false} {
			f.setTime(0)
			id := f.request(t, 201, policyCoordinator, time.Duration(f.next)*time.Hour, 4321, 4331)
			f.next += 2
			if policy {
				f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
				f.cancelled(t, id, policyCoordinator, policyReason["reason"], "REQUESTED")
			} else {
				f.setTime(time.Duration(f.next-3) * time.Hour)
				f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, policyCoordinator)
				f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND cancellation_origin='PARTICIPANT' AND changed_by_user_id=$2", id, fixture(policyCoordinator))
				f.count(t, 1, "SELECT count(*) FROM user_violations WHERE session_id=$1 AND user_id=$2", id, fixture(policyCoordinator))
			}
		}
		id = f.book(t, false)
		before := f.state(t, id)
		f.a.call(t, 204, "POST", "/auth/logout", nil, f.cookie[policyCoordinator])
		f.a.call(t, 401, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
		if f.state(t, id) != before {
			t.Fatal("revoked login changed session")
		}
		fresh := f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": fixture(policyCoordinator) + "@uni.example", "password": password}, nil).Result().Cookies()[0]
		// Expire only this disposable fixture's newly issued Redis session. A
		// past deadline exercises expiration without a wall-clock sleep.
		digest := sha256.Sum256([]byte(fresh.Value))
		key := "tutti:identity:session:" + base64.RawURLEncoding.EncodeToString(digest[:])
		if expired, err := f.redis.PExpireAt(f.ctx, key, time.Unix(1, 0)).Result(); err != nil || !expired {
			t.Fatal("could not expire fixture login", err)
		}
		f.a.call(t, 401, "POST", policyPath(id), policyReason, fresh)
		if f.state(t, id) != before {
			t.Fatal("expired login changed session")
		}
	})
}
