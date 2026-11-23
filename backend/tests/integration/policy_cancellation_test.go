package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coord "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	feedbackpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/infrastructure/postgres"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	policyStudent            = 4201
	policyTutor              = 4202
	policyOtherStudent       = 4203
	policyOtherTutor         = 4204
	policyPending            = 4205
	policyCoordinator        = 4206
	policyOtherCoordinator   = 4207
	policyMutableCoordinator = 4208
)

type policyFixture struct {
	*bookingFixture
	logs  bytes.Buffer
	next  int
	redis *redis.Client
}

func policyPath(id string) string { return "/coordinator/sessions/" + id + "/cancel" }

var policyReason = map[string]string{"reason": "  Policy review — private cancellation evidence  "}

func newPolicyFixture(t *testing.T) *policyFixture {
	t.Helper()
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_docker.ps1 -PolicyCancellationOnly")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["application_name"] = "tutti_policy_integration"
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	hasher, err := security.NewPasswords(security.Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	idService, err := identity.New(ctx, identitypg.New(db), sessionredis.New(client), hasher, identity.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f := &policyFixture{bookingFixture: &bookingFixture{ctx: ctx, db: db, start: time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), cookie: map[int]*http.Cookie{}}, next: 8, redis: client}
	f.setTime(0)
	now := func() time.Time { return time.UnixMicro(f.clock.Load()) }
	logger := slog.New(slog.NewTextHandler(&f.logs, nil))
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, logger)
	auth.RegisterRoutes(mux)
	f.service = scheduling.NewBooking(schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{
		LockStudent: identitypg.LockStudent, Catalog: func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) }, Offerings: tutoringpg.BookingOfferings, Discipline: coordpg.Discipline}), now)
	schedulinghttp.NewBooking(f.service, logger).RegisterRoutes(mux, auth)
	policy := coord.NewPolicyCancellation(coordpg.NewPolicyCancellation(db, coordpg.PolicyCancellationDependencies{LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, Sessions: schedulingpg.PolicySessions}), now)
	coordhttp.NewPolicyCancellation(policy, logger).RegisterRoutes(mux, auth)
	coordhttp.NewMonitoring(coord.NewMonitoring(coordpg.NewMonitoring(db, coordpg.MonitoringDependencies{
		Faculty: identitypg.ReadCoordinatorFaculty, Tutors: tutoringpg.Monitoring, Activity: feedbackpg.Monitoring, Scheduling: schedulingpg.Monitoring})), logger).RegisterRoutes(mux, auth)
	attendance := coordpg.NewAttendance(db, coordpg.AttendanceDependencies{LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty,
		HomeFaculty: func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
			return catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program)
		}, Sessions: schedulingpg.AttendanceSessions})
	coordhttp.New(coord.NewAttendance(attendance, now), logger).RegisterRoutes(mux, auth)
	f.a = api{mux}
	for i := 1; i <= 4; i++ {
		f.exec(t, "INSERT INTO faculties(id,name) VALUES($1,$2)", fixture(4100+i), fmt.Sprintf("Policy faculty %d", i))
	}
	for i := 1; i <= 3; i++ {
		f.exec(t, "INSERT INTO programs VALUES($1,$2,$3,'BACHELOR')", fixture(4110+i), fixture(4100+i), fmt.Sprintf("Policy program %d", i))
	}
	f.exec(t, "INSERT INTO subjects VALUES($1,'POLICY','Policy fixture subject')", fixture(4121))
	for i := 1; i <= 3; i++ {
		f.exec(t, "INSERT INTO program_subjects VALUES($1,$2)", fixture(4110+i), fixture(4121))
	}
	f.exec(t, "INSERT INTO semesters VALUES($1,'Policy term','2028-01-01T00:00Z','2028-02-01T00:00Z')", fixture(4131))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	for id := policyStudent; id <= policyMutableCoordinator; id++ {
		f.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES($1,$2,$3,$4)", fixture(id), "Policy "+fixture(id), fixture(id)+"@uni.example", hash)
	}
	for i, id := range []int{policyCoordinator, policyOtherCoordinator, policyMutableCoordinator} {
		f.exec(t, "INSERT INTO coordinators VALUES($1,$2,$3)", fixture(id), fixture(id), fixture(4101+i))
	}
	for id := policyStudent; id <= policyPending; id++ {
		program, faculty, reviewer := 4111, 4101, policyCoordinator
		if id == policyStudent || id == policyOtherTutor {
			program, faculty, reviewer = 4112, 4102, policyOtherCoordinator
		}
		f.exec(t, "INSERT INTO students VALUES($1,$2,$3)", fixture(id), fixture(id), fixture(program))
		if id != policyPending {
			f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$3)", fixture(id), fixture(faculty), fixture(reviewer))
		}
	}
	for i, id := range []int{policyTutor, policyOtherTutor} {
		f.exec(t, "INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Policy tutor',now())", fixture(id))
		f.exec(t, "INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,$3,'Private grade evidence','APPROVED',now(),$4)", fixture(4301+i), fixture(id), fixture(4101+i), fixture(policyCoordinator+i))
		f.exec(t, "INSERT INTO tutor_qualifications(id,application_id,student_id,subject_id,grade) VALUES($1,$2,$3,$4,19.37)", fixture(4311+i), fixture(4301+i), fixture(id), fixture(4121))
		f.exec(t, "INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Policy offering')", fixture(4321+i), fixture(id), fixture(4121), fixture(4311+i))
		f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,'2028-01-01T00:00Z','2028-02-01T00:00Z','BOTH')", fixture(4331+i), fixture(id))
	}
	for id := policyStudent; id <= policyMutableCoordinator; id++ {
		f.cookie[id] = f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": fixture(id) + "@uni.example", "password": password}, nil).Result().Cookies()[0]
	}
	return f
}

func (f *policyFixture) book(t *testing.T, accepted bool) string {
	t.Helper()
	f.setTime(0)
	id := f.request(t, 201, policyStudent, time.Duration(f.next)*time.Hour, 4321, 4331)
	f.next += 2
	if accepted {
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, policyTutor)
	}
	return id
}
func (f *policyFixture) state(t *testing.T, id string) string {
	t.Helper()
	var state string
	err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_array(
		(SELECT to_jsonb(s) FROM tutoring_sessions s WHERE id=$1),
		(SELECT jsonb_agg(to_jsonb(h) ORDER BY h.id) FROM session_status_history h WHERE session_id=$1),
		(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.user_id) FROM session_participant_blocks b WHERE session_id=$1),
		(SELECT jsonb_agg(to_jsonb(n) ORDER BY n.id) FROM notifications n WHERE session_id=$1))::text`, id).Scan(&state)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func (f *policyFixture) cancelled(t *testing.T, id string, actor int, reason, from string) {
	t.Helper()
	f.count(t, 1, `SELECT count(*) FROM tutoring_sessions s JOIN session_status_history h ON h.session_id=s.id
		WHERE s.id=$1 AND s.status='CANCELLED' AND NOT s.is_late_cancellation AND s.cancelled_by_user_id=$2
		AND s.cancellation_reason=$3 AND h.from_status::text=$4 AND h.to_status='CANCELLED' AND h.actor_kind='USER'
		AND h.changed_by_user_id=s.cancelled_by_user_id AND h.reason=s.cancellation_reason AND h.changed_at=s.cancelled_at
		AND h.cancellation_origin='COORDINATOR_POLICY'`, id, fixture(actor), strings.TrimSpace(reason), from)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", id)
	f.count(t, 2, `SELECT count(*) FROM notifications n JOIN tutoring_sessions s ON s.id=n.session_id
		WHERE n.session_id=$1 AND n.type='SESSION_CANCELLED' AND n.email_status='PENDING'
		AND n.recipient_user_id IN(s.student_id,s.tutor_id) AND n.deduplication_key='session:'||s.id||':SESSION_CANCELLED'
		AND strpos(n.body,$2)=0`, id, strings.TrimSpace(reason))
}

func TestPolicyCancellation(t *testing.T) {
	f := newPolicyFixture(t)
	t.Run("authorization validation and no partial effects", func(t *testing.T) {
		id := f.book(t, false)
		before := f.state(t, id)
		for _, v := range []struct{ actor, code int }{{0, 401}, {policyStudent, 403}, {policyTutor, 403}, {policyPending, 403}, {policyOtherCoordinator, 404}} {
			f.a.call(t, v.code, "POST", policyPath(id), policyReason, f.cookie[v.actor])
		}
		missing := f.a.call(t, 404, "POST", policyPath(fixture(999999)), policyReason, f.cookie[policyCoordinator])
		foreign := f.a.call(t, 404, "POST", policyPath(id), policyReason, f.cookie[policyOtherCoordinator])
		if missing.Body.String() != foreign.Body.String() {
			t.Fatal("session enumeration")
		}
		for _, from := range []string{"", "https://attacker.example"} {
			if w := f.a.request("POST", policyPath(id), from, policyReason, f.cookie[policyCoordinator]); w.Code != 403 {
				t.Fatal(w.Code)
			}
		}
		for _, v := range []struct {
			body any
			code int
		}{
			{map[string]any{}, 422}, {map[string]any{"reason": nil}, 422}, {map[string]string{"reason": " \t\u2003"}, 422},
			{map[string]string{"reason": "a\x00b"}, 422}, {map[string]string{"reason": strings.Repeat("é", 1001)}, 422},
			{map[string]int{"reason": 5}, 400}, {map[string]string{"reason": "valid", "faculty_id": fixture(4101)}, 400},
			{map[string]string{"reason": "valid", "coordinator_id": fixture(policyCoordinator)}, 400},
		} {
			f.a.call(t, v.code, "POST", policyPath(id), v.body, f.cookie[policyCoordinator])
		}
		for _, raw := range [][]byte{[]byte(`{"reason":`), []byte(`{"reason":"x"} {}`), []byte("{\"reason\":\"\xff\"}"), []byte("null")} {
			r := httptest.NewRequest("POST", origin+"/api/v1"+policyPath(id), bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Origin", origin)
			r.AddCookie(f.cookie[policyCoordinator])
			w := httptest.NewRecorder()
			f.a.handler.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		f.a.call(t, 413, "POST", policyPath(id), map[string]string{"reason": strings.Repeat("a", 17000)}, f.cookie[policyCoordinator])
		f.a.call(t, 422, "POST", policyPath("bad"), policyReason, f.cookie[policyCoordinator])
		f.a.call(t, 400, "POST", policyPath(id)+"?faculty_id="+fixture(4101), policyReason, f.cookie[policyCoordinator])
		f.a.call(t, 403, "POST", "/sessions/"+id+"/cancel", policyReason, f.cookie[policyCoordinator])
		if f.state(t, id) != before {
			t.Fatal("invalid/unauthorized action changed durable state")
		}
		f.a.call(t, 200, "POST", policyPath(id), map[string]string{"reason": strings.Repeat("é", 1000)}, f.cookie[policyCoordinator])
		f.cancelled(t, id, policyCoordinator, strings.Repeat("é", 1000), "REQUESTED")
	})
	t.Run("requested accepted reservations history privacy and no strikes", func(t *testing.T) {
		// Give the student one existing late cancellation: policy action must not
		// cross the two-incident threshold, even inside 24 hours.
		prior := f.book(t, false)
		f.session(t, 200, "POST", "/sessions/"+prior+"/cancel", testReason, policyStudent)
		f.count(t, 1, "SELECT count(*) FROM user_violations WHERE user_id=$1", fixture(policyStudent))
		for _, accepted := range []bool{false, true} {
			id := f.book(t, accepted)
			f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
			var identityBefore string
			err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_array(student_id,tutor_id,offering_id,faculty_id,semester_id,starts_at,ends_at,requested_at,description)::text FROM tutoring_sessions WHERE id=$1`, id).Scan(&identityBefore)
			if err != nil {
				t.Fatal(err)
			}
			w := f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
			got := fields(t, w)["data"].(map[string]any)
			if got["is_late_cancellation"] != false || got["cancellation_origin"] != "COORDINATOR_POLICY" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(got)
			}
			for _, private := range []string{"student_id", "tutor_id", "meeting_link", "meeting_room", "description", "grade", "19.37", "@uni.example"} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("leaked", private)
				}
			}
			from, histories := "REQUESTED", 2
			if accepted {
				from, histories = "ACCEPTED", 3
			}
			f.cancelled(t, id, policyCoordinator, policyReason["reason"], from)
			f.count(t, histories, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
			f.count(t, 1, `SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND jsonb_build_array(student_id,tutor_id,offering_id,faculty_id,semester_id,starts_at,ends_at,requested_at,description)::text=$2`, id, identityBefore)
			for _, actor := range []int{policyStudent, policyTutor} {
				if f.session(t, 200, "GET", "/sessions/"+id, nil, actor)["cancellation_reason"] != strings.TrimSpace(policyReason["reason"]) {
					t.Fatal("reason unavailable to participant")
				}
			}
			before := f.state(t, id)
			f.a.call(t, 409, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
			if before != f.state(t, id) {
				t.Fatal("duplicate changed history")
			}
			// Reuse the released interval and verify that new overlaps still fail.
			at := time.Duration(f.next-2) * time.Hour
			rebook := f.request(t, 201, policyStudent, at, 4321, 4331)
			f.request(t, 409, policyStudent, at, 4322, 4332)
			f.a.call(t, 200, "POST", policyPath(rebook), policyReason, f.cookie[policyCoordinator])
		}
		f.count(t, 1, "SELECT count(*) FROM user_violations WHERE user_id=$1", fixture(policyStudent))
		f.count(t, 0, "SELECT count(*) FROM user_semester_flags WHERE user_id IN($1,$2)", fixture(policyStudent), fixture(policyTutor))
		f.count(t, 0, "SELECT count(*) FROM users WHERE id IN($1,$2) AND is_suspended", fixture(policyStudent), fixture(policyTutor))
		stats := f.session(t, 200, "GET", "/coordinator/statistics", nil, policyCoordinator)
		if stats["completed_sessions"] != float64(0) || stats["rating_count"] != float64(0) {
			t.Fatal(stats)
		}
	})
	t.Run("history and outbox rollback", func(t *testing.T) {
		for _, table := range []string{"session_status_history", "notifications"} {
			id := f.book(t, true)
			before := f.state(t, id)
			condition := "NEW.to_status='CANCELLED'"
			if table == "notifications" {
				condition = "NEW.type='SESSION_CANCELLED' AND NEW.recipient_user_id='" + fixture(policyTutor) + "'"
			}
			f.exec(t, `CREATE FUNCTION reject_policy_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF `+condition+` THEN RAISE EXCEPTION 'private policy fixture failure'; END IF; RETURN NEW; END; $$;
				CREATE TRIGGER reject_policy_test BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION reject_policy_test()`)
			w := f.a.call(t, 500, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
			if strings.Contains(w.Body.String(), "fixture") {
				t.Fatal("SQL error exposed")
			}
			if f.state(t, id) != before {
				t.Fatal("partial cancellation after", table, "failure")
			}
			f.exec(t, "DROP TRIGGER reject_policy_test ON "+table+"; DROP FUNCTION reject_policy_test()")
			f.a.call(t, 200, "POST", policyPath(id), policyReason, f.cookie[policyCoordinator])
			f.cancelled(t, id, policyCoordinator, policyReason["reason"], "ACCEPTED")
		}
	})
	f.testPolicyRaces(t)
	f.testPolicyAuthority(t)
	for _, secret := range []string{"private cancellation evidence", "private policy fixture failure", "19.37"} {
		if strings.Contains(f.logs.String(), secret) {
			t.Fatal("private log", secret)
		}
	}
}
