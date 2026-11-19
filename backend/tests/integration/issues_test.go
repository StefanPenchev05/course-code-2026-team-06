package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
)

const (
	issueStudentA      = 1801
	issueTutorA        = 1802
	issueStudentB      = 1803
	issueTutorB        = 1804
	issuePending       = 1805
	issueRejected      = 1806
	issueCoordinatorA  = 1807
	issueCoordinatorB  = 1808
	issueRoleless      = 1809
	issueNoCoordinator = 1811
	issueSuspended     = 1812
	issueClosed        = 1813
)

type issueFixture struct {
	ctx     context.Context
	db      *pgxpool.Pool
	a       api
	cookies map[int]*http.Cookie
	logs    *bytes.Buffer
}

func newIssueFixture(t *testing.T) *issueFixture {
	t.Helper()
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run test_docker.ps1 -IssuesOnly for disposable PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["application_name"] = "tutti_issue_integration"
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
	identityService, err := identity.New(ctx, identitypg.New(db), sessionredis.New(client), hasher,
		identity.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	logs := new(bytes.Buffer)
	logger := slog.New(slog.NewTextHandler(logs, nil))
	auth := identityhttp.New(identityService, origin, true, logger)
	mux := http.NewServeMux()
	auth.RegisterRoutes(mux)
	repo := coordpg.NewIssues(db, coordpg.IssueDependencies{
		LockStudent: identitypg.LockStudent,
		HomeFaculty: func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
			return catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program)
		},
		LockParticipantSession: func(ctx context.Context, tx pgx.Tx, id, actor string) (scheduling.Session, error) {
			return schedulingpg.AttendanceSessions(tx).LockParticipantSession(ctx, id, actor)
		},
	})
	coordhttp.NewIssues(coordination.NewIssues(repo), logger).RegisterRoutes(mux, auth)
	f := &issueFixture{ctx: ctx, db: db, a: api{mux}, cookies: map[int]*http.Cookie{}, logs: logs}
	for n := 1; n <= 3; n++ {
		f.exec(t, "INSERT INTO faculties(id,name) VALUES($1,$2)", fixture(1700+n), fmt.Sprintf("Issue faculty %d", n))
		f.exec(t, "INSERT INTO programs VALUES($1,$2,$3,'BACHELOR')", fixture(1710+n), fixture(1700+n), fmt.Sprintf("Issue program %d", n))
	}
	f.exec(t, "INSERT INTO subjects VALUES($1,'ISSUE','Issue subject')", fixture(1721))
	f.exec(t, "INSERT INTO program_subjects VALUES($1,$3),($2,$3)", fixture(1711), fixture(1712), fixture(1721))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	users := []int{issueStudentA, issueTutorA, issueStudentB, issueTutorB, issuePending, issueRejected,
		issueCoordinatorA, issueCoordinatorB, issueRoleless, issueNoCoordinator, issueSuspended, issueClosed}
	for _, id := range users {
		f.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES($1,$2,$3,$4)", fixture(id), "Issue fixture", fmt.Sprintf("issue-%d@uni.example", id), hash)
	}
	f.exec(t, "INSERT INTO coordinators VALUES($1,'ISSUE-C1',$3),($2,'ISSUE-C2',$4)", fixture(issueCoordinatorA), fixture(issueCoordinatorB), fixture(1701), fixture(1702))
	for _, id := range users {
		if id == issueCoordinatorA || id == issueCoordinatorB || id == issueRoleless {
			continue
		}
		faculty, program, coordinator := 1701, 1711, issueCoordinatorA
		if id == issueStudentB || id == issueTutorB {
			faculty, program, coordinator = 1702, 1712, issueCoordinatorB
		}
		f.exec(t, "INSERT INTO students VALUES($1,$2,$3)", fixture(id), fmt.Sprintf("ISSUE-%d", id), fixture(program))
		switch id {
		case issuePending:
			f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2)", fixture(id), fixture(faculty))
		case issueRejected:
			f.exec(t, `INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id,rejection_reason)
				VALUES($1,$2,'REJECTED',now(),$3,'Fixture rejection')`, fixture(id), fixture(faculty), fixture(coordinator))
		default:
			f.exec(t, `INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
				VALUES($1,$2,'APPROVED',now(),$3)`, fixture(id), fixture(faculty), fixture(coordinator))
		}
	}
	// An approved student may move to a program whose coordinator is unassigned.
	f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(issueNoCoordinator), fixture(1713))
	for i, tutor := range []int{issueTutorA, issueTutorB} {
		f.exec(t, "INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Issue tutor',now())", fixture(tutor))
		f.exec(t, `INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES($1,$2,$3,'Issue application','APPROVED',now(),$4)`, fixture(1861+i), fixture(tutor), fixture(1701+i), fixture(issueCoordinatorA+i))
		f.exec(t, "INSERT INTO tutor_qualifications VALUES($1,$2,$3,18.00,$4)", fixture(1871+i), fixture(1861+i), fixture(1721), fixture(tutor))
		f.exec(t, "INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Issue offering')", fixture(1881+i), fixture(tutor), fixture(1721), fixture(1871+i))
		f.exec(t, `INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode)
			VALUES($1,$2,'2030-01-01 UTC','2030-01-02 UTC','BOTH')`, fixture(1891+i), fixture(tutor))
	}
	f.exec(t, "INSERT INTO session_policies(id) VALUES($1)", fixture(1722))
	f.exec(t, "INSERT INTO semesters VALUES($1,'Issue semester','2030-01-01 UTC','2030-02-01 UTC')", fixture(1723))
	for i, pair := range [][2]int{{issueStudentA, issueTutorA}, {issueStudentB, issueTutorB}, {issueStudentB, issueTutorA}, {issueTutorA, issueTutorB}} {
		tutorIndex := 0
		if pair[1] == issueTutorB {
			tutorIndex = 1
		}
		start := time.Date(2030, 1, 1, 8+2*i, 0, 0, 0, time.UTC)
		f.exec(t, `INSERT INTO tutoring_sessions(id,student_id,tutor_id,offering_id,availability_slot_id,policy_id,semester_id,
			faculty_id,starts_at,ends_at,duration_minutes,mode,purpose,description,requested_at,expires_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,60,'ONLINE','HOMEWORK','Issue session',$11,$12)`,
			fixture(1901+i), fixture(pair[0]), fixture(pair[1]), fixture(1881+tutorIndex), fixture(1891+tutorIndex),
			fixture(1722), fixture(1723), fixture(1701+tutorIndex), start, start.Add(time.Hour), start.Add(-8*time.Hour), start.Add(-7*time.Hour))
	}
	for _, id := range users {
		code := 200
		if id == issueRoleless {
			code = 403
		}
		w := f.a.call(t, code, "POST", "/auth/login", map[string]string{"email": fmt.Sprintf("issue-%d@uni.example", id), "password": password}, nil)
		if code == 200 {
			f.cookies[id] = w.Result().Cookies()[0]
		}
	}
	f.exec(t, `UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Fixture' WHERE id=$1`, fixture(issueSuspended), fixture(issueCoordinatorA))
	f.exec(t, "UPDATE users SET closed_at=now() WHERE id=$1", fixture(issueClosed))
	return f
}

func (f *issueFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *issueFixture) counts(t *testing.T) (int, int) {
	t.Helper()
	var reports, history int
	if err := f.db.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM issue_reports WHERE reporter_user_id IN (SELECT id FROM users WHERE email LIKE 'issue-%@uni.example')),
		(SELECT count(*) FROM issue_status_history h JOIN issue_reports r ON r.id=h.issue_report_id
		 JOIN users u ON u.id=r.reporter_user_id WHERE u.email LIKE 'issue-%@uni.example')`).Scan(&reports, &history); err != nil {
		t.Fatal(err)
	}
	return reports, history
}
func (f *issueFixture) assertCreated(t *testing.T, w *httptest.ResponseRecorder, actor, faculty int, session *string) string {
	t.Helper()
	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Data coordomain.Issue `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	r := response.Data
	if r.ID == "" || r.ReporterID != fixture(actor) || r.FacultyID != fixture(faculty) || r.Status != coordomain.IssueOpen || r.CreatedAt.IsZero() || (session == nil) != (r.SessionID == nil) {
		t.Fatalf("wrong issue response: %+v", r)
	}
	if session != nil && *session != *r.SessionID {
		t.Fatal("wrong session")
	}
	var auditOK bool
	err := f.db.QueryRow(f.ctx, `SELECT count(*)=1 AND bool_and(h.from_status IS NULL AND h.to_status='OPEN'
		AND h.actor_kind='USER' AND h.changed_by_user_id=r.reporter_user_id AND h.changed_at=r.created_at
		AND r.reporter_user_id=$2 AND r.faculty_id=$3 AND r.session_id IS NOT DISTINCT FROM $4::uuid
		AND r.description=$5 AND r.created_at=$6)
		FROM issue_reports r JOIN issue_status_history h ON h.issue_report_id=r.id WHERE r.id=$1`,
		r.ID, fixture(actor), fixture(faculty), session, r.Description, r.CreatedAt).Scan(&auditOK)
	if err != nil || !auditOK {
		t.Fatal("incorrect report/audit persistence", err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private response is cacheable")
	}
	return r.ID
}

func TestIssueReporting(t *testing.T) {
	f := newIssueFixture(t)
	valid := map[string]any{"description": "  Private issue — 学生\n"}
	var first string
	t.Run("trusted faculty and participant routing", func(t *testing.T) {
		for _, actor := range []int{issueStudentA, issueTutorA, issueStudentB, issueTutorB, issueNoCoordinator} {
			faculty := 1701
			if actor == issueStudentB || actor == issueTutorB {
				faculty = 1702
			}
			if actor == issueNoCoordinator {
				faculty = 1703
			}
			id := f.assertCreated(t, f.a.request("POST", "/issues", origin, valid, f.cookies[actor]), actor, faculty, nil)
			if actor == issueStudentA {
				first = id
			}
		}
		for _, tc := range []struct{ actor, session, faculty int }{
			{issueStudentA, 1901, 1701}, {issueTutorA, 1901, 1701},
			{issueStudentB, 1903, 1701}, {issueTutorA, 1903, 1701}, {issueTutorA, 1904, 1702},
		} {
			id := fixture(tc.session)
			f.assertCreated(t, f.a.request("POST", "/issues", origin, map[string]any{"description": "Linked issue", "session_id": strings.ToUpper(id)}, f.cookies[tc.actor]), tc.actor, tc.faculty, &id)
		}
		f.assertCreated(t, f.a.request("POST", "/issues", origin, map[string]any{"description": "Null optional link", "session_id": nil}, f.cookies[issueStudentA]), issueStudentA, 1701, nil)
		// General issues remain possible for terminal sessions, independent of no-show rules.
		f.exec(t, "UPDATE tutoring_sessions SET status='DECLINED',decline_reason='Fixture' WHERE id=$1", fixture(1901))
		id := fixture(1901)
		f.assertCreated(t, f.a.request("POST", "/issues", origin, map[string]any{"description": "After decline", "session_id": id}, f.cookies[issueStudentA]), issueStudentA, 1701, &id)
	})
	t.Run("authorization and private session IDs", func(t *testing.T) {
		before, history := f.counts(t)
		f.a.call(t, 401, "POST", "/issues", valid, nil)
		for _, actor := range []int{issueCoordinatorA, issueCoordinatorB, issuePending, issueRejected, issueSuspended, issueClosed} {
			f.a.call(t, 403, "POST", "/issues", valid, f.cookies[actor])
		}
		for _, from := range []string{"", "https://evil.example"} {
			if w := f.a.request("POST", "/issues", from, valid, f.cookies[issueStudentA]); w.Code != 403 {
				t.Fatal(w.Code)
			}
		}
		for _, actor := range []int{issueStudentA, issueTutorA} {
			unrelated := f.a.call(t, 404, "POST", "/issues", map[string]any{"description": "Issue", "session_id": fixture(1902)}, f.cookies[actor])
			missing := f.a.call(t, 404, "POST", "/issues", map[string]any{"description": "Issue", "session_id": fixture(9999)}, f.cookies[actor])
			if unrelated.Body.String() != missing.Body.String() {
				t.Fatal("session existence disclosed")
			}
		}
		// No report reads, edits, deletes or coordinator inbox APIs are published in this slice.
		for _, actor := range []int{issueStudentA, issueStudentB, issueCoordinatorA, issueCoordinatorB} {
			for _, method := range []string{"GET", "PATCH", "DELETE"} {
				f.a.call(t, 404, method, "/issues/"+first, nil, f.cookies[actor])
			}
			f.a.call(t, 404, "GET", "/coordinator/issues/"+first, nil, f.cookies[actor])
		}
		after, afterHistory := f.counts(t)
		if after != before || afterHistory != history {
			t.Fatal("unauthorized request wrote data")
		}
	})
	t.Run("strict bounded description and JSON", func(t *testing.T) {
		before, history := f.counts(t)
		for _, input := range []map[string]any{
			{}, {"description": ""}, {"description": " \t\n\u00a0\u3000"}, {"description": strings.Repeat("é", 1001)},
			{"description": "nul\x00text"}, {"description": "Issue", "session_id": ""}, {"description": "Issue", "session_id": "malformed"},
		} {
			f.a.call(t, 422, "POST", "/issues", input, f.cookies[issueStudentA])
		}
		for _, key := range []string{"reporter_id", "reporter_user_id", "faculty_id", "status", "created_at", "handled_by_coordinator_id", "reported_tutor_id"} {
			f.a.call(t, 400, "POST", "/issues", map[string]any{"description": "Issue", key: fixture(issueStudentB)}, f.cookies[issueStudentA])
		}
		for _, tc := range []struct {
			body, content string
			want          int
		}{
			{"null", "application/json", 400}, {"[]", "application/json", 400}, {`{"description":`, "application/json", 400},
			{`{"description":"Issue"} {}`, "application/json", 400}, {`{"description":12}`, "application/json", 400},
			{`{"description":"Issue","session_id":12}`, "application/json", 400},
			{"{\"description\":\"invalid\xff\"}", "application/json", 400},
			{`{"description":"` + strings.Repeat("x", 17*1024) + `"}`, "application/json", 413},
			{`{"description":"Issue"}`, "text/plain", 415},
		} {
			r := httptest.NewRequest("POST", origin+"/api/v1/issues", strings.NewReader(tc.body))
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", tc.content)
			r.AddCookie(f.cookies[issueStudentA])
			w := httptest.NewRecorder()
			f.a.handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("invalid JSON returned %d, want %d", w.Code, tc.want)
			}
		}
		after, afterHistory := f.counts(t)
		if after != before || afterHistory != history {
			t.Fatal("invalid request persisted")
		}
		f.assertCreated(t, f.a.request("POST", "/issues", origin, map[string]any{"description": strings.Repeat("é", 1000)}, f.cookies[issueStudentA]), issueStudentA, 1701, nil)
	})
	t.Run("audit failure rolls back creation", func(t *testing.T) {
		before, history := f.counts(t)
		f.exec(t, `CREATE FUNCTION issue_test_reject_history() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION 'private database failure'; END; $$;
			CREATE TRIGGER issue_test_reject_history BEFORE INSERT ON issue_status_history
			FOR EACH ROW EXECUTE FUNCTION issue_test_reject_history();`)
		defer f.exec(t, "DROP TRIGGER issue_test_reject_history ON issue_status_history; DROP FUNCTION issue_test_reject_history();")
		w := f.a.call(t, 500, "POST", "/issues", valid, f.cookies[issueStudentA])
		if w.Body.String() != "{\"error\":\"internal server error\"}\n" {
			t.Fatal("database detail disclosed")
		}
		after, afterHistory := f.counts(t)
		if after != before || afterHistory != history {
			t.Fatal("report survived failed audit")
		}
	})
	t.Run("append-only history and referential integrity", func(t *testing.T) {
		for _, tc := range []struct {
			sql, state string
			args       []any
		}{
			{"UPDATE issue_status_history SET changed_by_user_id=$2 WHERE issue_report_id=$1", "23514", []any{first, fixture(issueStudentB)}},
			{"DELETE FROM issue_status_history WHERE issue_report_id=$1", "23514", []any{first}},
			{"TRUNCATE issue_status_history", "23514", nil},
			{"DELETE FROM issue_reports WHERE id=$1", "23503", []any{first}},
			{`INSERT INTO issue_status_history(issue_report_id,to_status,actor_kind,changed_by_user_id) VALUES($1,'OPEN','USER',$2)`, "23505", []any{first, fixture(issueStudentA)}},
			{`INSERT INTO issue_status_history(issue_report_id,to_status,actor_kind,changed_by_user_id) VALUES($1,'OPEN','USER',$2)`, "23503", []any{fixture(9999), fixture(issueStudentA)}},
		} {
			_, err := f.db.Exec(f.ctx, tc.sql, tc.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.state {
				t.Fatalf("expected %s, got %v", tc.state, err)
			}
		}
		var count int
		if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM issue_status_history WHERE issue_report_id=$1", first).Scan(&count); err != nil || count != 1 {
			t.Fatal("history was changed", err)
		}
	})
	t.Run("concurrent submissions have independent histories", func(t *testing.T) {
		before, history := f.counts(t)
		results := make(chan *httptest.ResponseRecorder, 2)
		start := make(chan struct{})
		for range 2 {
			go func() { <-start; results <- f.a.request("POST", "/issues", origin, valid, f.cookies[issueStudentA]) }()
		}
		close(start)
		x := f.assertCreated(t, <-results, issueStudentA, 1701, nil)
		y := f.assertCreated(t, <-results, issueStudentA, 1701, nil)
		after, afterHistory := f.counts(t)
		if x == y || after != before+2 || afterHistory != history+2 {
			t.Fatal("concurrent audit corruption")
		}
	})
	t.Run("program change is rechecked under lock", func(t *testing.T) {
		w := f.raceChange(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", []any{fixture(issueStudentA), fixture(1712)},
			"SELECT user_id::text FROM students WHERE user_id = $1 FOR SHARE")
		f.assertCreated(t, w, issueStudentA, 1702, nil)
		var assigned string
		if err := f.db.QueryRow(f.ctx, "SELECT faculty_id::text FROM issue_reports WHERE id=$1", first).Scan(&assigned); err != nil || assigned != fixture(1701) {
			t.Fatal("historical ownership changed", err)
		}
	})
	t.Run("suspension winning the lock prevents submission", func(t *testing.T) {
		before, history := f.counts(t)
		w := f.raceChange(t, `UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Race fixture' WHERE id=$1`,
			[]any{fixture(issueStudentA), fixture(issueCoordinatorB)}, "SELECT u.id::text FROM users u WHERE u.id = $1 FOR NO KEY UPDATE")
		if w.Code != 403 {
			t.Fatal("stale authenticated principal accepted", w.Code)
		}
		after, afterHistory := f.counts(t)
		if after != before || afterHistory != history {
			t.Fatal("suspended reporter wrote data")
		}
	})
	for _, private := range []string{"Private issue", "private database failure", "issue-1801@uni.example", password} {
		if strings.Contains(f.logs.String(), private) {
			t.Fatal("logs disclose private report data")
		}
	}
}

func (f *issueFixture) raceChange(t *testing.T, sql string, args []any, blockedQuery string) *httptest.ResponseRecorder {
	t.Helper()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err := tx.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- f.a.request("POST", "/issues", origin, map[string]any{"description": "Concurrent issue"}, f.cookies[issueStudentA])
	}()
	wait, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := f.db.QueryRow(wait, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
			WHERE application_name='tutti_issue_integration' AND query=$1 AND cardinality(pg_blocking_pids(pid))>0)`, blockedQuery).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case response := <-result:
			t.Fatalf("submission did not wait for authorization lock: %d", response.Code)
		case <-ticker.C:
		case <-wait.Done():
			t.Fatal("submission never reached expected lock")
		}
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		return response
	case <-wait.Done():
		t.Fatal("submission did not finish after lock release")
		return nil
	}
}
