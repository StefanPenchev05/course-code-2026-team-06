package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coord "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
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
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	tutoringhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/interfaces/http"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	suspStudent            = 5201 // home B
	suspTutor              = 5202 // home A; target in both participant roles
	suspOtherStudent       = 5203
	suspOtherTutor         = 5204 // home B
	suspPending            = 5205
	suspCoordinator        = 5206
	suspOtherCoordinator   = 5207
	suspMutableCoordinator = 5208 // faculty C, no historical approval FKs
	suspMutableTarget      = 5209 // home C
)

var suspensionReason = map[string]string{"reason": "  Révision — private suspension evidence  "}

func suspensionPath(id string, suspend bool) string {
	action := "reinstate"
	if suspend {
		action = "suspend"
	}
	return "/coordinator/users/" + id + "/" + action
}

type suspensionFixture struct {
	*bookingFixture
	logs  bytes.Buffer
	redis *redis.Client
	next  int
}

func newSuspensionFixture(t *testing.T) *suspensionFixture {
	t.Helper()
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_docker.ps1 -SuspensionOnly")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["application_name"] = "tutti_suspension_integration"
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
	f := &suspensionFixture{bookingFixture: &bookingFixture{ctx: ctx, db: db, start: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), cookie: map[int]*http.Cookie{}}, redis: client, next: 8}
	f.setTime(0)
	now := func() time.Time { return time.UnixMicro(f.clock.Load()) }
	logger := slog.New(slog.NewTextHandler(&f.logs, nil))
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, logger)
	auth.RegisterRoutes(mux)
	academic := func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) }
	f.service = scheduling.NewBooking(schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{LockStudent: identitypg.LockStudent, Catalog: academic, Offerings: tutoringpg.BookingOfferings, Discipline: coordpg.Discipline}), now)
	schedulinghttp.NewBooking(f.service, logger).RegisterRoutes(mux, auth)
	feedbackService := feedback.New(feedbackpg.New(db, feedbackpg.Dependencies{Student: identitypg.LockStudent, Session: schedulingpg.LockStudentSession}), f.service)
	feedbackhttp.New(feedbackService, logger).RegisterRoutes(mux, auth)
	tutoringService := tutoring.New(tutoringpg.New(db, tutoringpg.Dependencies{Catalog: academic, LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty}), catalog.New(catalogpg.New(db)), feedbackService)
	tutoringhttp.New(tutoringService, logger).RegisterRoutes(mux, auth)
	schedulinghttp.New(scheduling.New(schedulingpg.New(db, identitypg.LockStudent), tutoringService, now), logger).RegisterRoutes(mux, auth)
	coordhttp.NewSuspension(coord.NewSuspension(coordpg.NewSuspension(db, coordpg.SuspensionDependencies{Accounts: identitypg.SuspensionAccounts, Catalog: academic, Sessions: schedulingpg.SuspensionSessions}), now), logger).RegisterRoutes(mux, auth)
	coordhttp.NewAudit(coord.NewAudit(coordpg.NewAudit(db, coordpg.AuditDependencies{Faculty: identitypg.ReadCoordinatorFaculty, Accounts: identitypg.SuspensionHistory})), logger).RegisterRoutes(mux, auth)
	coordhttp.NewPolicyCancellation(coord.NewPolicyCancellation(coordpg.NewPolicyCancellation(db, coordpg.PolicyCancellationDependencies{LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, Sessions: schedulingpg.PolicySessions}), now), logger).RegisterRoutes(mux, auth)
	coordhttp.NewMonitoring(coord.NewMonitoring(coordpg.NewMonitoring(db, coordpg.MonitoringDependencies{Faculty: identitypg.ReadCoordinatorFaculty, Tutors: tutoringpg.Monitoring, Activity: feedbackpg.Monitoring, Scheduling: schedulingpg.Monitoring})), logger).RegisterRoutes(mux, auth)
	home := func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
		return academic(tx).GetProgramFaculty(ctx, program)
	}
	coordhttp.New(coord.NewAttendance(coordpg.NewAttendance(db, coordpg.AttendanceDependencies{LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, HomeFaculty: home, Sessions: schedulingpg.AttendanceSessions}), now), logger).RegisterRoutes(mux, auth)
	coordhttp.NewIssues(coord.NewIssues(coordpg.NewIssues(db, coordpg.IssueDependencies{LockStudent: identitypg.LockStudent, LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, HomeFaculty: home, LockParticipantSession: func(ctx context.Context, tx pgx.Tx, id, actor string) (schedulingdomain.Session, error) {
		return schedulingpg.AttendanceSessions(tx).LockParticipantSession(ctx, id, actor)
	}})), logger).RegisterRoutes(mux, auth)
	f.a = api{mux}
	for i := 1; i <= 4; i++ {
		f.exec(t, "INSERT INTO faculties(id,name) VALUES($1,$2)", fixture(5100+i), fmt.Sprintf("Suspension faculty %d", i))
		f.exec(t, "INSERT INTO programs VALUES($1,$2,$3,'BACHELOR')", fixture(5110+i), fixture(5100+i), fmt.Sprintf("Suspension program %d", i))
	}
	f.exec(t, "INSERT INTO subjects VALUES($1,'SUSP','Suspension subject')", fixture(5121))
	for i := 1; i <= 4; i++ {
		f.exec(t, "INSERT INTO program_subjects VALUES($1,$2)", fixture(5110+i), fixture(5121))
	}
	f.exec(t, "INSERT INTO semesters VALUES($1,'Suspension term','2030-01-01T00:00Z','2030-02-01T00:00Z')", fixture(5131))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	for id := suspStudent; id <= suspMutableTarget; id++ {
		f.exec(t, "INSERT INTO users(id,name,email,password_hash) VALUES($1,$2,$3,$4)", fixture(id), "Suspension "+fixture(id), fixture(id)+"@uni.example", hash)
	}
	for i, id := range []int{suspCoordinator, suspOtherCoordinator, suspMutableCoordinator} {
		f.exec(t, "INSERT INTO coordinators VALUES($1,$2,$3)", fixture(id), fixture(id), fixture(5101+i))
	}
	for _, id := range []int{suspStudent, suspTutor, suspOtherStudent, suspOtherTutor, suspPending, suspMutableTarget} {
		program, faculty, reviewer := 5111, 5101, suspCoordinator
		if id == suspStudent || id == suspOtherTutor {
			program, faculty, reviewer = 5112, 5102, suspOtherCoordinator
		}
		if id == suspMutableTarget {
			program, faculty = 5113, 5103
		}
		f.exec(t, "INSERT INTO students VALUES($1,$2,$3)", fixture(id), fixture(id), fixture(program))
		if id != suspPending && id != suspMutableTarget {
			f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$3)", fixture(id), fixture(faculty), fixture(reviewer))
		}
	}
	for i, id := range []int{suspTutor, suspOtherTutor} {
		f.exec(t, "INSERT INTO tutor_profiles(student_id,description,approved_at) VALUES($1,'Suspension tutor',now())", fixture(id))
		f.exec(t, "INSERT INTO tutor_applications(id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,$3,'Private grade evidence','APPROVED',now(),$4)", fixture(5301+i), fixture(id), fixture(5101+i), fixture(suspCoordinator+i))
		f.exec(t, "INSERT INTO tutor_qualifications(id,application_id,student_id,subject_id,grade) VALUES($1,$2,$3,$4,19.37)", fixture(5311+i), fixture(5301+i), fixture(id), fixture(5121))
		f.exec(t, "INSERT INTO tutor_offerings(id,tutor_id,subject_id,qualification_id,description) VALUES($1,$2,$3,$4,'Suspension offering')", fixture(5321+i), fixture(id), fixture(5121), fixture(5311+i))
		f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,'2030-01-01T00:00Z','2030-02-01T00:00Z','BOTH')", fixture(5331+i), fixture(id))
	}
	for id := suspStudent; id <= suspMutableTarget; id++ {
		f.cookie[id] = f.login(t, id)
	}
	return f
}
func (f *suspensionFixture) login(t *testing.T, id int) *http.Cookie {
	return f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": fixture(id) + "@uni.example", "password": password}, nil).Result().Cookies()[0]
}
func (f *suspensionFixture) book(t *testing.T, asTutor, accepted bool) string {
	f.setTime(0)
	actor, offer, slot, tutor := suspTutor, 5322, 5332, suspOtherTutor
	if asTutor {
		actor, offer, slot, tutor = suspStudent, 5321, 5331, suspTutor
	}
	id := f.request(t, 201, actor, time.Duration(f.next)*time.Hour, offer, slot)
	f.next += 2
	if accepted {
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, tutor)
	}
	return id
}
func (f *suspensionFixture) decide(t *testing.T, actor, target int, suspend bool, code int) map[string]any {
	w := f.a.call(t, code, "POST", suspensionPath(fixture(target), suspend), suspensionReason, f.cookie[actor])
	if code != 200 {
		return nil
	}
	return fields(t, w)["data"].(map[string]any)
}
func (f *suspensionFixture) snapshot(t *testing.T, tables ...string) string {
	t.Helper()
	var queries []string
	for _, table := range tables {
		queries = append(queries, "SELECT '"+table+"' AS name, (SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM "+table+" r) AS records")
	}
	var result string
	if err := f.db.QueryRow(f.ctx, "SELECT jsonb_object_agg(name,records)::text FROM ("+strings.Join(queries, " UNION ALL ")+") snapshot").Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

var suspensionStateTables = []string{"users", "user_suspension_history", "tutoring_sessions", "session_status_history", "session_participant_blocks", "notifications", "user_violations", "user_semester_flags"}
var suspensionEvidenceTables = []string{"student_verifications", "tutor_profiles", "tutor_applications", "tutor_qualifications", "tutor_offerings", "feedback", "issue_reports", "issue_status_history", "attendance_reports", "user_violations", "user_semester_flags"}
