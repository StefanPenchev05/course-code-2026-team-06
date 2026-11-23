package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	coord "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coorddomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
)

const flaggedUsersPath = "/coordinator/flagged-users"

// Reuse the existing isolated suspension/attendance harness, after its audit
// checks. These new actors have no incidents from the preceding regression tests.
func (f *suspensionFixture) testFlaggedUsers(t *testing.T) {
	const studentA, studentB, tutorB = 6101, 6102, 6103
	// Other integration suites define short semesters in 2030. Give this
	// workflow its own time range so booking cannot select those semesters.
	f.start = time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	f.next = 72
	f.setTime(0)
	f.exec(t, "INSERT INTO semesters VALUES($1,'Flag first semester','2040-01-01T00:00Z','2040-02-01T00:00Z')", fixture(6131))
	for _, v := range []struct{ id, program, faculty, coordinator int }{
		{studentA, 5111, 5101, suspCoordinator}, {studentB, 5112, 5102, suspOtherCoordinator}, {tutorB, 5112, 5102, suspOtherCoordinator}, {6104, 5111, 5101, suspCoordinator},
	} {
		f.exec(t, `INSERT INTO users(id,name,email,password_hash)
 SELECT $1,'Flag review user',$2,password_hash FROM users WHERE id=$3`, fixture(v.id), fixture(v.id)+"@uni.example", fixture(suspStudent))
		f.exec(t, "INSERT INTO students VALUES($1,$2,$3)", fixture(v.id), fixture(v.id), fixture(v.program))
		f.exec(t, `INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
 VALUES($1,$2,'APPROVED',now(),$3)`, fixture(v.id), fixture(v.faculty), fixture(v.coordinator))
	}
	f.exec(t, "INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Flag review tutor',now())", fixture(tutorB))
	f.exec(t, `INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
 VALUES($1,$2,$3,'Private academic evidence','APPROVED',now(),$4)`, fixture(6301), fixture(tutorB), fixture(5102), fixture(suspOtherCoordinator))
	f.exec(t, "INSERT INTO tutor_qualifications(id,application_id,student_id,subject_id,grade) VALUES($1,$2,$3,$4,19.37)", fixture(6311), fixture(6301), fixture(tutorB), fixture(5121))
	f.exec(t, "INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Flag offering')", fixture(6321), fixture(tutorB), fixture(5121), fixture(6311))
	f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,'2040-01-01T00:00Z','2040-02-01T00:00Z','BOTH')", fixture(6331), fixture(tutorB))
	for _, id := range []int{studentA, studentB, tutorB, 6104} {
		f.cookie[id] = f.login(t, id)
	}
	read := func(t *testing.T, actor int, query string) map[string]any {
		t.Helper()
		w := f.a.call(t, 200, "GET", flaggedUsersPath+query, nil, f.cookie[actor])
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable flag data")
		}
		assertPrivate(t, w.Body.String())
		got := fields(t, w)
		if len(got) != 2 || len(got["pagination"].(map[string]any)) != 3 {
			t.Fatal("unexpected totals or metadata", got)
		}
		for _, raw := range got["data"].([]any) {
			e := raw.(map[string]any)
			keys := []string{}
			for k := range e {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !reflect.DeepEqual(keys, []string{"flag_id", "flagged_at", "is_suspended", "semester_id", "strike_count", "user_id"}) {
				t.Fatal("private summary fields", e)
			}
		}
		return got
	}
	assertFlag := func(t *testing.T, actor, user, semester int, count int64, suspended bool) string {
		t.Helper()
		for _, raw := range read(t, actor, "?limit=100")["data"].([]any) {
			e := raw.(map[string]any)
			if e["user_id"] == fixture(user) && e["semester_id"] == fixture(semester) {
				if e["strike_count"] != float64(count) || e["is_suspended"] != suspended {
					t.Fatal(e, count, suspended)
				}
				return e["flag_id"].(string)
			}
		}
		t.Fatal("missing flag", actor, user, semester)
		return ""
	}
	assertAbsent := func(t *testing.T, actor, user int) {
		t.Helper()
		for _, raw := range read(t, actor, "?limit=100")["data"].([]any) {
			if raw.(map[string]any)["user_id"] == fixture(user) {
				t.Fatal("foreign or premature flag", actor, user)
			}
		}
	}
	// All bookings use the real API. No synthetic violations or mutable flag rows.
	book := func(t *testing.T, student int) (string, time.Duration) {
		t.Helper()
		f.setTime(0)
		at := time.Duration(f.next) * time.Hour
		f.next += 2
		id := f.request(t, 201, student, at, 6321, 6331)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, tutorB)
		return id, at
	}
	late := func(t *testing.T, student, affected int) {
		t.Helper()
		id, at := book(t, student)
		f.setTime(at - time.Hour)
		f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, affected)
	}
	report := func(t *testing.T, id string, at time.Duration, reporter int) string {
		t.Helper()
		f.setTime(at + time.Hour)
		return f.session(t, 201, "POST", "/sessions/"+id+"/no-show-reports", map[string]string{"description": "Private attendance evidence"}, reporter)["id"].(string)
	}
	confirm := func(t *testing.T, id string, actor, code int) {
		t.Helper()
		f.session(t, code, "POST", "/coordinator/attendance-reports/"+id+"/confirm", map[string]string{"resolution_note": "Private confirmed evidence"}, actor)
	}
	t.Run("flag review cross faculty student mixed threshold and accurate current totals", func(t *testing.T) {
		assertAbsent(t, suspCoordinator, studentA)
		late(t, studentA, studentA)
		assertAbsent(t, suspCoordinator, studentA)
		id, at := book(t, studentA)
		r := report(t, id, at, tutorB)
		assertAbsent(t, suspCoordinator, studentA) // allegation contributes nothing
		confirm(t, r, suspCoordinator, 404)        // home faculty cannot adjudicate session B
		confirm(t, r, suspOtherCoordinator, 200)
		flag := assertFlag(t, suspCoordinator, studentA, 6131, 2, false)
		assertAbsent(t, suspOtherCoordinator, studentA)
		f.count(t, 1, `SELECT count(*) FROM attendance_reports r JOIN tutoring_sessions s ON s.id=r.session_id
 WHERE r.id=$1 AND r.reviewed_by_coordinator_id=$2 AND s.faculty_id=$3`, r, fixture(suspOtherCoordinator), fixture(5102))
		confirm(t, r, suspOtherCoordinator, 409)
		for n := int64(3); n <= 4; n++ {
			late(t, studentA, studentA)
			if got := assertFlag(t, suspCoordinator, studentA, 6131, n, false); got != flag {
				t.Fatal("flag replaced", got, flag)
			}
		}
		f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1 AND semester_id=$2", fixture(studentA), fixture(6131))
		// Rejected allegations, early participant cancellations and policy actions
		// cannot inflate the read projection's count.
		id, at = book(t, studentA)
		r = report(t, id, at, tutorB)
		f.session(t, 200, "POST", "/coordinator/attendance-reports/"+r+"/reject", map[string]string{"resolution_note": "Unsubstantiated"}, suspOtherCoordinator)
		id, _ = book(t, studentA)
		f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, studentA) // clock is still day one
		id, at = book(t, studentA)
		f.setTime(at - time.Hour)
		f.session(t, 200, "POST", policyPath(id), policyReason, suspOtherCoordinator)
		assertFlag(t, suspCoordinator, studentA, 6131, 4, false)
		// Another home faculty's newest flag must not affect A's pages or totals.
		late(t, studentB, studentB)
		late(t, studentB, studentB)
		assertFlag(t, suspOtherCoordinator, studentB, 6131, 2, false)
		assertAbsent(t, suspCoordinator, studentB)
		// D has no coordinator yet: a later assignment must grant exactly D's scope.
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(6104), fixture(5114))
		late(t, 6104, 6104)
		late(t, 6104, 6104)
		f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1 AND faculty_id=$2", fixture(6104), fixture(5104))
	})
	t.Run("flag review reverse tutor routing and historical program ownership", func(t *testing.T) {
		late(t, studentB, tutorB)
		id, at := book(t, studentB) // session captures B before the program move
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(tutorB), fixture(5111))
		r := report(t, id, at, studentB)
		confirm(t, r, suspCoordinator, 404)
		confirm(t, r, suspOtherCoordinator, 200)
		assertFlag(t, suspCoordinator, tutorB, 6131, 2, false)
		assertAbsent(t, suspOtherCoordinator, tutorB)
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(tutorB), fixture(5112))
		assertFlag(t, suspCoordinator, tutorB, 6131, 2, false)
		assertAbsent(t, suspOtherCoordinator, tutorB)
		// A third incident is now owned by B but counts toward this same semester.
		late(t, studentB, tutorB)
		assertFlag(t, suspCoordinator, tutorB, 6131, 3, false)
		f.count(t, 2, "SELECT count(*) FROM user_violations WHERE user_id=$1 AND home_faculty_id=$2", fixture(tutorB), fixture(5102))
		f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1", fixture(tutorB))
		// Existing detail policy remains independent: B can read its own incidents
		// but cannot enumerate A's flag, even though the tutor now lives in B.
		missing := f.a.call(t, 200, "GET", "/coordinator/users/"+fixture(999999)+"/audit-history?category=FLAG", nil, f.cookie[suspOtherCoordinator])
		foreign := f.a.call(t, 200, "GET", "/coordinator/users/"+fixture(tutorB)+"/audit-history?category=FLAG", nil, f.cookie[suspOtherCoordinator])
		if missing.Body.String() != foreign.Body.String() {
			t.Fatal("foreign historical flag disclosed")
		}
		f.decide(t, suspCoordinator, tutorB, true, 404) // historical review is not current sanction authority
		f.decide(t, suspOtherCoordinator, tutorB, true, 200)
		assertFlag(t, suspCoordinator, tutorB, 6131, 3, true)
		f.decide(t, suspOtherCoordinator, tutorB, false, 200)
		assertFlag(t, suspCoordinator, tutorB, 6131, 3, false)
	})
	t.Run("flag review one snapshot across concurrent incident and suspension commits", func(t *testing.T) {
		id, at := book(t, studentA)
		f.setTime(at - time.Hour)
		svc := coord.NewFlaggedUsers(coordpg.NewFlaggedUsers(f.db, coordpg.FlaggedUsersDependencies{
			Faculty: func(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
				faculty, err := identitypg.ReadCoordinatorFaculty(ctx, tx, actor)
				if err != nil {
					return "", err
				}
				var isolation, readOnly string
				if err = tx.QueryRow(ctx, "SELECT current_setting('transaction_isolation'),current_setting('transaction_read_only')").Scan(&isolation, &readOnly); err != nil {
					return "", err
				}
				if isolation != "repeatable read" || readOnly != "on" {
					t.Fatal(isolation, readOnly)
				}
				f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, studentA)
				// Include an active future session: suspension must cancel it without a strike.
				book(t, studentA)
				f.decide(t, suspCoordinator, studentA, true, 200)
				return faculty, nil
			}, Accounts: identitypg.AccountStates,
		}))
		rows, err := svc.List(f.ctx, identity.Principal{ID: fixture(suspCoordinator), CoordinatorFaculty: fixture(5101)}, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range rows {
			if row.UserID == fixture(studentA) {
				found = true
				if row.StrikeCount != 4 || row.IsSuspended {
					t.Fatal("mixed snapshots", row)
				}
			}
		}
		if !found {
			t.Fatal("missing snapshot flag")
		}
		flag := assertFlag(t, suspCoordinator, studentA, 6131, 5, true)
		f.decide(t, suspCoordinator, studentA, false, 200)
		if got := assertFlag(t, suspCoordinator, studentA, 6131, 5, false); got != flag {
			t.Fatal("reinstatement lost history")
		}
	})
	t.Run("flag review semester separation tied ordering and bounded scoped pages", func(t *testing.T) {
		f.exec(t, "INSERT INTO semesters VALUES($1,'Flag second semester','2040-02-01T00:00Z','2040-03-01T00:00Z')", fixture(6132))
		f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,'2040-02-01T00:00Z','2040-03-01T00:00Z','BOTH')", fixture(6332), fixture(tutorB))
		// Create real flags at exactly the same decision time (no audit mutation).
		var seconds []struct {
			id   string
			user int
		}
		for i, user := range []int{studentA, studentB} {
			for n := 0; n < 2; n++ {
				f.setTime(744 * time.Hour)
				at := time.Duration(752+i*4+n*2) * time.Hour
				id := f.request(t, 201, user, at, 6321, 6332)
				f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, tutorB)
				if n == 0 {
					f.setTime(750 * time.Hour)
					f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, user)
					f.count(t, 0, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1 AND semester_id=$2", fixture(user), fixture(6132))
				} else {
					seconds = append(seconds, struct {
						id   string
						user int
					}{id, user})
				}
			}
		}
		// Move B to A before its second incident, so both tied flags belong to A.
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(studentB), fixture(5111))
		for _, s := range seconds {
			f.setTime(751 * time.Hour)
			f.session(t, 200, "POST", "/sessions/"+s.id+"/cancel", testReason, s.user)
			assertFlag(t, suspCoordinator, s.user, 6132, 2, false)
		}
		assertFlag(t, suspCoordinator, studentA, 6131, 5, false)
		assertFlag(t, suspOtherCoordinator, studentB, 6131, 2, false)
		for _, v := range []struct{ actor, faculty int }{{suspCoordinator, 5101}, {suspOtherCoordinator, 5102}, {suspMutableCoordinator, 5103}} {
			rows, err := f.db.Query(f.ctx, "SELECT id::text FROM user_semester_flags WHERE faculty_id=$1 ORDER BY flagged_at DESC,id DESC", fixture(v.faculty))
			if err != nil {
				t.Fatal(err)
			}
			want, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for offset := 0; offset <= len(want); offset++ {
				page := read(t, v.actor, fmt.Sprintf("?limit=1&offset=%d", offset))
				items := page["data"].([]any)
				if page["pagination"].(map[string]any)["has_more"] != (offset+1 < len(want)) {
					t.Fatal(page, want)
				}
				if offset < len(want) {
					if len(items) != 1 {
						t.Fatal(page)
					}
					got = append(got, items[0].(map[string]any)["flag_id"].(string))
				} else if len(items) != 0 {
					t.Fatal(page)
				}
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatal("duplicate, missing, reordered or foreign flags", got, want)
			}
		}
		page := read(t, suspCoordinator, "")
		if page["pagination"].(map[string]any)["limit"] != float64(25) {
			t.Fatal(page)
		}
		if got := read(t, suspCoordinator, "?limit=100&offset=100000"); len(got["data"].([]any)) != 0 || got["pagination"].(map[string]any)["has_more"] != false {
			t.Fatal(got)
		}
	})
	f.testFlaggedUsersAuthority(t)
}

func (f *suspensionFixture) testFlaggedUsersAuthority(t *testing.T) {
	t.Run("flag review auth strict input privacy and read only database state", func(t *testing.T) {
		rows, err := f.db.Query(f.ctx, "SELECT tablename::text FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
		if err != nil {
			t.Fatal(err)
		}
		tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil || len(tables) < 33 {
			t.Fatal(tables, err)
		}
		before := f.snapshot(t, tables...)
		for _, v := range []struct{ actor, code int }{{0, 401}, {suspStudent, 403}, {suspTutor, 403}, {suspPending, 403}, {suspCoordinator, 200}} {
			f.a.call(t, v.code, "GET", flaggedUsersPath, nil, f.cookie[v.actor])
		}
		for _, query := range []string{"limit=0", "limit=101", "offset=-1", "offset=100001"} {
			f.a.call(t, 422, "GET", flaggedUsersPath+"?"+query, nil, f.cookie[suspCoordinator])
		}
		for _, query := range []string{"faculty_id=" + fixture(5102), "user_id=" + fixture(6102), "semester_id=" + fixture(6131), "limit=x", "limit=1&limit=2", "offset=0&offset=1", "limit=999999999999999999999999", "search=private", "%zz=x"} {
			f.a.call(t, 400, "GET", flaggedUsersPath+"?"+query, nil, f.cookie[suspCoordinator])
		}
		for _, method := range []string{"POST", "PATCH", "DELETE"} {
			f.a.call(t, 405, method, flaggedUsersPath, nil, f.cookie[suspCoordinator])
		}
		for _, id := range []string{fixture(999999), fixture(6102)} {
			f.a.call(t, 404, "GET", flaggedUsersPath+"/"+id, nil, f.cookie[suspCoordinator])
		}
		for _, expired := range []bool{true, false} {
			cookie := f.login(t, suspCoordinator)
			if expired {
				digest := sha256.Sum256([]byte(cookie.Value))
				if ok, err := f.redis.PExpireAt(f.ctx, "tutti:identity:session:"+base64.RawURLEncoding.EncodeToString(digest[:]), time.Unix(1, 0)).Result(); err != nil || !ok {
					t.Fatal(ok, err)
				}
			} else {
				f.a.call(t, 204, "POST", "/auth/logout", nil, cookie)
			}
			f.a.call(t, 401, "GET", flaggedUsersPath, nil, cookie)
		}
		if before != f.snapshot(t, tables...) {
			t.Fatal("reads or rejected requests changed state")
		}
	})
	t.Run("flag review current authority despite stale principals and cookies", func(t *testing.T) {
		service := coord.NewFlaggedUsers(coordpg.NewFlaggedUsers(f.db, coordpg.FlaggedUsersDependencies{Faculty: identitypg.ReadCoordinatorFaculty, Accounts: identitypg.AccountStates}))
		stale := identity.Principal{ID: fixture(suspMutableCoordinator), CoordinatorFaculty: fixture(5101)}
		got, err := service.List(f.ctx, stale, 25, 0)
		if err != nil || len(got) != 0 {
			t.Fatal("trusted stale faculty", got, err)
		}
		for _, v := range []struct{ change, restore string }{
			{"UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1"},
			{"UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='private test' WHERE id=$1", "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1"},
			{"DELETE FROM coordinators WHERE user_id=$1", "INSERT INTO coordinators VALUES($1,'FLAGS-restored','00000000-0000-0000-0000-000000005103')"},
		} {
			f.exec(t, v.change, stale.ID)
			f.a.call(t, 403, "GET", flaggedUsersPath, nil, f.cookie[suspMutableCoordinator])
			if _, err := service.List(f.ctx, stale, 25, 0); !errors.Is(err, coorddomain.ErrForbidden) {
				t.Fatal(err)
			}
			f.exec(t, v.restore, stale.ID)
		}
		for _, faculty := range []int{5104, 5103} {
			f.exec(t, "UPDATE coordinators SET faculty_id=$2 WHERE user_id=$1", stale.ID, fixture(faculty))
			got, err := service.List(f.ctx, stale, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			var want int
			if err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM user_semester_flags WHERE faculty_id=$1", fixture(faculty)).Scan(&want); err != nil || len(got) != want {
				t.Fatal(got, want, err)
			}
			for _, row := range got {
				f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE id=$1 AND faculty_id=$2", row.FlagID, fixture(faculty))
			}
			w := f.a.call(t, 200, "GET", flaggedUsersPath+"?limit=100", nil, f.cookie[suspMutableCoordinator])
			if len(fields(t, w)["data"].([]any)) != want {
				t.Fatal("stale cookie authority")
			}
		}
		ctx, cancel := context.WithCancel(f.ctx)
		cancel()
		if _, err := service.List(ctx, stale, 25, 0); err == nil {
			t.Fatal("ignored cancelled context")
		}
	})
}
