package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	feedbackpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/infrastructure/postgres"
	feedbackhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/interfaces/http"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	workers "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/worker"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	tutoringhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/interfaces/http"
)

type bookingFixture struct {
	ctx     context.Context
	db      *pgxpool.Pool
	a       api
	service *scheduling.BookingService
	clock   atomic.Int64
	start   time.Time
	cookie  map[int]*http.Cookie
}

const (
	bookingStudent         = 411
	bookingTutor           = 412
	bookingOtherTutor      = 413
	bookingOtherStudent    = 414
	bookingPending         = 415
	bookingCoordinator     = 416
	bookingHomeCoordinator = 417
	bookingSlot            = 451
	bookingOtherSlot       = 452
	bookingOffer           = 441
	bookingOtherOffer      = 442
)

func newBookingFixture(t *testing.T) *bookingFixture {
	t.Helper()
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_identity.sh for PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	db, err := pgxpool.New(ctx, dsn)
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
	idService, err := identity.New(ctx, identitypg.New(db), sessionredis.New(client), hasher, identity.Config{
		Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &bookingFixture{ctx: ctx, db: db, start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), cookie: map[int]*http.Cookie{}}
	f.setTime(0)
	repo := schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{
		LockStudent: identitypg.LockStudent,
		Catalog:     func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
		Offerings:   tutoringpg.BookingOfferings, Discipline: coordpg.Discipline,
	})
	f.service = scheduling.NewBooking(repo, func() time.Time { return time.UnixMicro(f.clock.Load()) })
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, slog.Default())
	auth.RegisterRoutes(mux)
	schedulinghttp.NewBooking(f.service, slog.Default()).RegisterRoutes(mux, auth)
	feedbackService := feedback.New(feedbackpg.New(db, feedbackpg.Dependencies{
		Student: identitypg.LockStudent, Session: schedulingpg.LockStudentSession,
	}), f.service)
	feedbackhttp.New(feedbackService, slog.Default()).RegisterRoutes(mux, auth)
	tutoringService := tutoring.New(tutoringpg.New(db, tutoringpg.Dependencies{}), catalog.New(catalogpg.New(db)), feedbackService)
	tutoringhttp.New(tutoringService, slog.Default()).RegisterRoutes(mux, auth)
	attendance := coordpg.NewAttendance(db, coordpg.AttendanceDependencies{
		LockStudent:            identitypg.LockStudent,
		LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty,
		HomeFaculty: func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
			return catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program)
		},
		Sessions: schedulingpg.AttendanceSessions,
	})
	coordhttp.New(coordination.NewAttendance(attendance, func() time.Time { return time.UnixMicro(f.clock.Load()) }), slog.Default()).RegisterRoutes(mux, auth)
	f.a = api{mux}
	f.exec(t, "INSERT INTO faculties (id,name) VALUES ($1,'Booking Science'),($2,'Booking Engineering')", fixture(401), fixture(402))
	f.exec(t, "INSERT INTO programs VALUES ($1,$3,'Booking Science','BACHELOR'),($2,$4,'Booking Engineering','MASTER')", fixture(403), fixture(404), fixture(401), fixture(402))
	f.exec(t, "INSERT INTO subjects VALUES ($1,'BOOK','Booking')", fixture(405))
	f.exec(t, "INSERT INTO program_subjects VALUES ($1,$3),($2,$3)", fixture(403), fixture(404), fixture(405))
	f.exec(t, "INSERT INTO semesters VALUES ($1,'Booking term','2027-01-01T00:00Z','2027-02-01T00:00Z')", fixture(406))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	users := map[int]string{bookingStudent: "book-student", bookingTutor: "book-tutor", bookingOtherTutor: "book-other-tutor",
		bookingOtherStudent: "book-other-student", bookingPending: "book-pending", bookingCoordinator: "book-coord", bookingHomeCoordinator: "book-home-coord"}
	for id, email := range users {
		f.exec(t, "INSERT INTO users (id,name,email,password_hash) VALUES ($1,$2,$3,$4)", fixture(id), email, email+"@uni.example", hash)
	}
	f.exec(t, "INSERT INTO coordinators VALUES ($1,'BOOK-C1',$3),($2,'BOOK-C2',$4)", fixture(bookingCoordinator), fixture(bookingHomeCoordinator), fixture(401), fixture(402))
	for id := bookingStudent; id <= bookingPending; id++ {
		program, faculty, coordinator := 403, 401, bookingCoordinator
		if id == bookingStudent {
			program, faculty, coordinator = 404, 402, bookingHomeCoordinator
		}
		f.exec(t, "INSERT INTO students VALUES ($1,$2,$3)", fixture(id), fixture(id), fixture(program))
		if id == bookingPending {
			continue
		}
		f.exec(t, `INSERT INTO student_verifications (student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES ($1,$2,'APPROVED',now(),$3)`, fixture(id), fixture(faculty), fixture(coordinator))
	}
	for i, tutor := range []int{bookingTutor, bookingOtherTutor} {
		f.exec(t, "INSERT INTO tutor_profiles (student_id,description,approved_at) VALUES ($1,'Fixture',now())", fixture(tutor))
		f.exec(t, `INSERT INTO tutor_applications (id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES ($1,$2,$3,'Fixture','APPROVED',now(),$4)`, fixture(421+i), fixture(tutor), fixture(401), fixture(bookingCoordinator))
		f.exec(t, "INSERT INTO tutor_qualifications (id,application_id,student_id,subject_id,grade) VALUES ($1,$2,$3,$4,18.50)", fixture(431+i), fixture(421+i), fixture(tutor), fixture(405))
		f.exec(t, "INSERT INTO tutor_offerings (id,tutor_id,subject_id,qualification_id,description) VALUES ($1,$2,$3,$4,'Fixture')", fixture(441+i), fixture(tutor), fixture(405), fixture(431+i))
		f.exec(t, `INSERT INTO availability_slots (id,tutor_id,starts_at,ends_at,allowed_mode)
			VALUES ($1,$2,'2027-01-01 00:00Z','2027-01-10 00:00Z',$3)`, fixture(451+i), fixture(tutor), []string{"BOTH", "ONLINE"}[i])
	}
	for id, email := range users {
		f.cookie[id] = f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": email + "@uni.example", "password": password}, nil).Result().Cookies()[0]
	}
	return f
}

func (f *bookingFixture) setTime(d time.Duration) { f.clock.Store(f.start.Add(d).UnixMicro()) }
func (f *bookingFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *bookingFixture) count(t *testing.T, want int, sql string, args ...any) {
	t.Helper()
	var n int
	if err := f.db.QueryRow(f.ctx, sql, args...).Scan(&n); err != nil || n != want {
		t.Fatalf("%s: got %d (%v), want %d", sql, n, err, want)
	}
}
func (f *bookingFixture) payload(at time.Duration, offering, slot int) map[string]any {
	return map[string]any{"offering_id": fixture(offering), "availability_slot_id": fixture(slot),
		"starts_at": f.start.Add(at).Format(time.RFC3339), "duration_minutes": 60, "mode": "ONLINE",
		"purpose": "EXAM_PREPARATION", "description": "Practice"}
}
func (f *bookingFixture) request(t *testing.T, code, actor int, at time.Duration, offering, slot int) string {
	t.Helper()
	w := f.a.call(t, code, "POST", "/sessions", f.payload(at, offering, slot), f.cookie[actor])
	if code != 201 {
		return ""
	}
	return fields(t, w)["data"].(map[string]any)["id"].(string)
}
func (f *bookingFixture) session(t *testing.T, code int, method, path string, body any, actor int) map[string]any {
	t.Helper()
	w := f.a.call(t, code, method, path, body, f.cookie[actor])
	if code >= 400 {
		return nil
	}
	assertPrivate(t, w.Body.String())
	return fields(t, w)["data"].(map[string]any)
}

const tutorRequestsPath = "/tutors/me/session-requests/"

var testMeeting = map[string]string{"meeting_link": "https://example.edu/meeting"}
var testReason = map[string]string{"reason": "Schedule changed"}

func TestBookingLifecycle(t *testing.T) {
	f := newBookingFixture(t)
	f.request(t, 403, bookingPending, 8*time.Hour, bookingOffer, bookingSlot)
	f.request(t, 422, bookingTutor, 8*time.Hour, bookingOffer, bookingSlot)
	f.request(t, 422, bookingStudent, 2*time.Hour+59*time.Minute, bookingOffer, bookingSlot)
	bad := f.payload(8*time.Hour, bookingOtherOffer, bookingOtherSlot)
	bad["mode"] = "ONSITE"
	f.a.call(t, 422, "POST", "/sessions", bad, f.cookie[bookingStudent])
	f.request(t, 422, bookingStudent, 215*time.Hour+30*time.Minute, bookingOffer, bookingSlot)
	f.request(t, 404, bookingStudent, 8*time.Hour, bookingOffer, bookingOtherSlot)
	f.exec(t, "DELETE FROM program_subjects WHERE program_id=$1 AND subject_id=$2", fixture(404), fixture(405))
	f.request(t, 422, bookingStudent, 8*time.Hour, bookingOffer, bookingSlot)
	f.exec(t, "INSERT INTO program_subjects VALUES ($1,$2)", fixture(404), fixture(405))

	id := f.request(t, 201, bookingStudent, 8*time.Hour, bookingOffer, bookingSlot)
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND from_status IS NULL AND changed_by_user_id=$2", id, fixture(bookingStudent))
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='NEW_REQUEST' AND recipient_user_id=$2", id, fixture(bookingTutor))
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND faculty_id=$2 AND expires_at=$3", id, fixture(401), f.start.Add(time.Hour))
	if got := f.session(t, 200, "GET", "/sessions/"+id, nil, bookingStudent); got["viewer_role"] != "STUDENT" {
		t.Fatal(got)
	}
	f.session(t, 404, "GET", "/sessions/"+id, nil, bookingOtherTutor)
	f.session(t, 403, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingStudent)
	f.session(t, 404, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingOtherTutor)
	f.session(t, 422, "POST", tutorRequestsPath+id+"/accept", map[string]string{"meeting_link": "javascript:alert(1)"}, bookingTutor)
	pending := fields(t, f.a.call(t, 200, "GET", strings.TrimSuffix(tutorRequestsPath, "/")+"?limit=1", nil, f.cookie[bookingTutor]))["data"].([]any)
	if len(pending) != 1 || pending[0].(map[string]any)["viewer_role"] != "TUTOR" {
		t.Fatal(pending)
	}
	f.request(t, 409, bookingStudent, 8*time.Hour, bookingOtherOffer, bookingOtherSlot)
	f.request(t, 409, bookingTutor, 8*time.Hour, bookingOtherOffer, bookingOtherSlot)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE starts_at=$1", f.start.Add(8*time.Hour))
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE starts_at=$1", f.start.Add(8*time.Hour))

	// An outbox uniqueness failure happens after the session mutation and audit.
	key := "session:" + id + ":REQUEST_ACCEPTED"
	f.exec(t, `INSERT INTO notifications (recipient_user_id,type,subject,body,deduplication_key)
		VALUES ($1,'REQUEST_ACCEPTED','Fixture','Fixture',$2)`, fixture(bookingStudent), key)
	f.session(t, 409, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='REQUESTED' AND meeting_link IS NULL", id)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
	f.exec(t, "DELETE FROM notifications WHERE recipient_user_id=$1 AND deduplication_key=$2", fixture(bookingStudent), key)
	f.exec(t, `UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Fixture' WHERE id=$1`, fixture(bookingTutor), fixture(bookingCoordinator))
	f.session(t, 403, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
	f.request(t, 403, bookingStudent, 9*time.Hour, bookingOffer, bookingSlot)
	f.exec(t, "UPDATE users SET is_suspended=false WHERE id=$1", fixture(bookingTutor))
	accepted := f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, bookingTutor)
	if accepted["status"] != "ACCEPTED" || accepted["meeting_link"] != testMeeting["meeting_link"] {
		t.Fatal(accepted)
	}
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)

	next := f.request(t, 201, bookingStudent, 9*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 422, "POST", tutorRequestsPath+next+"/decline", map[string]string{"reason": " "}, bookingTutor)
	f.exec(t, `CREATE FUNCTION reject_test_booking_history() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN IF NEW.to_status='DECLINED' THEN RAISE EXCEPTION 'fixture failure'; END IF; RETURN NEW; END; $$;
		CREATE TRIGGER reject_test_booking_history BEFORE INSERT ON session_status_history
		FOR EACH ROW EXECUTE FUNCTION reject_test_booking_history();`)
	f.session(t, 500, "POST", tutorRequestsPath+next+"/decline", testReason, bookingTutor)
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='REQUESTED'", next)
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", next)
	f.count(t, 0, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='REQUEST_DECLINED'", next)
	f.exec(t, "DROP TRIGGER reject_test_booking_history ON session_status_history; DROP FUNCTION reject_test_booking_history();")
	f.session(t, 200, "POST", tutorRequestsPath+next+"/decline", testReason, bookingTutor)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", next)
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='REQUEST_DECLINED'", next)

	f.session(t, 422, "POST", "/sessions/"+id+"/cancel", map[string]string{"reason": " "}, bookingStudent)
	cancelled := f.session(t, 200, "POST", "/sessions/"+id+"/cancel", testReason, bookingStudent)
	if cancelled["is_late_cancellation"] != true || cancelled["cancelled_by_user_id"] != fixture(bookingStudent) {
		t.Fatal(cancelled)
	}
	f.session(t, 409, "POST", "/sessions/"+id+"/cancel", testReason, bookingStudent)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", id)
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='SESSION_CANCELLED' AND recipient_user_id=$2", id, fixture(bookingTutor))
	f.count(t, 1, "SELECT count(*) FROM user_violations WHERE user_id=$1 AND session_id=$2 AND kind='LATE_CANCELLATION'", fixture(bookingStudent), id)
	f.count(t, 0, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1", fixture(bookingStudent))
	second := f.request(t, 201, bookingStudent, 9*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", "/sessions/"+second+"/cancel", testReason, bookingStudent)
	f.count(t, 1, "SELECT count(*) FROM user_semester_flags WHERE user_id=$1 AND semester_id=$2 AND faculty_id=$3", fixture(bookingStudent), fixture(406), fixture(402))
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE type='USER_FLAGGED' AND recipient_user_id=$1", fixture(bookingHomeCoordinator))
	f.count(t, 1, "SELECT count(*) FROM users WHERE id=$1 AND NOT is_suspended", fixture(bookingStudent))
	early := f.request(t, 201, bookingStudent, 24*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", "/sessions/"+early+"/cancel", testReason, bookingTutor)
	f.count(t, 0, "SELECT count(*) FROM user_violations WHERE session_id=$1", early)

	// The same person appears in different roles in their session list.
	f.request(t, 201, bookingTutor, 12*time.Hour, bookingOtherOffer, bookingOtherSlot)
	list := fields(t, f.a.call(t, 200, "GET", "/sessions/me?limit=25", nil, f.cookie[bookingTutor]))["data"].([]any)
	roles := map[string]bool{}
	for _, value := range list {
		roles[value.(map[string]any)["viewer_role"].(string)] = true
	}
	if !roles["TUTOR"] || !roles["STUDENT"] {
		t.Fatal(roles)
	}

	held := f.request(t, 201, bookingOtherStudent, 15*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", tutorRequestsPath+held+"/accept", testMeeting, bookingTutor)
	expired := f.request(t, 201, bookingStudent, 20*time.Hour, bookingOffer, bookingSlot)
	f.setTime(time.Hour)
	outcomes := make(chan int, 2)
	go func() {
		outcomes <- f.a.request("POST", tutorRequestsPath+expired+"/accept", origin, testMeeting, f.cookie[bookingTutor]).Code
	}()
	go func() {
		outcomes <- f.a.request("POST", tutorRequestsPath+expired+"/decline", origin, testReason, f.cookie[bookingTutor]).Code
	}()
	n, err := f.service.ExpireDueRequests(f.ctx, f.start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if x, y := <-outcomes, <-outcomes; x != 409 || y != 409 {
		t.Fatalf("responses at expiry: %d, %d", x, y)
	}
	// SKIP LOCKED may leave a row for the next bounded batch.
	remaining, err := f.service.ExpireDueRequests(f.ctx, f.start.Add(time.Hour))
	if err != nil || n+remaining != 2 {
		t.Fatalf("expire: %d + %d, %v", n, remaining, err)
	}
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='ACCEPTED'", held)
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", held)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", expired)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='EXPIRED' AND actor_kind='SYSTEM' AND changed_by_user_id IS NULL", expired)
	f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='REQUEST_EXPIRED'", expired)
	if n, err := f.service.ExpireDueRequests(f.ctx, f.start.Add(time.Hour)); err != nil || n != 0 {
		t.Fatalf("repeat expiry: %d, %v", n, err)
	}
	t.Run("concurrent requests and eligibility", f.testConcurrentRequests)
	t.Run("expiration workers", f.testExpirationWorkers)
	t.Run("completion and attendance", f.testAttendance)
	t.Run("feedback and tutor activity", f.testFeedback)
}

func (f *bookingFixture) testConcurrentRequests(t *testing.T) {
	type attempt struct{ actor, offering, slot int }
	race := func(at time.Duration, a, b attempt) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan int, 2)
		for _, v := range []attempt{a, b} {
			go func() {
				<-start
				results <- f.a.request("POST", "/sessions", origin, f.payload(at, v.offering, v.slot), f.cookie[v.actor]).Code
			}()
		}
		close(start)
		x, y := <-results, <-results
		if !((x == 201 && y == 409) || (x == 409 && y == 201)) {
			t.Fatalf("concurrent bookings at %s: %d, %d", at, x, y)
		}
		f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE starts_at=$1", f.start.Add(at))
		f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE starts_at=$1", f.start.Add(at))
	}
	first := attempt{bookingStudent, bookingOffer, bookingSlot}
	race(30*time.Hour, first, attempt{bookingOtherStudent, bookingOffer, bookingSlot})
	race(32*time.Hour, first, attempt{bookingStudent, bookingOtherOffer, bookingOtherSlot})
	race(34*time.Hour, first, attempt{bookingTutor, bookingOtherOffer, bookingOtherSlot})

	// A real concurrent raw writer holds the same slot row used by the
	// booking trigger. Start the API request while DELETE is uncommitted.
	slot := fixture(453)
	f.exec(t, `INSERT INTO availability_slots (id,tutor_id,starts_at,ends_at,allowed_mode)
		VALUES ($1,$2,'2027-01-11 00:00Z','2027-01-12 00:00Z','BOTH')`, slot, fixture(bookingTutor))
	f.raceLockedChange(t, `DELETE FROM availability_slots WHERE id=$1`, slot,
		f.payload(241*time.Hour, bookingOffer, 453), "FROM availability_slots WHERE id=", 404)
	f.count(t, 0, "SELECT count(*) FROM tutoring_sessions WHERE starts_at=$1", f.start.Add(241*time.Hour))

	// Deactivation winning after initial offer lookup must still block booking.
	f.raceLockedChange(t, `UPDATE tutor_offerings SET is_active=false,deactivated_at=now(),deactivation_reason='Fixture'
		WHERE id=$1`, fixture(bookingOtherOffer), f.payload(36*time.Hour, bookingOtherOffer, bookingOtherSlot),
		"FOR SHARE OF o,tp,q,a", 422)
	f.request(t, 422, bookingStudent, 37*time.Hour, bookingOtherOffer, bookingOtherSlot)
	f.count(t, 0, "SELECT count(*) FROM tutoring_sessions WHERE starts_at=$1", f.start.Add(36*time.Hour))

	// One valid tutor response can win while the request is still pending.
	id := f.request(t, 201, bookingStudent, 38*time.Hour, bookingOffer, bookingSlot)
	results := make(chan int, 2)
	go func() {
		results <- f.a.request("POST", tutorRequestsPath+id+"/accept", origin, testMeeting, f.cookie[bookingTutor]).Code
	}()
	go func() {
		results <- f.a.request("POST", tutorRequestsPath+id+"/decline", origin, testReason, f.cookie[bookingTutor]).Code
	}()
	x, y := <-results, <-results
	if !((x == 200 && y == 409) || (x == 409 && y == 200)) {
		t.Fatalf("concurrent responses: %d, %d", x, y)
	}
	f.count(t, 2, "SELECT count(*) FROM session_status_history WHERE session_id=$1", id)
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type IN ('REQUEST_ACCEPTED','REQUEST_DECLINED')", id)
}

func (f *bookingFixture) raceLockedChange(t *testing.T, sql, arg string, input map[string]any, queryFragment string, want int) {
	t.Helper()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err := tx.Exec(f.ctx, sql, arg); err != nil {
		t.Fatal(err)
	}
	result := make(chan *http.Response, 1)
	go func() { result <- f.a.request("POST", "/sessions", origin, input, f.cookie[bookingStudent]).Result() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		err := f.db.QueryRow(f.ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE strpos(query,$1)>0 AND cardinality(pg_blocking_pids(pid))>0)`, queryFragment).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("booking did not wait on the changed row")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	response := <-result
	defer response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("expected %d after concurrent change, got %d", want, response.StatusCode)
	}
}

func (f *bookingFixture) testExpirationWorkers(t *testing.T) {
	f.setTime(80 * time.Hour)
	w := workers.Expiration{Service: f.service, BatchSize: 5, Now: func() time.Time { return time.UnixMicro(f.clock.Load()) }}
	if _, err := w.RunBatch(f.ctx); err != nil {
		t.Fatal(err)
	}
	due := f.request(t, 201, bookingStudent, 85*time.Hour, bookingOffer, bookingSlot)
	racing := f.request(t, 201, bookingStudent, 86*time.Hour, bookingOffer, bookingSlot)
	accepted := f.request(t, 201, bookingStudent, 87*time.Hour, bookingOffer, bookingSlot)
	f.session(t, 200, "POST", tutorRequestsPath+accepted+"/accept", testMeeting, bookingTutor)
	if n, err := w.RunBatch(f.ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	f.setTime(81 * time.Hour)

	// A duplicate outbox key aborts the entire bounded expiration batch.
	key := "session:" + due + ":REQUEST_EXPIRED"
	f.exec(t, `INSERT INTO notifications (recipient_user_id,type,subject,body,deduplication_key)
		VALUES ($1,'REQUEST_EXPIRED','Fixture','Fixture',$2)`, fixture(bookingStudent), key)
	if n, err := w.RunBatch(f.ctx); err == nil || n != 0 {
		t.Fatal(n, err)
	}
	f.count(t, 2, "SELECT count(*) FROM tutoring_sessions WHERE id=ANY($1::text[]::uuid[]) AND status='REQUESTED'", []string{due, racing})
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", due)
	f.count(t, 0, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='EXPIRED'", due)
	f.exec(t, "DELETE FROM notifications WHERE recipient_user_id=$1 AND deduplication_key=$2", fixture(bookingStudent), key)

	// An abandoned transaction releases its row locks on rollback, letting a
	// different worker retry it. Before that, SKIP LOCKED must leave it alone.
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err := tx.Exec(f.ctx, "SELECT id FROM tutoring_sessions WHERE id=ANY($1::text[]::uuid[]) FOR UPDATE", []string{due, racing}); err != nil {
		t.Fatal(err)
	}
	if n, err := w.RunBatch(f.ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err := tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			workerCtx, stop := context.WithCancel(f.ctx)
			defer stop()
			workers.Poll(workerCtx, time.Second, func(ctx context.Context) {
				_, err := w.RunBatch(ctx)
				outcomes <- err
				stop()
			})
		}()
	}
	cancelResult := make(chan int, 1)
	go func() {
		<-start
		cancelResult <- f.a.request("POST", "/sessions/"+racing+"/cancel", origin, testReason, f.cookie[bookingStudent]).Code
	}()
	close(start)
	for range 2 {
		if err := <-outcomes; err != nil {
			t.Fatal(err)
		}
	}
	code := <-cancelResult
	if code != 200 && code != 409 {
		t.Fatalf("cancellation race returned %d", code)
	}
	f.count(t, 1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND status='EXPIRED'", due)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status='EXPIRED'", due)
	f.count(t, 2, "SELECT count(*) FROM notifications WHERE session_id=$1 AND type='REQUEST_EXPIRED'", due)
	f.count(t, 1, "SELECT count(*) FROM session_status_history WHERE session_id=$1 AND to_status IN ('EXPIRED','CANCELLED')", racing)
	f.count(t, 0, "SELECT count(*) FROM session_participant_blocks WHERE session_id=ANY($1::text[]::uuid[])", []string{due, racing})
	f.count(t, 2, "SELECT count(*) FROM session_participant_blocks WHERE session_id=$1", accepted)
	if n, err := w.RunBatch(f.ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
