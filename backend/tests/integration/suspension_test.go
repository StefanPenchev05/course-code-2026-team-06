package integration_test

import (
	"bytes"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestSuspensionAdministration(t *testing.T) {
	f := newSuspensionFixture(t)
	t.Run("home faculty authentication strict reasons and no partial effects", func(t *testing.T) {
		id := f.book(t, true, false) // student home B, session owned by A
		before := f.snapshot(t, suspensionStateTables...)
		for _, action := range []bool{true, false} {
			for _, v := range []struct{ actor, code int }{{0, 401}, {suspStudent, 403}, {suspTutor, 403}, {suspPending, 403}, {suspOtherCoordinator, 404}} {
				f.decide(t, v.actor, suspTutor, action, v.code)
			}
			missing := f.a.call(t, 404, "POST", suspensionPath(fixture(999999), action), suspensionReason, f.cookie[suspCoordinator])
			foreign := f.a.call(t, 404, "POST", suspensionPath(fixture(suspStudent), action), suspensionReason, f.cookie[suspCoordinator])
			if missing.Body.String() != foreign.Body.String() {
				t.Fatal("account enumeration")
			}
			for _, body := range []any{map[string]any{}, map[string]any{"reason": nil}, map[string]string{"reason": " \t\u2003"}, map[string]string{"reason": "a\x00b"}, map[string]string{"reason": strings.Repeat("é", 1001)}} {
				f.a.call(t, 422, "POST", suspensionPath(fixture(suspTutor), action), body, f.cookie[suspCoordinator])
			}
			for _, body := range []any{map[string]int{"reason": 1}, map[string]string{"reason": "x", "faculty_id": fixture(5101)}, map[string]string{"reason": "x", "coordinator_id": fixture(suspCoordinator)}, map[string]any{"reason": "x", "is_suspended": true}} {
				f.a.call(t, 400, "POST", suspensionPath(fixture(suspTutor), action), body, f.cookie[suspCoordinator])
			}
			f.a.call(t, 422, "POST", suspensionPath("bad", action), suspensionReason, f.cookie[suspCoordinator])
			f.a.call(t, 400, "POST", suspensionPath(fixture(suspTutor), action)+"?faculty_id="+fixture(5101), suspensionReason, f.cookie[suspCoordinator])
			f.a.call(t, 413, "POST", suspensionPath(fixture(suspTutor), action), map[string]string{"reason": strings.Repeat("a", 17000)}, f.cookie[suspCoordinator])
			for _, raw := range []string{`{"reason":`, `{"reason":"x"} {}`, "null", "{\"reason\":\"\xff\"}"} {
				r := httptest.NewRequest("POST", origin+"/api/v1"+suspensionPath(fixture(suspTutor), action), bytes.NewBufferString(raw))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Origin", origin)
				r.AddCookie(f.cookie[suspCoordinator])
				w := httptest.NewRecorder()
				f.a.handler.ServeHTTP(w, r)
				if w.Code != 400 {
					t.Fatal(w.Code, w.Body.String())
				}
			}
			for _, from := range []string{"", "https://attacker.example"} {
				if w := f.a.request("POST", suspensionPath(fixture(suspTutor), action), from, suspensionReason, f.cookie[suspCoordinator]); w.Code != 403 {
					t.Fatal(w.Code)
				}
			}
		}
		if before != f.snapshot(t, suspensionStateTables...) {
			t.Fatal("unauthorized or invalid operation left effects")
		}
		f.decide(t, suspCoordinator, suspTutor, false, 409)
		// Session authority does not authorize sanctions over this student.
		f.decide(t, suspOtherCoordinator, suspStudent, true, 200)
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='CANCELLED'", id)
		f.decide(t, suspCoordinator, suspStudent, false, 404)
		f.decide(t, suspOtherCoordinator, suspStudent, false, 200)
		for _, suspended := range []bool{false, true} {
			if suspended {
				f.decide(t, suspCoordinator, suspPending, true, 200)
			}
			f.exec(t, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(suspPending))
			before = f.snapshot(t, suspensionStateTables...)
			f.decide(t, suspCoordinator, suspPending, true, 409)
			f.decide(t, suspCoordinator, suspPending, false, 409)
			if before != f.snapshot(t, suspensionStateTables...) {
				t.Fatal("closed account changed")
			}
			f.exec(t, "UPDATE users SET closed_at=NULL WHERE id=$1", fixture(suspPending))
			if suspended {
				f.decide(t, suspCoordinator, suspPending, false, 200)
			}
		}
	})
	t.Run("both roles exact future boundary preserved evidence and live cookies", func(t *testing.T) {
		completed := f.book(t, true, true)
		f.setTime(time.Duration(f.next-1) * time.Hour)
		f.session(t, 200, "POST", "/tutors/me/sessions/"+completed+"/complete", nil, suspTutor)
		f.session(t, 201, "POST", "/sessions/"+completed+"/feedback", map[string]any{"rating": 5, "comment": "Private retained feedback"}, suspStudent)
		f.session(t, 201, "POST", "/issues", map[string]string{"session_id": completed, "description": "Retained report evidence"}, suspStudent)
		absent := f.book(t, true, true)
		f.setTime(time.Duration(f.next-1) * time.Hour)
		f.session(t, 200, "POST", "/tutors/me/sessions/"+absent+"/complete", nil, suspTutor)
		report := f.session(t, 201, "POST", "/sessions/"+absent+"/no-show-reports", map[string]string{"description": "Absent tutor"}, suspStudent)["id"].(string)
		f.session(t, 200, "POST", "/coordinator/attendance-reports/"+report+"/confirm", map[string]string{"resolution_note": "Confirmed retained evidence"}, suspCoordinator)
		late := f.book(t, true, true)
		f.setTime(time.Duration(f.next-3) * time.Hour)
		f.session(t, 200, "POST", "/sessions/"+late+"/cancel", testReason, suspTutor)
		f.count(t, 2, "SELECT count(*) FROM user_violations WHERE user_id=$1", fixture(suspTutor))
		f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1", fixture(suspTutor))
		pastAccepted := f.book(t, true, true)
		pastRequested := f.book(t, false, false)
		boundary := f.next
		var ids []string
		for _, asTutor := range []bool{true, false} {
			for _, accepted := range []bool{false, true} {
				ids = append(ids, f.book(t, asTutor, accepted))
			}
		}
		f.setTime(time.Duration(boundary) * time.Hour)
		evidence := f.snapshot(t, suspensionEvidenceTables...)
		beforeSessions := f.snapshot(t, "tutoring_sessions")
		got := f.decide(t, suspCoordinator, suspTutor, true, 200)
		if got["cancelled_sessions"] != float64(4) || got["is_suspended"] != true || len(got) != 5 {
			t.Fatal(got)
		}
		decision := got["decision_id"].(string)
		f.assertDecision(t, decision, suspTutor, suspCoordinator, true, 4)
		f.count(t, 2, "SELECT count(*) FROM tutoring_sessions WHERE id IN ($1,$2) AND status IN ('REQUESTED','ACCEPTED')", pastAccepted, pastRequested)
		f.count(t, 4, "SELECT count(*) FROM session_participant_blocks WHERE session_id IN($1,$2)", pastAccepted, pastRequested)
		if evidence != f.snapshot(t, suspensionEvidenceTables...) {
			t.Fatal("evidence altered")
		}
		if beforeSessions == f.snapshot(t, "tutoring_sessions") {
			t.Fatal("no sessions cancelled")
		}
		for _, id := range ids {
			f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
		}
		before := f.snapshot(t, suspensionStateTables...)
		f.decide(t, suspCoordinator, suspTutor, true, 409)
		if before != f.snapshot(t, suspensionStateTables...) {
			t.Fatal("duplicate decision")
		}
		for _, v := range []struct {
			method, path string
			body         any
		}{
			{"GET", "/auth/me", nil}, {"GET", "/sessions/me", nil},
			{"POST", "/sessions", map[string]any{}}, {"POST", tutorRequestsPath + ids[0] + "/accept", testMeeting},
			{"POST", tutorRequestsPath + ids[0] + "/decline", testReason}, {"POST", "/tutors/me/sessions/" + pastAccepted + "/complete", nil},
			{"POST", "/tutors/me/availability/slots", map[string]any{}}, {"POST", "/tutor-applications", map[string]any{}},
			{"POST", "/sessions/" + completed + "/feedback", map[string]any{"rating": 5}},
			{"POST", "/issues", map[string]string{"description": "blocked"}},
		} {
			f.a.call(t, 403, v.method, v.path, v.body, f.cookie[suspTutor])
		}
		f.a.call(t, 403, "POST", "/auth/login", map[string]string{"email": fixture(suspTutor) + "@uni.example", "password": password}, nil)
		f.a.call(t, 404, "GET", "/tutors/"+fixture(suspTutor), nil, f.cookie[suspStudent])
		// The other participant can rebook the released boundary time elsewhere.
		rebook := f.request(t, 201, suspStudent, time.Duration(boundary+4)*time.Hour, 5322, 5332)
		f.request(t, 409, suspStudent, time.Duration(boundary+4)*time.Hour, 5322, 5332)
		f.session(t, 200, "POST", policyPath(rebook), policyReason, suspOtherCoordinator)
		// Remove independent eligibility before reinstatement: it must stay removed.
		f.exec(t, "UPDATE tutor_offerings SET is_active=false,deactivated_at=now(),deactivation_reason='Fixture withdrawal' WHERE tutor_id=$1", fixture(suspTutor))
		got = f.decide(t, suspCoordinator, suspTutor, false, 200)
		f.assertDecision(t, got["decision_id"].(string), suspTutor, suspCoordinator, false, 0)
		f.a.call(t, 200, "GET", "/auth/me", nil, f.cookie[suspTutor]) // same unexpired cookie, live PostgreSQL permissions
		f.count(t, 0, "SELECT count(*) FROM tutor_offerings WHERE tutor_id=$1 AND is_active", fixture(suspTutor))
		f.request(t, 422, suspStudent, time.Duration(f.next)*time.Hour, 5321, 5331)
		f.count(t, 4, "SELECT count(*) FROM session_status_history WHERE suspension_history_id=$1", decision)
		before = f.snapshot(t, suspensionStateTables...)
		f.decide(t, suspCoordinator, suspTutor, false, 409)
		if before != f.snapshot(t, suspensionStateTables...) {
			t.Fatal("duplicate reinstatement")
		}
		f.exec(t, "UPDATE tutor_offerings SET is_active=true,deactivated_at=NULL,deactivation_reason=NULL WHERE tutor_id=$1", fixture(suspTutor)) // restore only test setup
		if evidence != f.snapshot(t, suspensionEvidenceTables...) {
			t.Fatal("reinstatement erased evidence")
		}
	})
	t.Run("pending verification no sessions and unicode reason", func(t *testing.T) {
		for _, suspend := range []bool{true, false} {
			w := f.a.call(t, 200, "POST", suspensionPath(fixture(suspPending), suspend), map[string]string{"reason": strings.Repeat("é", 1000)}, f.cookie[suspCoordinator])
			if fields(t, w)["data"].(map[string]any)["cancelled_sessions"] != float64(0) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Body.String())
			}
		}
		f.a.call(t, 200, "GET", "/auth/me", nil, f.cookie[suspPending])
		f.a.call(t, 403, "POST", "/sessions", map[string]any{}, f.cookie[suspPending])
		f.a.call(t, 403, "POST", "/tutor-applications", map[string]any{}, f.cookie[suspPending])
	})
	t.Run("atomic rollback across multiple sessions and reinstatement", func(t *testing.T) {
		var ids []string
		for i := 0; i < 3; i++ {
			ids = append(ids, f.book(t, i%2 == 0, true))
		}
		sort.Strings(ids)
		for _, v := range []struct{ table, event, condition string }{
			{"user_suspension_history", "INSERT", "true"},
			{"notifications", "INSERT", "NEW.type='USER_SUSPENDED'"},
			{"session_status_history", "INSERT", "NEW.cancellation_origin='SUSPENSION' AND NEW.session_id='" + ids[2] + "'"},
			{"notifications", "INSERT", "NEW.type='SESSION_CANCELLED' AND NEW.session_id='" + ids[2] + "'"},
		} {
			before := f.snapshot(t, suspensionStateTables...)
			f.exec(t, `CREATE FUNCTION reject_suspension_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+v.condition+` THEN RAISE EXCEPTION 'private failure evidence'; END IF; RETURN NEW; END; $$; CREATE TRIGGER reject_suspension_test BEFORE `+v.event+` ON `+v.table+` FOR EACH ROW EXECUTE FUNCTION reject_suspension_test()`)
			f.decide(t, suspCoordinator, suspTutor, true, 500)
			f.exec(t, "DROP TRIGGER reject_suspension_test ON "+v.table+"; DROP FUNCTION reject_suspension_test()")
			if before != f.snapshot(t, suspensionStateTables...) {
				t.Fatal("partial commit on", v.table)
			}
		}
		f.decide(t, suspCoordinator, suspTutor, true, 200)
		for _, table := range []string{"user_suspension_history", "notifications"} {
			before := f.snapshot(t, suspensionStateTables...)
			f.exec(t, `CREATE FUNCTION reject_suspension_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private failure evidence'; END; $$; CREATE TRIGGER reject_suspension_test BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION reject_suspension_test()`)
			f.decide(t, suspCoordinator, suspTutor, false, 500)
			f.exec(t, "DROP TRIGGER reject_suspension_test ON "+table+"; DROP FUNCTION reject_suspension_test()")
			if before != f.snapshot(t, suspensionStateTables...) {
				t.Fatal("partial reinstatement")
			}
		}
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
	t.Run("more than one page all cancelled", func(t *testing.T) {
		for i := 0; i < 105; i++ {
			f.book(t, i%2 == 0, i%3 == 0)
		}
		got := f.decide(t, suspCoordinator, suspTutor, true, 200)
		if got["cancelled_sessions"] != float64(105) {
			t.Fatal(got)
		}
		f.assertDecision(t, got["decision_id"].(string), suspTutor, suspCoordinator, true, 105)
		f.decide(t, suspCoordinator, suspTutor, false, 200)
	})
	f.testSuspensionRaces(t)
	f.testSuspensionAuthority(t)
	f.testAuditHistory(t)
	f.testFlaggedUsers(t)
	for _, private := range []string{"private suspension evidence", "private failure evidence", "19.37"} {
		if strings.Contains(f.logs.String(), private) {
			t.Fatal("private log", private)
		}
	}
}

func (f *suspensionFixture) assertDecision(t *testing.T, id string, target, actor int, suspend bool, cancelled int) {
	t.Helper()
	f.count(t, 1, `SELECT count(*) FROM user_suspension_history h JOIN users u ON u.id=h.user_id
		WHERE h.id=$1 AND h.user_id=$2 AND h.changed_by_coordinator_id=$3 AND h.is_suspended=$4 AND h.was_suspended<>h.is_suspended
		AND h.reason=$5 AND u.is_suspended=h.is_suspended AND h.changed_at=$6`, id, fixture(target), fixture(actor), suspend, strings.TrimSpace(suspensionReason["reason"]), time.UnixMicro(f.clock.Load()))
	kind := "USER_REINSTATED"
	if suspend {
		kind = "USER_SUSPENDED"
	}
	f.count(t, 1, `SELECT count(*) FROM notifications WHERE recipient_user_id=$1 AND type::text=$2 AND deduplication_key=$3 AND email_status='PENDING' AND strpos(body,'private suspension evidence')=0`, fixture(target), kind, "account-decision:"+id)
	f.count(t, cancelled, `SELECT count(*) FROM session_status_history h JOIN tutoring_sessions s ON s.id=h.session_id JOIN user_suspension_history a ON a.id=h.suspension_history_id
		WHERE a.id=$1 AND a.is_suspended AND h.cancellation_origin='SUSPENSION' AND h.changed_at=a.changed_at
		AND h.changed_by_user_id=a.changed_by_coordinator_id AND h.actor_kind='USER' AND h.to_status='CANCELLED'
		AND s.status='CANCELLED' AND NOT s.is_late_cancellation AND s.cancelled_at=a.changed_at
		AND s.cancelled_by_user_id=a.changed_by_coordinator_id AND h.reason=s.cancellation_reason
		AND s.cancellation_reason='Session cancelled following an account suspension.'`, id)
	f.count(t, 2*cancelled, `SELECT count(*) FROM notifications n JOIN session_status_history h ON h.session_id=n.session_id
		WHERE h.suspension_history_id=$1 AND n.type='SESSION_CANCELLED' AND n.email_status='PENDING' AND strpos(n.body,'private suspension evidence')=0`, id)
}
