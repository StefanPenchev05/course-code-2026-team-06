package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identitydomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

const issueInbox = "/coordinator/issues"
const issueCoordinatorMutable = 1814

func issueResponse(t *testing.T, w *httptest.ResponseRecorder) coordomain.Issue {
	t.Helper()
	if w.Code != 200 && w.Code != 201 {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data coordomain.Issue `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("sensitive response cacheable")
	}
	return body.Data
}

type issuePage struct {
	Data       []coordomain.Issue `json:"data"`
	Pagination struct {
		Limit, Offset int
		HasMore       bool `json:"has_more"`
	} `json:"pagination"`
}

func (f *issueFixture) inbox(t *testing.T, actor int, query string) issuePage {
	t.Helper()
	w := f.a.call(t, 200, "GET", issueInbox+query, nil, f.cookies[actor])
	var page issuePage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Data == nil || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing empty array or privacy header")
	}
	return page
}
func (f *issueFixture) seedIssue(t *testing.T, n, faculty int, legacy bool) string {
	t.Helper()
	id := fixture(n)
	f.exec(t, `INSERT INTO issue_reports(id,reporter_user_id,faculty_id,description,created_at)
		VALUES($1,$2,$3,'Private general issue','2020-01-01 UTC')`, id, fixture(issueStudentB), fixture(faculty))
	if !legacy {
		f.exec(t, `INSERT INTO issue_status_history(issue_report_id,to_status,changed_at,actor_kind,changed_by_user_id)
		SELECT id,status,created_at,'USER',reporter_user_id FROM issue_reports WHERE id=$1`, id)
	}
	return id
}
func (f *issueFixture) issueState(t *testing.T, id string) string {
	t.Helper()
	var state string
	if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_object('report',to_jsonb(i),'history',
		(SELECT jsonb_agg(h ORDER BY h.changed_at,h.id) FROM issue_status_history h WHERE h.issue_report_id=i.id))::text
		FROM issue_reports i WHERE i.id=$1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
func (f *issueFixture) resolved(t *testing.T, w *httptest.ResponseRecorder, before coordomain.Issue, actor int, note string, events int) coordomain.Issue {
	t.Helper()
	r := issueResponse(t, w)
	if r.ID != before.ID || r.Status != coordomain.IssueResolved || r.ResolverID != fixture(actor) || r.Resolution != note || r.ResolvedAt == nil || r.ResolvedAt.Before(before.CreatedAt) {
		t.Fatal("incorrect resolution", r)
	}
	original := r
	original.Status, original.ResolvedAt, original.ResolverID, original.Resolution = before.Status, before.ResolvedAt, before.ResolverID, before.Resolution
	if !reflect.DeepEqual(original, before) {
		t.Fatal("original issue changed")
	}
	var valid bool
	if err := f.db.QueryRow(f.ctx, `SELECT count(*)=$2 AND count(*) FILTER(WHERE from_status='OPEN' AND to_status='RESOLVED'
		AND actor_kind='USER' AND changed_by_user_id=$3 AND changed_at=$4)=1 FROM issue_status_history WHERE issue_report_id=$1`, r.ID, events, r.ResolverID, r.ResolvedAt).Scan(&valid); err != nil || !valid {
		t.Fatal("wrong audit", err)
	}
	stored := issueResponse(t, f.a.call(t, 200, "GET", issueInbox+"/"+r.ID, nil, f.cookies[actor]))
	if !reflect.DeepEqual(stored, r) {
		t.Fatal("resolution not persisted")
	}
	return r
}

func (f *issueFixture) testCoordinatorIssues(t *testing.T) {
	unrelatedState := func() string {
		var state string
		if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'sessions',(SELECT jsonb_agg(s ORDER BY id) FROM tutoring_sessions s),
			'blocks',(SELECT count(*) FROM session_participant_blocks),
			'attendance',(SELECT count(*) FROM attendance_reports),
			'violations',(SELECT count(*) FROM user_violations),
			'flags',(SELECT count(*) FROM user_semester_flags),
			'notifications',(SELECT count(*) FROM notifications))::text`).Scan(&state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	unrelatedBefore := unrelatedState()
	// Independent of creation subtest order: all report IDs and authority-race
	// identities used here belong to this fixture, and each mutation is restored.
	for _, faculty := range []int{1704, 1705} {
		f.exec(t, "INSERT INTO faculties(id,name) VALUES($1,$2)", fixture(faculty), "Issue inbox "+fixture(faculty))
	}
	f.exec(t, `INSERT INTO users(id,name,email,password_hash) SELECT $1,'Mutable coordinator','issue-1814@uni.example',password_hash FROM users WHERE id=$2`, fixture(issueCoordinatorMutable), fixture(issueCoordinatorA))
	f.exec(t, "INSERT INTO coordinators VALUES($1,'ISSUE-C4',$2)", fixture(issueCoordinatorMutable), fixture(1704))
	w := f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": "issue-1814@uni.example", "password": password}, nil)
	f.cookies[issueCoordinatorMutable] = w.Result().Cookies()[0]
	// Student verification is not a prerequisite for the separate coordinator role.
	f.exec(t, "INSERT INTO students VALUES($1,'ISSUE-COORD-STUDENT',$2)", fixture(issueCoordinatorA), fixture(1711))
	f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2)", fixture(issueCoordinatorA), fixture(1701))
	// Keep this pending verification as immutable evidence until the disposable
	// database is removed. The separate coordinator role remains authorized.

	a := f.seedIssue(t, 2001, 1701, false)
	b := f.seedIssue(t, 2002, 1701, false)
	c := f.seedIssue(t, 2003, 1702, false)
	resolve := func(id string) string { return issueInbox + "/" + id + "/resolve" }
	note := map[string]string{"resolution_note": "  Confidential resolution — 学生\n"}
	read := func(t *testing.T, id string, actor int) coordomain.Issue {
		return issueResponse(t, f.a.call(t, 200, "GET", issueInbox+"/"+id, nil, f.cookies[actor]))
	}

	t.Run("faculty inbox pagination and read-only detail", func(t *testing.T) {
		state := f.issueState(t, a)
		for _, tc := range []struct {
			actor, faculty int
			ids            []string
		}{{issueCoordinatorA, 1701, []string{a, b}}, {issueCoordinatorB, 1702, []string{c}}} {
			page := f.inbox(t, tc.actor, "")
			if len(page.Data) != len(tc.ids) || page.Pagination.Limit != 25 || page.Pagination.Offset != 0 || page.Pagination.HasMore {
				t.Fatal(page)
			}
			for i, r := range page.Data {
				if r.ID != tc.ids[i] || r.FacultyID != fixture(tc.faculty) || r.Status != coordomain.IssueOpen {
					t.Fatal("faculty or stable ordering failed", page)
				}
			}
		}
		page := f.inbox(t, issueCoordinatorA, "?limit=1")
		if len(page.Data) != 1 || page.Data[0].ID != a || !page.Pagination.HasMore {
			t.Fatal(page)
		}
		page = f.inbox(t, issueCoordinatorA, "?limit=1&offset=1")
		if len(page.Data) != 1 || page.Data[0].ID != b || page.Pagination.HasMore {
			t.Fatal(page)
		}
		for _, query := range []string{"?offset=100000&limit=100", "?status=RESOLVED"} {
			if len(f.inbox(t, issueCoordinatorA, query).Data) != 0 {
				t.Fatal("expected empty page")
			}
		}
		if len(f.inbox(t, issueCoordinatorMutable, "").Data) != 0 {
			t.Fatal("empty faculty leaked another faculty")
		}
		read(t, a, issueCoordinatorA)
		if f.issueState(t, a) != state {
			t.Fatal("GET changed issue/history")
		}
	})
	t.Run("authorization input and private identifiers", func(t *testing.T) {
		state := f.issueState(t, a)
		for _, op := range []struct {
			method, path string
			body         any
		}{{"GET", issueInbox, nil}, {"GET", issueInbox + "/" + a, nil}, {"POST", resolve(a), note}} {
			f.a.call(t, 401, op.method, op.path, op.body, nil)
			for _, actor := range []int{issueStudentB, issueTutorA, issuePending, issueRejected, issueSuspended, issueClosed} {
				f.a.call(t, 403, op.method, op.path, op.body, f.cookies[actor])
			}
			bad := *f.cookies[issueCoordinatorA]
			bad.Value = "invalid-session"
			f.a.call(t, 401, op.method, op.path, op.body, &bad)
		}
		for _, op := range []struct {
			method string
			path   func(string) string
			body   any
		}{{"GET", func(id string) string { return issueInbox + "/" + id }, nil}, {"POST", resolve, note}} {
			foreign := f.a.call(t, 404, op.method, op.path(c), op.body, f.cookies[issueCoordinatorA])
			missing := f.a.call(t, 404, op.method, op.path(fixture(9999)), op.body, f.cookies[issueCoordinatorA])
			if foreign.Body.String() != missing.Body.String() || strings.Contains(foreign.Body.String(), "Private general issue") {
				t.Fatal("report existence disclosed")
			}
			f.a.call(t, 422, op.method, op.path("malformed"), op.body, f.cookies[issueCoordinatorA])
		}
		for _, from := range []string{"", "https://evil.example"} {
			if f.a.request("POST", resolve(a), from, note, f.cookies[issueCoordinatorA]).Code != 403 {
				t.Fatal("CSRF accepted")
			}
		}
		for _, q := range []string{"?faculty_id=" + fixture(1702), "?status=OPEN&status=RESOLVED", "?limit=x", "?offset=1&offset=2", "?other=x", "?limit=%zz"} {
			f.a.call(t, 400, "GET", issueInbox+q, nil, f.cookies[issueCoordinatorA])
		}
		for _, q := range []string{"?status=CLOSED", "?status=", "?status=PENDING", "?limit=0", "?limit=101", "?offset=-1", "?offset=100001"} {
			f.a.call(t, 422, "GET", issueInbox+q, nil, f.cookies[issueCoordinatorA])
		}
		for _, body := range []map[string]any{{}, {"resolution_note": nil}, {"resolution_note": ""}, {"resolution_note": " \t\n\u3000"}, {"resolution_note": strings.Repeat("é", 1001)}, {"resolution_note": "nul\x00"}} {
			f.a.call(t, 422, "POST", resolve(a), body, f.cookies[issueCoordinatorA])
		}
		for _, key := range []string{"faculty_id", "handled_by_coordinator_id", "resolver_id", "reporter_user_id", "resolved_at", "status", "actor_kind"} {
			f.a.call(t, 400, "POST", resolve(a), map[string]any{"resolution_note": "Note", key: fixture(issueCoordinatorA)}, f.cookies[issueCoordinatorA])
		}
		for _, tc := range []struct {
			body, content string
			code          int
		}{
			{"null", "application/json", 400}, {"[]", "application/json", 400}, {`{"resolution_note":`, "application/json", 400},
			{`{"resolution_note":123}`, "application/json", 400}, {`{"resolution_note":"note"} {}`, "application/json", 400},
			{"{\"resolution_note\":\"invalid\xff\"}", "application/json", 400},
			{`{"resolution_note":"` + strings.Repeat("x", 17*1024) + `"}`, "application/json", 413}, {`{}`, "text/plain", 415},
		} {
			r := httptest.NewRequest("POST", origin+"/api/v1"+resolve(a), strings.NewReader(tc.body))
			r.Header.Set("Origin", origin)
			r.Header.Set("Content-Type", tc.content)
			r.AddCookie(f.cookies[issueCoordinatorA])
			w := httptest.NewRecorder()
			f.a.handler.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatal("invalid resolution input", w.Code, tc.code)
			}
		}
		if f.issueState(t, a) != state {
			t.Fatal("unauthorized/invalid request changed issue")
		}
	})
	t.Run("resolution history and historical access", func(t *testing.T) {
		before := read(t, a, issueCoordinatorA)
		var creation string
		if err := f.db.QueryRow(f.ctx, "SELECT row_to_json(h)::text FROM issue_status_history h WHERE issue_report_id=$1", a).Scan(&creation); err != nil {
			t.Fatal(err)
		}
		f.resolved(t, f.a.call(t, 200, "POST", resolve(a), note, f.cookies[issueCoordinatorA]), before, issueCoordinatorA, "Confidential resolution — 学生", 2)
		var preserved bool
		if err := f.db.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM issue_status_history h WHERE issue_report_id=$1 AND row_to_json(h)::text=$2)", a, creation).Scan(&preserved); err != nil || !preserved {
			t.Fatal("creation event changed", err)
		}
		page := f.inbox(t, issueCoordinatorA, "?status=RESOLVED")
		if len(page.Data) != 1 || page.Data[0].ID != a || page.Data[0].Resolution != "Confidential resolution — 学生" {
			t.Fatal(page)
		}
		if page = f.inbox(t, issueCoordinatorA, ""); len(page.Data) != 1 || page.Data[0].ID != b {
			t.Fatal(page)
		}
		state := f.issueState(t, a)
		f.a.call(t, 409, "POST", resolve(a), map[string]string{"resolution_note": "Overwrite attempt"}, f.cookies[issueCoordinatorA])
		for _, method := range []string{"GET", "POST"} {
			path := issueInbox + "/" + a
			if method == "POST" {
				path = resolve(a)
			}
			f.a.call(t, 404, method, path, note, f.cookies[issueCoordinatorB])
		}
		if f.issueState(t, a) != state {
			t.Fatal("closed report rewritten")
		}
	})
	t.Run("audit and update failures roll back", func(t *testing.T) {
		id := f.seedIssue(t, 2010, 1701, false)
		state := f.issueState(t, id)
		f.exec(t, `CREATE FUNCTION issue_inbox_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private resolution database failure'; END; $$;
			CREATE TRIGGER issue_inbox_failure BEFORE INSERT ON issue_status_history FOR EACH ROW EXECUTE FUNCTION issue_inbox_failure();`)
		w := f.a.call(t, 500, "POST", resolve(id), note, f.cookies[issueCoordinatorA])
		f.exec(t, "DROP TRIGGER issue_inbox_failure ON issue_status_history")
		if w.Body.String() != "{\"error\":\"internal server error\"}\n" || f.issueState(t, id) != state {
			t.Fatal("audit failure leaked details or committed")
		}
		f.exec(t, "CREATE TRIGGER issue_inbox_failure BEFORE UPDATE ON issue_reports FOR EACH ROW EXECUTE FUNCTION issue_inbox_failure()")
		f.a.call(t, 500, "POST", resolve(id), note, f.cookies[issueCoordinatorA])
		f.exec(t, "DROP TRIGGER issue_inbox_failure ON issue_reports; DROP FUNCTION issue_inbox_failure()")
		if f.issueState(t, id) != state {
			t.Fatal("update failure committed history")
		}
		before := read(t, id, issueCoordinatorA)
		f.resolved(t, f.a.call(t, 200, "POST", resolve(id), note, f.cookies[issueCoordinatorA]), before, issueCoordinatorA, "Confidential resolution — 学生", 2)
	})
	t.Run("legacy evidence is retained", func(t *testing.T) {
		id := f.seedIssue(t, 2011, 1701, true)
		// Simulate pre-11 content only in this isolated test DB. The schema suite
		// separately tests a real 10 -> 11 -> 12 upgrade with this content.
		f.exec(t, `BEGIN; ALTER TABLE issue_reports DISABLE TRIGGER issue_reports_description_bounds;
			ALTER TABLE issue_reports DISABLE TRIGGER audit_decision;
			UPDATE issue_reports SET description=repeat('é',1001) WHERE id='00000000-0000-0000-0000-000000002011';
			ALTER TABLE issue_reports ENABLE TRIGGER issue_reports_description_bounds;
			ALTER TABLE issue_reports ENABLE TRIGGER audit_decision; COMMIT;`)
		before := read(t, id, issueCoordinatorA)
		if len(before.Description) != 2002 {
			t.Fatal("legacy evidence changed")
		}
		f.resolved(t, f.a.call(t, 200, "POST", resolve(id), map[string]string{"resolution_note": strings.Repeat("é", 1000)}, f.cookies[issueCoordinatorA]), before, issueCoordinatorA, strings.Repeat("é", 1000), 1)
	})
	t.Run("competing resolutions commit exactly once", func(t *testing.T) {
		id := f.seedIssue(t, 2012, 1701, false)
		before := read(t, id, issueCoordinatorA)
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err = tx.Exec(f.ctx, "SELECT id FROM issue_reports WHERE id=$1 FOR UPDATE", id); err != nil {
			t.Fatal(err)
		}
		results := make(chan *httptest.ResponseRecorder, 2)
		// There is one coordinator per faculty; two requests from that authorized
		// coordinator compete, without relaxing the schema for artificial actors.
		for _, text := range []string{"Winning note A", "Winning note B"} {
			go func(note string) {
				results <- f.a.request("POST", resolve(id), origin, map[string]string{"resolution_note": note}, f.cookies[issueCoordinatorA])
			}(text)
		}
		f.waitIssueLocks(t, 2, "SELECT %FROM issue_reports i WHERE i.id=%FOR UPDATE OF i")
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		x, y := <-results, <-results
		if x.Code != 200 {
			x, y = y, x
		}
		if x.Code != 200 || y.Code != 409 {
			t.Fatal(x.Code, y.Code)
		}
		winner := issueResponse(t, x)
		f.resolved(t, x, before, issueCoordinatorA, winner.Resolution, 2)
	})
	t.Run("stored faculty survives reporter move", func(t *testing.T) {
		id := f.seedIssue(t, 2013, 1702, false)
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(issueStudentB), fixture(1711))
		defer f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(issueStudentB), fixture(1712))
		f.a.call(t, 404, "GET", issueInbox+"/"+id, nil, f.cookies[issueCoordinatorA])
		before := read(t, id, issueCoordinatorB)
		f.resolved(t, f.a.call(t, 200, "POST", resolve(id), note, f.cookies[issueCoordinatorB]), before, issueCoordinatorB, "Confidential resolution — 学生", 2)
	})
	f.testIssueAuthorityRaces(t, resolve, note)
	if unrelatedState() != unrelatedBefore {
		t.Fatal("general issue resolution changed scheduling, discipline or notifications")
	}
	for _, private := range []string{"Private general issue", "Confidential resolution", "private resolution database failure"} {
		if strings.Contains(f.logs.String(), private) {
			t.Fatal("private issue content logged")
		}
	}
}

func (f *issueFixture) waitIssueLocks(t *testing.T, count int, query string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		if err := f.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name='tutti_issue_integration'
			AND query LIKE $1 AND cardinality(pg_blocking_pids(pid))>0`, query).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked >= count {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("expected database lock was not reached")
		}
	}
}

func (f *issueFixture) testIssueAuthorityRaces(t *testing.T, resolve func(string) string, note map[string]string) {
	for _, tc := range []struct {
		name, change, restore string
		code                  int
	}{
		{"faculty reassignment", `UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000001705' WHERE user_id=$1`, `UPDATE coordinators SET faculty_id='00000000-0000-0000-0000-000000001704' WHERE user_id=$1`, 404},
		{"suspension", `UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='Fixture' WHERE id=$1`, `UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1`, 403},
		{"closure", `UPDATE users SET closed_at=now() WHERE id=$1`, `UPDATE users SET closed_at=NULL WHERE id=$1`, 403},
		{"assignment removal", `DELETE FROM coordinators WHERE user_id=$1`, `INSERT INTO coordinators VALUES($1,'ISSUE-C4','00000000-0000-0000-0000-000000001704')`, 403},
	} {
		t.Run(tc.name+" before resolution", func(t *testing.T) {
			var id string
			if err := f.db.QueryRow(f.ctx, `INSERT INTO issue_reports(reporter_user_id,faculty_id,description) VALUES($1,$2,'Authority race') RETURNING id::text`, fixture(issueStudentB), fixture(1704)).Scan(&id); err != nil {
				t.Fatal(err)
			}
			state := f.issueState(t, id)
			tx, err := f.db.Begin(f.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(f.ctx)
			if _, err = tx.Exec(f.ctx, tc.change, fixture(issueCoordinatorMutable)); err != nil {
				t.Fatal(err)
			}
			result := make(chan *httptest.ResponseRecorder, 1)
			go func() { result <- f.a.request("POST", resolve(id), origin, note, f.cookies[issueCoordinatorMutable]) }()
			f.waitIssueLocks(t, 1, "%SELECT c.faculty_id::text FROM coordinators c JOIN users u%FOR SHARE OF c, u")
			if err = tx.Commit(f.ctx); err != nil {
				t.Fatal(err)
			}
			defer f.exec(t, tc.restore, fixture(issueCoordinatorMutable))
			w := <-result
			if w.Code != tc.code {
				t.Fatal("stale coordinator authority accepted", w.Code, w.Body.String())
			}
			if f.issueState(t, id) != state {
				t.Fatal("unauthorized mutation")
			}
			// Bypass middleware with an old principal to prove reads recheck SQL scope.
			old := identitydomain.Principal{ID: fixture(issueCoordinatorMutable), CoordinatorFaculty: fixture(1704)}
			service := coordination.NewIssues(f.repo)
			if _, err = service.Get(f.ctx, old, id); !errors.Is(err, coordomain.ErrIssueNotFound) {
				t.Fatal("stale read leaked report", err)
			}
			rows, err := service.List(f.ctx, old, coordomain.IssueOpen, 25, 0)
			if err != nil || len(rows) != 0 {
				t.Fatal("stale inbox leaked report", err)
			}
		})
	}
	t.Run("canceled transaction leaves report open", func(t *testing.T) {
		id := f.seedIssue(t, 2014, 1701, false)
		before := f.issueState(t, id)
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err = tx.Exec(f.ctx, "SELECT id FROM issue_reports WHERE id=$1 FOR UPDATE", id); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := coordination.NewIssues(f.repo).Resolve(ctx, identitydomain.Principal{ID: fixture(issueCoordinatorA), CoordinatorFaculty: fixture(1701)}, id, "Note")
			result <- err
		}()
		f.waitIssueLocks(t, 1, "SELECT %FROM issue_reports i WHERE i.id=%FOR UPDATE OF i")
		cancel()
		err = <-result
		if err == nil {
			t.Fatal("cancellation was ignored")
		}
		if rollbackErr := tx.Rollback(f.ctx); rollbackErr != nil {
			t.Fatal(rollbackErr)
		}
		if f.issueState(t, id) != before {
			t.Fatal("canceled resolution committed", err)
		}
	})
	t.Run("database rejects invalid resolution notes", func(t *testing.T) {
		id := f.seedIssue(t, 2015, 1701, true)
		for _, note := range []string{" \t\n\u3000", strings.Repeat("é", 1001)} {
			_, err := f.db.Exec(f.ctx, `UPDATE issue_reports SET status='RESOLVED',resolved_at=now(),handled_by_coordinator_id=$2,resolution_note=$3 WHERE id=$1`, id, fixture(issueCoordinatorA), note)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatal("invalid database note accepted", err)
			}
		}
	})
}
