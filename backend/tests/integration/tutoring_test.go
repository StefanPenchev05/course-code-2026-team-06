package integration_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	feedbackpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/infrastructure/postgres"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
	tutorpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	tutorhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/interfaces/http"
)

func TestTutoringAPI(t *testing.T) {
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_identity.sh for disposable PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	hasher, err := security.NewPasswords(security.Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	idService, err := identity.New(ctx, identitypg.New(db), sessionredis.New(client), hasher,
		identity.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	repo := tutorpg.New(db, tutorpg.Dependencies{
		Catalog:     func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
		LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty,
	})
	service := tutoring.New(repo, catalog.New(catalogpg.New(db)), feedback.New(feedbackpg.New(db, feedbackpg.Dependencies{}), nil))
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, slog.Default())
	auth.RegisterRoutes(mux)
	tutorhttp.New(service, slog.Default()).RegisterRoutes(mux, auth)
	a := api{mux}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(want int, sql string, args ...any) {
		t.Helper()
		var n int
		if err := db.QueryRow(ctx, sql, args...).Scan(&n); err != nil || n != want {
			t.Fatalf("expected %d rows, got %d (%v) for %s", want, n, err, sql)
		}
	}
	facultyA, facultyB := fixture(201), fixture(202)
	programA, programB, newProgram := fixture(211), fixture(212), fixture(213)
	subjectA, subjectB, otherSubject := fixture(221), fixture(222), fixture(223)
	tutor, learner, pending, coordA, coordB := fixture(231), fixture(232), fixture(233), fixture(234), fixture(235)
	exec("INSERT INTO faculties (id,name) VALUES ($1,'TW Science'),($2,'TW Engineering')", facultyA, facultyB)
	exec("INSERT INTO programs (id,faculty_id,name,level) VALUES ($1,$4,'TWA','BACHELOR'),($2,$5,'TWB','MASTER'),($3,$4,'TWC','BACHELOR')", programA, programB, newProgram, facultyA, facultyB)
	exec("INSERT INTO subjects (id,code,name) VALUES ($1,'TW1','Algorithms'),($2,'TW2','Databases'),($3,'TW3','Languages')", subjectA, subjectB, otherSubject)
	exec("INSERT INTO program_subjects VALUES ($1,$4),($2,$4),($1,$5),($2,$5),($3,$5),($2,$6)", programA, programB, newProgram, subjectA, subjectB, otherSubject)
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	accounts := []struct{ id, name, email string }{
		{tutor, "TW Tutor", "tw-tutor@uni.example"},
		{learner, "TW Learner", "tw-learner@uni.example"},
		{pending, "TW Pending", "tw-pending@uni.example"},
		{coordA, "TW CA", "tw-ca@uni.example"},
		{coordB, "TW CB", "tw-cb@uni.example"},
	}
	for _, u := range accounts {
		exec("INSERT INTO users (id,name,email,password_hash) VALUES ($1,$2,$3,$4)", u.id, u.name, u.email, hash)
	}
	exec("INSERT INTO coordinators VALUES ($1,'TW-C1',$3),($2,'TW-C2',$4)", coordA, coordB, facultyA, facultyB)
	exec("INSERT INTO students VALUES ($1,'TW-S1',$4),($2,'TW-S2',$5),($3,'TW-S3',$4)", tutor, learner, pending, programA, programB)
	exec(`INSERT INTO student_verifications (student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
		VALUES ($1,$4,'APPROVED',now(),$6),($2,$5,'APPROVED',now(),$7),($3,$4,'PENDING',NULL,NULL)`,
		tutor, learner, pending, facultyA, facultyB, coordA, coordB)
	cookies := make(map[string]*http.Cookie)
	for _, u := range accounts {
		cookies[u.id] = a.call(t, 200, "POST", "/auth/login", map[string]string{"email": u.email, "password": password}, nil).Result().Cookies()[0]
	}
	payload := func(subject string, grade float64) map[string]any {
		return map[string]any{"subject_id": subject, "grade": grade, "description": "TW Help with coursework"}
	}
	apply := func(subject string) string {
		t.Helper()
		f := fields(t, a.call(t, 201, "POST", "/tutor-applications", payload(subject, 17.50), cookies[tutor]))
		if f["status"] != "PENDING" {
			t.Fatal(f)
		}
		return f["application_id"].(string)
	}
	search := func(subject string, want int, extra string) {
		t.Helper()
		w := a.call(t, 200, "GET", "/tutors?subject_id="+subject+extra, nil, cookies[learner])
		assertPrivate(t, w.Body.String())
		items := fields(t, w)["data"].([]any)
		if len(items) != want {
			t.Fatalf("expected %d tutors, got %v", want, items)
		}
		if want == 1 && items[0].(map[string]any)["id"] != tutor {
			t.Fatal(items)
		}
	}
	a.call(t, 401, "POST", "/tutor-applications", payload(subjectA, 17.5), nil)
	a.call(t, 403, "POST", "/tutor-applications", payload(subjectA, 17.5), cookies[pending])
	a.call(t, 422, "POST", "/tutor-applications", payload(otherSubject, 17.5), cookies[tutor])
	a.call(t, 422, "POST", "/tutor-applications", payload(subjectA, 14.99), cookies[tutor])
	a.call(t, 422, "POST", "/tutor-applications", payload(subjectA, 20.01), cookies[tutor])
	a.call(t, 422, "POST", "/tutor-applications", payload(subjectA, 17.505), cookies[tutor])
	first := apply(subjectA)
	reviewPath := "/coordinator/tutor-applications/"
	search(subjectA, 0, "")
	a.call(t, 404, "GET", "/tutors/"+tutor, nil, cookies[learner])
	a.call(t, 409, "POST", "/tutor-applications", payload(subjectB, 18), cookies[tutor])
	count(1, "SELECT count(*) FROM tutor_qualifications WHERE application_id=$1 AND grade=17.50", first)
	count(1, "SELECT count(*) FROM application_status_history WHERE application_id=$1 AND from_status IS NULL AND to_status='PENDING' AND changed_by_user_id=$2", first, tutor)
	a.call(t, 404, "GET", "/tutor-applications/"+first, nil, cookies[learner])
	a.call(t, 404, "GET", reviewPath+first, nil, cookies[coordB])
	a.call(t, 404, "POST", reviewPath+first+"/approve", nil, cookies[coordB])
	a.call(t, 403, "POST", reviewPath+first+"/approve", nil, cookies[tutor])
	a.call(t, 422, "POST", reviewPath+first+"/reject", map[string]string{"reason": " "}, cookies[coordA])
	assertPrivate(t, a.call(t, 200, "GET", "/tutor-applications/"+first, nil, cookies[tutor]).Body.String())
	detail := fields(t, a.call(t, 200, "GET", reviewPath+first, nil, cookies[coordA]))["data"].(map[string]any)
	if detail["grade"] != "17.50" || detail["description"] == nil {
		t.Fatal(detail)
	}
	if got := fields(t, a.call(t, 200, "GET", strings.TrimSuffix(reviewPath, "/"), nil, cookies[coordB]))["data"].([]any); len(got) != 0 {
		t.Fatal(got)
	}

	// Failed history insertion must also undo approval, profile and offering.
	exec(`CREATE FUNCTION reject_application_audit_test() RETURNS trigger LANGUAGE plpgsql AS $$
	 BEGIN IF NEW.to_status='APPROVED' THEN RAISE EXCEPTION 'audit fixture'; END IF; RETURN NEW; END; $$;
	 CREATE TRIGGER reject_application_audit_test BEFORE INSERT ON application_status_history FOR EACH ROW EXECUTE FUNCTION reject_application_audit_test()`)
	a.call(t, 500, "POST", reviewPath+first+"/approve", nil, cookies[coordA])
	exec("DROP TRIGGER reject_application_audit_test ON application_status_history; DROP FUNCTION reject_application_audit_test()")
	count(1, "SELECT count(*) FROM tutor_applications WHERE id=$1 AND status='PENDING'", first)
	count(0, "SELECT count(*) FROM tutor_profiles WHERE student_id=$1", tutor)
	count(0, "SELECT count(*) FROM tutor_offerings WHERE tutor_id=$1", tutor)
	count(1, "SELECT count(*) FROM application_status_history WHERE application_id=$1", first)
	// A forced outbox conflict occurs after the profile, offering and history writes.
	key := "tutor-application-decision:" + first
	exec(`INSERT INTO notifications (recipient_user_id,type,subject,body,deduplication_key)
		VALUES ($1,'APPLICATION_APPROVED','fixture','fixture',$2)`, tutor, key)
	a.call(t, 409, "POST", reviewPath+first+"/approve", nil, cookies[coordA])
	count(1, "SELECT count(*) FROM tutor_applications WHERE id=$1 AND status='PENDING'", first)
	count(0, "SELECT count(*) FROM tutor_profiles WHERE student_id=$1", tutor)
	count(0, "SELECT count(*) FROM tutor_offerings WHERE tutor_id=$1", tutor)
	count(1, "SELECT count(*) FROM application_status_history WHERE application_id=$1", first)
	exec("DELETE FROM notifications WHERE recipient_user_id=$1 AND deduplication_key=$2", tutor, key)
	a.call(t, 204, "POST", reviewPath+first+"/approve", nil, cookies[coordA])
	a.call(t, 409, "POST", reviewPath+first+"/approve", nil, cookies[coordA])
	a.call(t, 409, "POST", "/tutor-applications", payload(subjectA, 17.5), cookies[tutor])
	count(1, "SELECT count(*) FROM tutor_offerings o JOIN tutor_qualifications q ON q.id=o.qualification_id WHERE q.application_id=$1 AND o.is_active", first)
	count(1, "SELECT count(*) FROM notifications WHERE application_id=$1 AND type='APPLICATION_APPROVED' AND email_status='PENDING' AND recipient_user_id=$2", first, tutor)
	count(1, "SELECT count(*) FROM application_status_history WHERE application_id=$1 AND from_status='PENDING' AND to_status='APPROVED' AND changed_by_user_id=$2", first, coordA)
	search(subjectA, 1, "&faculty_id="+facultyA+"&program_id="+programA+"&level=BACHELOR")
	search(subjectA, 0, "&faculty_id="+facultyB)
	a.call(t, 422, "GET", "/tutors?subject_id="+otherSubject, nil, cookies[tutor])

	second := apply(subjectB)
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, decision := range []string{"approve", "reject"} {
		go func() {
			<-start
			var body any
			if decision == "reject" {
				body = map[string]string{"reason": "TW Review"}
			}
			results <- a.request("POST", reviewPath+second+"/"+decision, origin, body, cookies[coordA]).Code
		}()
	}
	close(start)
	x, y := <-results, <-results
	if !((x == 204 && y == 409) || (x == 409 && y == 204)) {
		t.Fatalf("concurrent decisions: %d, %d", x, y)
	}
	count(2, "SELECT count(*) FROM application_status_history WHERE application_id=$1", second)
	count(1, "SELECT count(*) FROM notifications WHERE application_id=$1", second)
	var secondStatus string
	if err := db.QueryRow(ctx, "SELECT status::text FROM tutor_applications WHERE id=$1", second).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if secondStatus == "REJECTED" {
		count(0, "SELECT count(*) FROM tutor_offerings WHERE tutor_id=$1 AND subject_id=$2", tutor, subjectB)
		second = apply(subjectB)
	}
	if secondStatus == "APPROVED" {
		// A separate tutor application exercises the rejection path regardless of the race winner.
		w := a.call(t, 201, "POST", "/tutor-applications", payload(subjectA, 17.50), cookies[learner])
		rejected := fields(t, w)["application_id"].(string)
		a.call(t, 204, "POST", reviewPath+rejected+"/reject", map[string]string{"reason": "TW Rejection"}, cookies[coordB])
		count(1, "SELECT count(*) FROM notifications WHERE application_id=$1 AND type='APPLICATION_REJECTED'", rejected)
		count(0, "SELECT count(*) FROM tutor_profiles WHERE student_id=$1", learner)
	} else {
		a.call(t, 204, "POST", reviewPath+second+"/approve", nil, cookies[coordA])
	}
	count(1, "SELECT count(*) FROM tutor_profiles WHERE student_id=$1", tutor)
	count(2, "SELECT count(*) FROM tutor_offerings WHERE tutor_id=$1 AND is_active", tutor)
	for _, path := range []string{"/tutors/" + tutor, "/tutors/me", "/tutor-applications/me?limit=1"} {
		assertPrivate(t, a.call(t, 200, "GET", path, nil, cookies[tutor]).Body.String())
	}
	if got := fields(t, a.call(t, 200, "GET", strings.TrimSuffix(reviewPath, "/")+"?status=APPROVED", nil, cookies[coordA]))["data"].([]any); len(got) != 2 {
		t.Fatal(got)
	}

	var eligibility tutoring.OfferingEligibility = service
	before, err := eligibility.GetApprovedOffering(ctx, tutor, subjectA)
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE students SET program_id=$2 WHERE user_id=$1", tutor, newProgram)
	search(subjectA, 0, "")
	if _, err := eligibility.GetApprovedOffering(ctx, tutor, subjectA); !errors.Is(err, tutordomain.ErrNotFound) {
		t.Fatal("current program check before reconciliation", err)
	}
	if err := service.RevalidateTutorEligibility(ctx, tutor); err != nil {
		t.Fatal(err)
	}
	count(1, "SELECT count(*) FROM tutor_offerings WHERE id=$1 AND NOT is_active AND deactivated_at IS NOT NULL AND deactivation_reason IS NOT NULL", before.ID)
	count(1, `SELECT count(*) FROM tutor_offering_deactivations h JOIN tutor_offerings o ON o.id=h.offering_id
	 WHERE h.offering_id=$1 AND h.actor_kind='SYSTEM' AND h.changed_at=o.deactivated_at
	 AND h.reason=o.deactivation_reason AND h.program_id=$2 AND h.faculty_id=$3`, before.ID, newProgram, facultyA)
	search(subjectB, 1, "")
	own := a.call(t, 200, "GET", "/tutors/me", nil, cookies[tutor])
	assertPrivate(t, own.Body.String())
	if !strings.Contains(own.Body.String(), `"active":false`) {
		t.Fatal("owner cannot view inactive offering", own.Body.String())
	}
	exec("UPDATE students SET program_id=$2 WHERE user_id=$1", tutor, programA)
	if err := service.RevalidateTutorEligibility(ctx, tutor); err != nil {
		t.Fatal(err)
	}
	if _, err := eligibility.GetApprovedOffering(ctx, tutor, subjectA); !errors.Is(err, tutordomain.ErrNotFound) {
		t.Fatal(err)
	}
	reactivation := apply(subjectA)
	// Reconciliation holds the same account lock as coordinator approval.
	reconcileTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reconcileTx.Rollback(ctx)
	if err := tutoring.RevalidateTutorEligibilityInTransaction(ctx, tutorpg.EligibilityInTransaction(reconcileTx, eligibilityDeps()), tutor); err != nil {
		t.Fatal(err)
	}
	approveStart := make(chan struct{})
	for range 2 {
		go func() {
			<-approveStart
			results <- a.request("POST", reviewPath+reactivation+"/approve", origin, nil, cookies[coordA]).Code
		}()
	}
	close(approveStart)
	waitEligibilityBlock(t, ctx, db, reconcileTx.Conn().PgConn().PID())
	if err := reconcileTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	x, y = <-results, <-results
	if !((x == 204 && y == 409) || (x == 409 && y == 204)) {
		t.Fatalf("duplicate concurrent approvals: %d, %d", x, y)
	}
	count(1, "SELECT count(*) FROM notifications WHERE application_id=$1", reactivation)
	after, err := eligibility.GetApprovedOffering(ctx, tutor, subjectA)
	if err != nil || after.ID != before.ID {
		t.Fatalf("offering identity changed: %+v, %v", after, err)
	}
	count(1, "SELECT count(*) FROM tutor_qualifications WHERE application_id=$1", first)
	count(1, "SELECT count(*) FROM tutor_offering_deactivations WHERE offering_id=$1", before.ID)
	count(0, `SELECT count(*) FROM application_status_history h JOIN tutor_applications a ON a.id=h.application_id
	 WHERE a.student_id=$1 AND h.changed_at IS DISTINCT FROM CASE WHEN h.from_status IS NULL THEN a.submitted_at ELSE a.reviewed_at END`, tutor)
	count(1, "SELECT count(*) FROM tutor_offerings o JOIN tutor_qualifications q ON q.id=o.qualification_id WHERE q.application_id=$1 AND o.id=$2", reactivation, before.ID)
	t.Run("transactional offering revalidation", func(t *testing.T) { testOfferingRevalidation(t, ctx, db, service, tutor, pending) })
	exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='fixture' WHERE id=$1", tutor, coordA)
	search(subjectA, 0, "")
	a.call(t, 404, "GET", "/tutors/"+tutor, nil, cookies[learner])
	a.call(t, 403, "POST", "/tutor-applications", payload(subjectA, 17.5), cookies[tutor])
}

func assertPrivate(t *testing.T, body string) {
	t.Helper()
	for _, key := range []string{`"grade"`, `"email"`, `"student_number"`, `"password_hash"`, `"qualification_id"`} {
		if strings.Contains(body, key) {
			t.Fatalf("private field %s exposed: %s", key, body)
		}
	}
}
