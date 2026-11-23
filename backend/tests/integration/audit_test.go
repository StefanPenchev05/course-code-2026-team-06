package integration_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	coord "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coorddomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
)

func (f *suspensionFixture) testAuditHistory(t *testing.T) {
	path := func(user int, kind string) string {
		return "/coordinator/users/" + fixture(user) + "/audit-history?category=" + kind
	}
	read := func(t *testing.T, actor, user int, kind string) []any {
		t.Helper()
		w := f.a.call(t, 200, "GET", path(user, kind), nil, f.cookie[actor])
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable history")
		}
		assertPrivate(t, w.Body.String())
		return fields(t, w)["data"].([]any)
	}
	t.Run("audit historical ownership pagination and privacy", func(t *testing.T) {
		for _, kind := range []string{"SUSPENSION", "VIOLATION", "FLAG"} {
			own := read(t, suspCoordinator, suspTutor, kind)
			if len(own) == 0 {
				t.Fatal("missing history", kind)
			}
			before := f.a.call(t, 200, "GET", path(suspTutor, kind), nil, f.cookie[suspCoordinator]).Body.String()
			f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(suspTutor), fixture(5112))
			after := f.a.call(t, 200, "GET", path(suspTutor, kind), nil, f.cookie[suspCoordinator]).Body.String()
			if before != after {
				t.Fatal("historical ownership changed with program")
			}
			for _, raw := range read(t, suspOtherCoordinator, suspTutor, kind) {
				e := raw.(map[string]any)
				if e["faculty_id"] != fixture(5102) {
					t.Fatal("foreign history disclosed", e)
				}
			}
			f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(suspTutor), fixture(5111))
		}
		first := fields(t, f.a.call(t, 200, "GET", path(suspTutor, "SUSPENSION")+"&limit=1", nil, f.cookie[suspCoordinator]))
		next := fields(t, f.a.call(t, 200, "GET", path(suspTutor, "SUSPENSION")+"&limit=1&offset=1", nil, f.cookie[suspCoordinator]))
		if first["pagination"].(map[string]any)["has_more"] != true || len(first["data"].([]any)) != 1 || first["data"].([]any)[0].(map[string]any)["id"] == next["data"].([]any)[0].(map[string]any)["id"] {
			t.Fatal(first, next)
		}
		missing := f.a.call(t, 200, "GET", path(999999, "FLAG"), nil, f.cookie[suspOtherCoordinator]).Body.String()
		foreign := f.a.call(t, 200, "GET", path(suspTutor, "FLAG"), nil, f.cookie[suspOtherCoordinator]).Body.String()
		if missing != foreign {
			t.Fatal("foreign existence or counts disclosed")
		}
		for _, actor := range []int{0, suspTutor, suspStudent, suspPending} {
			code := 403
			if actor == 0 {
				code = 401
			}
			f.a.call(t, code, "GET", path(suspTutor, "SUSPENSION"), nil, f.cookie[actor])
		}
		for _, suffix := range []string{"&limit=0", "&limit=101", "&offset=-1", "&offset=100001"} {
			f.a.call(t, 422, "GET", path(suspTutor, "FLAG")+suffix, nil, f.cookie[suspCoordinator])
		}
		for _, suffix := range []string{"&faculty_id=x", "&actor_id=x", "&category=FLAG", "&limit=x"} {
			f.a.call(t, 400, "GET", path(suspTutor, "FLAG")+suffix, nil, f.cookie[suspCoordinator])
		}
		f.a.call(t, 422, "GET", "/coordinator/users/bad/audit-history", nil, f.cookie[suspCoordinator])
		f.a.call(t, 422, "GET", path(suspTutor, "ALL"), nil, f.cookie[suspCoordinator])
		f.a.call(t, 405, "DELETE", path(suspTutor, "SUSPENSION"), nil, f.cookie[suspCoordinator])
		for _, raw := range read(t, suspCoordinator, suspTutor, "VIOLATION") {
			e := raw.(map[string]any)
			if e["violation_kind"] == "NO_SHOW" && (e["actor_kind"] != "USER" || e["actor_user_id"] != fixture(suspCoordinator) || e["attendance_report_id"] == nil) {
				t.Fatal("absent person confused with reviewer", e)
			}
			if e["violation_kind"] == "LATE_CANCELLATION" && (e["actor_kind"] != "SYSTEM" || e["actor_user_id"] != nil || e["cancellation_history_id"] == nil) {
				t.Fatal(e)
			}
		}
		flag := read(t, suspCoordinator, suspTutor, "FLAG")[0].(map[string]any)
		if flag["actor_kind"] != "SYSTEM" || flag["actor_user_id"] != nil || flag["first_violation_id"] == nil || flag["first_violation_id"] == flag["second_violation_id"] {
			t.Fatal(flag)
		}
	})
	t.Run("audit live authority also checked inside read snapshot", func(t *testing.T) {
		service := coord.NewAudit(coordpg.NewAudit(f.db, coordpg.AuditDependencies{Faculty: identitypg.ReadCoordinatorFaculty, Accounts: identitypg.SuspensionHistory}))
		stale := identity.Principal{ID: fixture(suspMutableCoordinator), CoordinatorFaculty: fixture(5101)}
		// A forged/stale faculty is ignored even when bypassing middleware.
		got, err := service.List(f.ctx, stale, fixture(suspTutor), "SUSPENSION", 25, 0)
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
		for _, v := range []struct{ change, restore string }{
			{"UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1"},
			{"UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='test' WHERE id=$1", "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1"},
			{"DELETE FROM coordinators WHERE user_id=$1", "INSERT INTO coordinators VALUES($1,'AUDIT-restored','00000000-0000-0000-0000-000000005103')"},
		} {
			f.exec(t, v.change, stale.ID)
			f.a.call(t, 403, "GET", path(suspTutor, "SUSPENSION"), nil, f.cookie[suspMutableCoordinator])
			if _, err := service.List(f.ctx, stale, fixture(suspTutor), "SUSPENSION", 25, 0); !errors.Is(err, coorddomain.ErrForbidden) {
				t.Fatal(err)
			}
			f.exec(t, v.restore, stale.ID)
		}
		f.exec(t, "UPDATE coordinators SET faculty_id=$2 WHERE user_id=$1", stale.ID, fixture(5104))
		if rows := read(t, suspMutableCoordinator, suspTutor, "SUSPENSION"); len(rows) != 0 {
			t.Fatal(rows)
		}
		f.exec(t, "UPDATE coordinators SET faculty_id=$2 WHERE user_id=$1", stale.ID, fixture(5103))
	})
	t.Run("audit failure at flag rolls back cause history and notification", func(t *testing.T) {
		book := func() string {
			f.setTime(0)
			id := f.request(t, 201, suspOtherStudent, time.Duration(f.next)*time.Hour, 5322, 5332)
			f.next += 2
			f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, suspOtherTutor)
			return id
		}
		first := book()
		f.setTime(time.Duration(f.next-3) * time.Hour)
		f.session(t, 200, "POST", "/sessions/"+first+"/cancel", testReason, suspOtherStudent)
		f.count(t, 1, "SELECT count(*) FROM user_violations WHERE user_id=$1", fixture(suspOtherStudent))
		f.count(t, 0, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1", fixture(suspOtherStudent))
		second := book()
		f.setTime(time.Duration(f.next-3) * time.Hour)
		before := f.snapshot(t, suspensionStateTables...)
		f.exec(t, `CREATE FUNCTION reject_audit_flag_test() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'private audit failure'; END; $$;
   CREATE TRIGGER reject_audit_flag_test BEFORE INSERT ON user_semester_flags FOR EACH ROW EXECUTE FUNCTION reject_audit_flag_test()`)
		f.session(t, 500, "POST", "/sessions/"+second+"/cancel", testReason, suspOtherStudent)
		f.exec(t, "DROP TRIGGER reject_audit_flag_test ON user_semester_flags; DROP FUNCTION reject_audit_flag_test()")
		if before != f.snapshot(t, suspensionStateTables...) {
			t.Fatal("partial flag transaction")
		}
		f.session(t, 200, "POST", "/sessions/"+second+"/cancel", testReason, suspOtherStudent)
		f.session(t, 409, "POST", "/sessions/"+second+"/cancel", testReason, suspOtherStudent)
		f.count(t, 1, `SELECT count(*) FROM user_semester_flags f JOIN user_violations a ON a.id=f.first_violation_id
   JOIN user_violations b ON b.id=f.second_violation_id WHERE f.user_id=$1 AND f.actor_kind='SYSTEM'
   AND a.user_id=f.user_id AND b.user_id=f.user_id AND a.semester_id=f.semester_id AND b.semester_id=f.semester_id`, fixture(suspOtherStudent))
		f.count(t, 0, "SELECT count(*) FROM users WHERE id=$1 AND is_suspended", fixture(suspOtherStudent))
	})
	t.Run("audit ordinary database role cannot rewrite or truncate evidence", func(t *testing.T) {
		// A new application supplies real application history even in -AuditOnly mode.
		f.a.call(t, 201, "POST", "/tutor-applications", map[string]any{"subject_id": fixture(5121), "grade": 17.5, "description": "Audit application"}, f.cookie[suspOtherStudent])
		f.exec(t, `INSERT INTO tutor_offering_deactivations(offering_id,qualification_id,faculty_id,program_id,actor_kind,changed_at,reason)
   SELECT id,qualification_id,$1,$2,'SYSTEM',now(),'Append-only fixture' FROM tutor_offerings WHERE id=$3`, fixture(5101), fixture(5111), fixture(5321))
		tables := []string{"session_status_history", "application_status_history", "issue_status_history", "user_suspension_history", "user_violations", "user_semester_flags", "tutor_qualifications", "tutor_offering_deactivations", "student_verifications", "tutor_applications", "attendance_reports", "issue_reports"}
		f.exec(t, "CREATE ROLE tutti_audit_test NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION; GRANT USAGE ON SCHEMA public TO tutti_audit_test; GRANT SELECT,INSERT,UPDATE,DELETE,TRUNCATE ON ALL TABLES IN SCHEMA public TO tutti_audit_test")
		defer f.exec(t, "DROP OWNED BY tutti_audit_test; DROP ROLE tutti_audit_test")
		before := f.snapshot(t, tables...)
		for _, table := range tables {
			var count int
			if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count == 0 {
				t.Fatal("empty mutation fixture", table, err)
			}
			for _, sql := range []string{"UPDATE " + table + " SET id=id", "DELETE FROM " + table, "TRUNCATE " + table + " CASCADE"} {
				tx, err := f.db.Begin(f.ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(f.ctx, "SET LOCAL ROLE tutti_audit_test"); err != nil {
					t.Fatal(err)
				}
				var privileged bool
				if err = tx.QueryRow(f.ctx, "SELECT rolsuper FROM pg_roles WHERE rolname=current_user").Scan(&privileged); err != nil || privileged {
					t.Fatal("not a restricted role", err)
				}
				_, err = tx.Exec(f.ctx, sql)
				_ = tx.Rollback(f.ctx)
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != "23514" {
					t.Fatalf("%s: %v", sql, err)
				}
			}
		}
		if before != f.snapshot(t, tables...) {
			t.Fatal("audit mutated")
		}
		if strings.Contains(f.logs.String(), "private audit failure") {
			t.Fatal("private error leaked")
		}
	})
}
