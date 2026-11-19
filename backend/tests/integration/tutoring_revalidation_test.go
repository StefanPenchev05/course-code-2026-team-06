package integration_test

import (
	"context"
	"errors"
	identitydomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"sync"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
	tutorpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func eligibilityDeps() tutorpg.Dependencies {
	return tutorpg.Dependencies{
		Catalog:     func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
		LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty,
	}
}

// This is a trusted test orchestrator, not a program-change API or production service.
func changeProgram(ctx context.Context, tx pgx.Tx, student, program string) error {
	if _, err := identitypg.LockStudent(ctx, tx, student); err != nil {
		return err
	}
	if _, err := catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE students SET program_id=$2 WHERE user_id=$1", student, program); err != nil {
		return err
	}
	return tutoring.RevalidateTutorEligibilityInTransaction(ctx, tutorpg.EligibilityInTransaction(tx, eligibilityDeps()), student)
}

func testOfferingRevalidation(t *testing.T, ctx context.Context, db *pgxpool.Pool, service *tutoring.Service, tutor, nonTutor string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func(sql string, args ...any) string {
		t.Helper()
		var value string
		if err := db.QueryRow(ctx, sql, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	// Snapshot approval evidence for ALL students; revalidation may only change offerings.
	evidenceSQL := `SELECT jsonb_build_array(
  (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM tutor_applications a),
  (SELECT jsonb_agg(to_jsonb(q) ORDER BY id) FROM tutor_qualifications q),
  (SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM application_status_history h))::text`
	beforeEvidence := snapshot(evidenceSQL)
	offeringSQL := `SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY id),'[]')::text FROM tutor_offerings o WHERE tutor_id=$1`
	before := snapshot(offeringSQL, tutor)
	if err := service.RevalidateTutorEligibility(ctx, fixture(9999)); !errors.Is(err, tutordomain.ErrNotFound) {
		t.Fatal("unknown student", err)
	}
	if err := service.RevalidateTutorEligibility(ctx, nonTutor); err != nil {
		t.Fatal("non-tutor", err)
	}
	// Cross-faculty program shares Databases; empty program forces two deactivations.
	exec("INSERT INTO programs VALUES ($1,$3,'Revalidation shared','MASTER'),($2,$3,'Revalidation empty','MASTER')", fixture(214), fixture(215), fixture(202))
	exec("INSERT INTO program_subjects VALUES ($1,$2)", fixture(214), fixture(222))
	exec(`CREATE FUNCTION revalidation_test_failure() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.subject_id='00000000-0000-0000-0000-000000000222'::uuid AND NOT NEW.is_active
 THEN RAISE EXCEPTION 'revalidation fixture'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER revalidation_test_failure BEFORE UPDATE ON tutor_offerings FOR EACH ROW EXECUTE FUNCTION revalidation_test_failure()`)
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return changeProgram(ctx, tx, tutor, fixture(215)) })
	exec("DROP TRIGGER revalidation_test_failure ON tutor_offerings")
	exec("DROP FUNCTION revalidation_test_failure()")
	if err == nil {
		t.Fatal("expected second deactivation failure")
	}
	if got := snapshot(offeringSQL, tutor); got != before {
		t.Fatal("partial offering deactivation committed")
	}
	if got := snapshot("SELECT program_id::text FROM students WHERE user_id=$1", tutor); got != fixture(211) {
		t.Fatal("program change did not roll back", got)
	}
	// The independent reconciliation wrapper also rolls back partial work.
	exec("UPDATE students SET program_id=$2 WHERE user_id=$1", tutor, fixture(215))
	exec(`CREATE FUNCTION revalidation_test_failure() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.subject_id='00000000-0000-0000-0000-000000000222'::uuid AND NOT NEW.is_active
 THEN RAISE EXCEPTION 'revalidation fixture'; END IF; RETURN NEW; END $$`)
	exec(`CREATE TRIGGER revalidation_test_failure BEFORE UPDATE ON tutor_offerings FOR EACH ROW EXECUTE FUNCTION revalidation_test_failure()`)
	err = service.RevalidateTutorEligibility(ctx, tutor)
	exec("DROP TRIGGER revalidation_test_failure ON tutor_offerings")
	exec("DROP FUNCTION revalidation_test_failure()")
	if err == nil || snapshot(offeringSQL, tutor) != before {
		t.Fatal("independent reconciliation failed to roll back", err)
	}
	// Commit a cross-faculty change with a shared subject; only Algorithms withdraws.
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return changeProgram(ctx, tx, tutor, fixture(214)) }); err != nil {
		t.Fatal(err)
	}
	var active, withdrawn int
	if err := db.QueryRow(ctx, `SELECT count(*) FILTER(WHERE is_active AND subject_id=$2),
 count(*) FILTER(WHERE NOT is_active AND deactivated_at IS NOT NULL AND btrim(deactivation_reason)<>'')
 FROM tutor_offerings WHERE tutor_id=$1`, tutor, fixture(222)).Scan(&active, &withdrawn); err != nil || active != 1 || withdrawn != 1 {
		t.Fatal(active, withdrawn, err)
	}
	after := snapshot(offeringSQL, tutor)
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- service.RevalidateTutorEligibility(ctx, tutor) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if snapshot(offeringSQL, tutor) != after {
		t.Fatal("idempotent calls changed deactivation metadata")
	}
	if beforeEvidence != snapshot(evidenceSQL) {
		t.Fatal("approval evidence changed")
	}
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return changeProgram(ctx, tx, tutor, fixture(211)) }); err != nil {
		t.Fatal(err)
	}
	if snapshot(offeringSQL, tutor) != after {
		t.Fatal("returning to original program reactivated or changed offering")
	}
}

// Observe PostgreSQL lock waits rather than assuming a goroutine reached a lock.
func waitEligibilityBlock(t *testing.T, ctx context.Context, db *pgxpool.Pool, blocker uint32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)))`, int(blocker)).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("expected PostgreSQL lock wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (f *bookingFixture) testProgramRevalidation(t *testing.T) {
	f.setTime(155 * time.Hour)
	service := tutoring.New(tutorpg.New(f.db, eligibilityDeps()), nil, nil)
	tutor := fixture(bookingTutor)
	f.exec(t, "INSERT INTO programs VALUES ($1,$2,'Ineligible program','BACHELOR')", fixture(407), fixture(401))
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.db.QueryRow(f.ctx, `SELECT jsonb_build_array(
   (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM tutoring_sessions s),
   (SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM session_status_history h),
   (SELECT jsonb_agg(to_jsonb(b) ORDER BY session_id,user_id) FROM session_participant_blocks b),
   (SELECT jsonb_agg(to_jsonb(f) ORDER BY session_id) FROM feedback f),
   (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM tutor_applications a),
   (SELECT jsonb_agg(to_jsonb(q) ORDER BY id) FROM tutor_qualifications q),
   (SELECT to_jsonb(o) FROM tutor_offerings o WHERE id='00000000-0000-0000-0000-000000000442'))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	// Include an outstanding request and accepted session in the preservation check.
	f.request(t, 201, bookingStudent, 200*time.Hour, bookingOffer, bookingSlot)
	accepted := f.request(t, 201, bookingStudent, 202*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", tutorRequestsPath+accepted+"/accept", testMeeting, bookingTutor)
	before := snapshot()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err := changeProgram(f.ctx, tx, tutor, fixture(407)); err != nil {
		t.Fatal(err)
	}
	result := make(chan int, 1)
	go func() {
		result <- f.a.request("POST", "/sessions", origin, f.payload(204*time.Hour, bookingOffer, bookingSlot), f.cookie[bookingStudent]).Code
	}()
	waitEligibilityBlock(t, f.ctx, f.db, tx.Conn().PgConn().PID())
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if code := <-result; code != 422 {
		t.Fatalf("booking after change: %d", code)
	}
	if snapshot() != before {
		t.Fatal("revalidation modified sessions, blocks, feedback or approval evidence")
	}
	if _, err := service.GetApprovedOffering(f.ctx, tutor, fixture(405)); !errors.Is(err, tutordomain.ErrNotFound) {
		t.Fatal(err)
	}
	// Another tutor's offering is byte-for-byte unaffected by reconciliation.
	// The snapshot includes the other tutor’s full offering row.
	// Returning requires explicit reapproval, not automatic reactivation.
	if err := pgx.BeginFunc(f.ctx, f.db, func(tx pgx.Tx) error { return changeProgram(f.ctx, tx, tutor, fixture(403)) }); err != nil {
		t.Fatal(err)
	}
	f.request(t, 422, bookingStudent, 204*time.Hour, bookingOffer, bookingSlot)
	actor := identitydomain.Principal{ID: tutor, Student: true, ProgramID: fixture(403), Verification: identitydomain.Approved}
	app, err := service.Apply(f.ctx, actor, tutoring.Submission{SubjectID: fixture(405), Grade: 1850, Description: "Explicit reapproval"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Review(f.ctx, identitydomain.Principal{ID: fixture(bookingCoordinator), CoordinatorFaculty: fixture(401)}, app, tutordomain.Approved, ""); err != nil {
		t.Fatal(err)
	}
	offer, err := service.GetApprovedOffering(f.ctx, tutor, fixture(405))
	if err != nil || offer.ID != fixture(bookingOffer) {
		t.Fatal(offer, err)
	}
	// Booking wins first: hold its source slot so we can observe its account locks,
	// then start a program change. The later change must preserve the committed request.
	guard, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Rollback(f.ctx)
	if _, err := guard.Exec(f.ctx, "SELECT id FROM availability_slots WHERE id=$1 FOR UPDATE", fixture(bookingSlot)); err != nil {
		t.Fatal(err)
	}
	go func() {
		result <- f.a.request("POST", "/sessions", origin, f.payload(204*time.Hour, bookingOffer, bookingSlot), f.cookie[bookingStudent]).Code
	}()
	waitEligibilityBlock(t, f.ctx, f.db, guard.Conn().PgConn().PID())
	change, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer change.Rollback(f.ctx)
	changePID := change.Conn().PgConn().PID()
	changed := make(chan error, 1)
	go func() {
		err := changeProgram(f.ctx, change, tutor, fixture(407))
		if err == nil {
			err = change.Commit(f.ctx)
		}
		changed <- err
	}()
	// Inspect the change connection's wait; this proves it cannot pass the booking.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := f.db.QueryRow(f.ctx, "SELECT cardinality(pg_blocking_pids($1))>0", int(changePID)).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("program change did not wait on booking account locks")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := guard.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if code := <-result; code != 201 {
		t.Fatalf("booking before change: %d", code)
	}
	if err := <-changed; err != nil {
		t.Fatal(err)
	}
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE starts_at=$1 AND status='REQUESTED'", f.start.Add(204*time.Hour))
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE starts_at=$1", f.start.Add(204*time.Hour))
	f.request(t, 422, bookingStudent, 206*time.Hour, bookingOffer, bookingSlot)
}
