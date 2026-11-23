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

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coord "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	coorddomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	coordhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/interfaces/http"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	feedbackpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/infrastructure/postgres"
	feedbackhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/interfaces/http"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitydomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutoringdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	monitorStudent          = 3201
	monitorTutor            = 3202
	monitorSecond           = 3203
	monitorForeign          = 3204
	monitorPending          = 3205
	monitorCoordinator      = 3206
	monitorOtherCoordinator = 3207
	monitorEmptyCoordinator = 3208
	monitorNoOffering       = 3209
)

func monitoringDependencies() coordpg.MonitoringDependencies {
	return coordpg.MonitoringDependencies{Faculty: identitypg.ReadCoordinatorFaculty, Tutors: tutoringpg.Monitoring,
		Activity: feedbackpg.Monitoring, Scheduling: schedulingpg.Monitoring}
}

func newMonitoringFixture(t *testing.T) *bookingFixture {
	t.Helper()
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_docker.ps1 -MonitoringOnly")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	idService, err := identity.New(ctx, identitypg.New(db), sessionredis.New(client), hasher,
		identity.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f := &bookingFixture{ctx: ctx, db: db, start: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), cookie: map[int]*http.Cookie{}}
	f.setTime(0)
	now := func() time.Time { return time.UnixMicro(f.clock.Load()) }
	f.service = scheduling.NewBooking(schedulingpg.NewBooking(db, schedulingpg.BookingDependencies{
		LockStudent: identitypg.LockStudent, Catalog: func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
		Offerings: tutoringpg.BookingOfferings, Discipline: coordpg.Discipline}), now)
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, slog.Default())
	auth.RegisterRoutes(mux)
	schedulinghttp.NewBooking(f.service, slog.Default()).RegisterRoutes(mux, auth)
	feedbackService := feedback.New(feedbackpg.New(db, feedbackpg.Dependencies{Student: identitypg.LockStudent, Session: schedulingpg.LockStudentSession}), f.service)
	feedbackhttp.New(feedbackService, slog.Default()).RegisterRoutes(mux, auth)
	tutorService := tutoring.New(tutoringpg.New(db, tutoringpg.Dependencies{}), catalog.New(catalogpg.New(db)), feedbackService)
	schedulinghttp.New(scheduling.New(schedulingpg.New(db, identitypg.LockStudent), tutorService, now), slog.Default()).RegisterRoutes(mux, auth)
	coordhttp.NewMonitoring(coord.NewMonitoring(coordpg.NewMonitoring(db, monitoringDependencies())), slog.Default()).RegisterRoutes(mux, auth)
	attendance := coordpg.NewAttendance(db, coordpg.AttendanceDependencies{LockStudent: identitypg.LockStudent,
		LockCoordinatorFaculty: identitypg.LockCoordinatorFaculty, Sessions: schedulingpg.AttendanceSessions,
		HomeFaculty: func(ctx context.Context, tx pgx.Tx, program string) (string, error) {
			return catalog.New(catalogpg.InTransaction(tx)).GetProgramFaculty(ctx, program)
		}})
	coordhttp.New(coord.NewAttendance(attendance, now), slog.Default()).RegisterRoutes(mux, auth)
	f.a = api{mux}
	f.exec(t, "INSERT INTO faculties (id,name) VALUES ($1,'Monitoring A'),($2,'Monitoring B'),($3,'Monitoring Empty')", fixture(3101), fixture(3102), fixture(3103))
	f.exec(t, "INSERT INTO programs VALUES ($1,$3,'Monitoring A','BACHELOR'),($2,$4,'Monitoring B','MASTER')", fixture(3111), fixture(3112), fixture(3101), fixture(3102))
	f.exec(t, "INSERT INTO subjects VALUES ($1,'MON1','Monitoring One'),($2,'MON2','Monitoring Two')", fixture(3121), fixture(3122))
	f.exec(t, "INSERT INTO program_subjects VALUES ($1,$3),($1,$4),($2,$3),($2,$4)", fixture(3111), fixture(3112), fixture(3121), fixture(3122))
	f.exec(t, "INSERT INTO semesters VALUES ($1,'Monitoring term','2027-01-01Z','2027-02-01Z'),($2,'Monitoring DST','2027-03-20Z','2027-04-05Z')", fixture(3131), fixture(3132))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	for id := monitorStudent; id <= monitorNoOffering; id++ {
		f.exec(t, "INSERT INTO users (id,name,email,password_hash) VALUES ($1,$2,$3,$4)", fixture(id), "Monitor "+fixture(id), fixture(id)+"@uni.example", hash)
	}
	for i, id := range []int{monitorCoordinator, monitorOtherCoordinator, monitorEmptyCoordinator} {
		f.exec(t, "INSERT INTO coordinators VALUES ($1,$2,$3)", fixture(id), fixture(id), fixture(3101+i))
	}
	for _, id := range []int{monitorStudent, monitorTutor, monitorSecond, monitorForeign, monitorPending, monitorNoOffering} {
		program, faculty, coordinator := 3111, 3101, monitorCoordinator
		if id == monitorStudent || id == monitorForeign {
			program, faculty, coordinator = 3112, 3102, monitorOtherCoordinator
		}
		f.exec(t, "INSERT INTO students VALUES ($1,$2,$3)", fixture(id), fixture(id), fixture(program))
		if id != monitorPending {
			f.exec(t, `INSERT INTO student_verifications (student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
				VALUES ($1,$2,'APPROVED',now(),$3)`, fixture(id), fixture(faculty), fixture(coordinator))
		}
	}
	for _, id := range []int{monitorTutor, monitorSecond, monitorForeign, monitorNoOffering} {
		f.exec(t, "INSERT INTO tutor_profiles (student_id,description,approved_at) VALUES ($1,'Monitor profile',now())", fixture(id))
	}
	for i, id := range []int{monitorTutor, monitorSecond, monitorForeign, monitorTutor} {
		faculty, coordinator, subject := 3101, monitorCoordinator, 3121
		if id == monitorForeign {
			faculty, coordinator = 3102, monitorOtherCoordinator
		}
		if i == 3 {
			subject = 3122
		}
		f.exec(t, `INSERT INTO tutor_applications (id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES ($1,$2,$3,'Private qualification evidence','APPROVED',now(),$4)`, fixture(3301+i), fixture(id), fixture(faculty), fixture(coordinator))
		f.exec(t, "INSERT INTO tutor_qualifications (id,application_id,student_id,subject_id,grade) VALUES ($1,$2,$3,$4,19.37)", fixture(3311+i), fixture(3301+i), fixture(id), fixture(subject))
		f.exec(t, "INSERT INTO tutor_offerings (id,tutor_id,subject_id,qualification_id,description) VALUES ($1,$2,$3,$4,'Approved subject')", fixture(3321+i), fixture(id), fixture(subject), fixture(3311+i))
		if i < 3 {
			f.exec(t, "INSERT INTO availability_slots (id,tutor_id,starts_at,ends_at,allowed_mode) VALUES ($1,$2,'2027-01-01Z','2027-01-10Z','BOTH')", fixture(3331+i), fixture(id))
		}
	}
	for id := monitorStudent; id <= monitorNoOffering; id++ {
		f.cookie[id] = f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": fixture(id) + "@uni.example", "password": password}, nil).Result().Cookies()[0]
	}
	return f
}

func monitoringPrivacy(t *testing.T, body string) {
	t.Helper()
	for _, secret := range []string{"grade", "transcript", "qualification", "19.37", "@uni.example", "password", "student_id", "student_number", "meeting_link", "meeting_room", "Practice", "Private qualification evidence"} {
		if strings.Contains(body, secret) {
			t.Fatalf("monitoring exposed %q: %s", secret, body)
		}
	}
}

func TestCoordinatorMonitoring(t *testing.T) {
	f := newMonitoringFixture(t)
	const list = "/coordinator/tutors"
	const statsPath = "/coordinator/statistics"
	detail := list + "/" + fixture(monitorTutor)
	schedule := detail + "/schedule?from=2027-01-01T00:00:00Z&to=2027-01-10T00:00:00Z"
	read := func(path string, actor int) map[string]any {
		t.Helper()
		w := f.a.call(t, 200, "GET", path, nil, f.cookie[actor])
		monitoringPrivacy(t, w.Body.String())
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable monitoring response")
		}
		return fields(t, w)
	}
	data := func(path string, actor int) map[string]any { return read(path, actor)["data"].(map[string]any) }
	checkStats := func(v map[string]any, count, hours, ratings float64, average any) {
		t.Helper()
		if v["completed_sessions"] != count || v["hours_taught"] != hours || v["rating_count"] != ratings || v["average_rating"] != average {
			t.Fatal(v)
		}
	}
	t.Run("authorization and empty faculties", func(t *testing.T) {
		for _, path := range []string{list, detail, schedule, statsPath, detail + "/feedback"} {
			f.a.call(t, 401, "GET", path, nil, nil)
			for _, actor := range []int{monitorStudent, monitorTutor, monitorPending} {
				f.a.call(t, 403, "GET", path, nil, f.cookie[actor])
			}
		}
		for _, path := range []string{detail, schedule, detail + "/feedback"} {
			f.a.call(t, 404, "GET", path, nil, f.cookie[monitorOtherCoordinator])
		}
		foreign := f.a.call(t, 404, "GET", list+"/"+fixture(monitorForeign), nil, f.cookie[monitorCoordinator])
		missing := f.a.call(t, 404, "GET", list+"/"+fixture(999999), nil, f.cookie[monitorCoordinator])
		if foreign.Body.String() != missing.Body.String() {
			t.Fatal("resource enumeration")
		}
		checkStats(data(statsPath, monitorEmptyCoordinator), 0, 0, 0, nil)
		if len(read(list, monitorEmptyCoordinator)["data"].([]any)) != 0 {
			t.Fatal("empty faculty leaked tutors")
		}
		if len(read(list, monitorOtherCoordinator)["data"].([]any)) != 1 {
			t.Fatal("foreign faculty list")
		}
		checkStats(data(detail, monitorCoordinator), 0, 0, 0, nil)
		if data(list+"/"+fixture(monitorNoOffering), monitorCoordinator)["active"] != false {
			t.Fatal("tutor without offering eligible")
		}
	})

	var completed []string
	for _, v := range []struct {
		at                                   time.Duration
		duration, offer, slot, tutor, rating int
	}{
		{8 * time.Hour, 60, 3321, 3331, monitorTutor, 5}, {10 * time.Hour, 90, 3321, 3331, monitorTutor, 5},
		{12 * time.Hour, 120, 3324, 3331, monitorTutor, 0}, {14 * time.Hour, 30, 3322, 3332, monitorSecond, 1},
		{16 * time.Hour, 60, 3323, 3333, monitorForeign, 2},
	} {
		p := f.payload(v.at, v.offer, v.slot)
		p["duration_minutes"] = v.duration
		if v.duration == 30 {
			p["purpose"] = "INTRODUCTION"
		}
		id := f.session(t, 201, "POST", "/sessions", p, monitorStudent)["id"].(string)
		f.session(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, v.tutor)
		completed = append(completed, id)
	}
	accepted := f.request(t, 201, monitorStudent, 20*time.Hour, 3321, 3331)
	f.session(t, 200, "POST", tutorRequestsPath+accepted+"/accept", testMeeting, monitorTutor)
	cancelled := f.request(t, 201, monitorStudent, 22*time.Hour, 3321, 3331)
	f.session(t, 200, "POST", "/sessions/"+cancelled+"/cancel", testReason, monitorStudent)
	declined := f.request(t, 201, monitorStudent, 24*time.Hour, 3321, 3331)
	f.session(t, 200, "POST", tutorRequestsPath+declined+"/decline", testReason, monitorTutor)
	expired := f.request(t, 201, monitorStudent, 26*time.Hour, 3321, 3331)
	f.setTime(18 * time.Hour)
	if _, err := f.service.ExpireDueRequestsBatch(f.ctx, time.UnixMicro(f.clock.Load()), 100); err != nil {
		t.Fatal(err)
	}
	requested := f.request(t, 201, monitorStudent, 30*time.Hour, 3321, 3331)
	for i, id := range completed {
		tutor := monitorTutor
		if i == 3 {
			tutor = monitorSecond
		}
		if i == 4 {
			tutor = monitorForeign
		}
		f.session(t, 200, "POST", "/tutors/me/sessions/"+id+"/complete", nil, tutor)
		if i != 2 {
			f.session(t, 201, "POST", "/sessions/"+id+"/feedback", map[string]any{"rating": []int{5, 5, 0, 1, 2}[i], "comment": "Private monitoring feedback"}, monitorStudent)
		}
	}
	t.Run("derived totals pagination and privacy", func(t *testing.T) {
		checkStats(data(detail, monitorCoordinator), 3, 4.5, 2, float64(5))
		faculty := data(statsPath, monitorCoordinator)
		checkStats(faculty, 4, 5, 3, 3.67)
		if faculty["approved_tutors"] != float64(3) || faculty["active_tutors"] != float64(2) {
			t.Fatal(faculty)
		}
		checkStats(data(statsPath, monitorOtherCoordinator), 1, 1, 1, float64(2))
		first := read(list+"?limit=1", monitorCoordinator)
		if first["pagination"].(map[string]any)["has_more"] != true {
			t.Fatal(first)
		}
		checkStats(first["data"].([]any)[0].(map[string]any), 3, 4.5, 2, float64(5))
		second := read(list+"?limit=1&offset=1", monitorCoordinator)["data"].([]any)
		if second[0].(map[string]any)["id"] != fixture(monitorSecond) {
			t.Fatal(second)
		}
		if len(read(list+"?offset=100000", monitorCoordinator)["data"].([]any)) != 0 {
			t.Fatal("nonempty final page")
		}
		if len(read(list+"?program_id="+fixture(3112), monitorCoordinator)["data"].([]any)) != 0 {
			t.Fatal("filter escaped scope")
		}
		if len(read(list+"?subject_id="+fixture(3122), monitorCoordinator)["data"].([]any)) != 1 {
			t.Fatal("subject filtering")
		}
		d := read(detail+"?limit=1", monitorCoordinator)
		if len(d["data"].(map[string]any)["offerings"].([]any)) != 1 || d["offerings_pagination"].(map[string]any)["has_more"] != true {
			t.Fatal(d)
		}
		checkStats(data(detail+"?limit=1&offset=1", monitorCoordinator), 3, 4.5, 2, float64(5))
		w := f.a.call(t, 200, "GET", detail+"/feedback?limit=1", nil, f.cookie[monitorCoordinator])
		feedback := fields(t, w)
		if feedback["average_rating"] != float64(5) || feedback["rating_count"] != float64(2) || !strings.Contains(w.Body.String(), "Private monitoring feedback") {
			t.Fatal(feedback)
		}
		for _, path := range []string{list, detail, statsPath, schedule} {
			if strings.Contains(f.a.call(t, 200, "GET", path, nil, f.cookie[monitorCoordinator]).Body.String(), "Private monitoring feedback") {
				t.Fatal("comment outside feedback view")
			}
		}
	})
	t.Run("bounded schedules and query validation", func(t *testing.T) {
		entries := read(schedule+"&limit=100", monitorCoordinator)["data"].([]any)
		statuses := map[string]string{}
		for _, item := range entries {
			v := item.(map[string]any)
			if v["kind"] == "SESSION" {
				statuses[v["id"].(string)] = v["status"].(string)
			}
		}
		for id, want := range map[string]string{completed[0]: "COMPLETED", accepted: "ACCEPTED", requested: "REQUESTED", cancelled: "CANCELLED", declined: "DECLINED", expired: "EXPIRED"} {
			if statuses[id] != want {
				t.Fatalf("%s: %v", want, statuses)
			}
		}
		if len(entries) != 9 || len(statuses) != 8 {
			t.Fatal(entries)
		} // one availability, eight separate bookings
		if read(schedule+"&limit=1", monitorCoordinator)["pagination"].(map[string]any)["has_more"] != true {
			t.Fatal("schedule pagination")
		}
		if len(read(list+"/"+fixture(monitorNoOffering)+"/schedule?from=2027-01-01T00:00:00Z&to=2027-01-02T00:00:00Z", monitorCoordinator)["data"].([]any)) != 0 {
			t.Fatal("empty schedule")
		}
		for path, code := range map[string]int{list + "?faculty_id=" + fixture(3102): 400, list + "?limit=1&limit=2": 400, list + "?limit=x": 400,
			list + "?limit=101": 422, list + "?offset=-1": 422, list + "?offset=100001": 422, list + "?subject_id=bad": 422, detail + "?extra=1": 400,
			list + "/bad": 422, statsPath + "?faculty_id=" + fixture(3102): 400, detail + "/schedule": 422,
			detail + "/schedule?from=2027-01-01T00:00:00Z&to=2027-02-02T00:00:00Z": 422,
			detail + "/schedule?from=2027-01-02T00:00:00Z&to=2027-01-01T00:00:00Z": 422} {
			f.a.call(t, code, "GET", path, nil, f.cookie[monitorCoordinator])
		}
	})
	t.Run("recurrence exceptions DST and semesters", func(t *testing.T) {
		rule := f.session(t, 201, "POST", "/tutors/me/availability/rules", map[string]any{"semester_id": fixture(3132), "iso_weekday": 1, "local_start_time": "09:00", "local_end_time": "11:00", "time_zone": "Europe/Luxembourg", "allowed_mode": "BOTH"}, monitorTutor)
		path := detail + "/schedule?from=2027-03-20T00:00:00Z&to=2027-04-05T00:00:00Z"
		items := read(path, monitorCoordinator)["data"].([]any)
		if len(items) != 2 {
			t.Fatal(items)
		}
		for i, want := range []string{"2027-03-22T08:00:00Z", "2027-03-29T07:00:00Z"} {
			v := items[i].(map[string]any)
			if v["starts_at"] != want || v["rule_id"] != rule["id"] || v["semester_id"] != fixture(3132) || v["time_zone"] != "Europe/Luxembourg" {
				t.Fatal(v)
			}
		}
		id := items[1].(map[string]any)["id"].(string)
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/slots/"+id, nil, f.cookie[monitorTutor])
		if len(read(path, monitorCoordinator)["data"].([]any)) != 1 {
			t.Fatal("exception ignored")
		}
		f.a.call(t, 204, "DELETE", "/tutors/me/availability/rules/"+rule["id"].(string)+"/exceptions/2027-03-29", nil, f.cookie[monitorTutor])
		if len(read(path, monitorCoordinator)["data"].([]any)) != 2 {
			t.Fatal("restored occurrence absent")
		}
		// Half-open boundaries exclude a window ending exactly at from.
		if len(read(detail+"/schedule?from=2027-03-22T10:00:00Z&to=2027-03-23T10:00:00Z", monitorCoordinator)["data"].([]any)) != 0 {
			t.Fatal("range boundary")
		}
	})
	t.Run("no-show correction and consistent snapshot", func(t *testing.T) {
		claim := f.session(t, 201, "POST", "/sessions/"+completed[0]+"/no-show-reports", map[string]string{"description": "Absent tutor"}, monitorStudent)["id"].(string)
		deps := monitoringDependencies()
		deps.Faculty = func(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
			faculty, err := identitypg.ReadCoordinatorFaculty(ctx, tx, actor)
			if err != nil {
				return "", err
			}
			var isolation, readOnly string
			if err := tx.QueryRow(ctx, "SELECT current_setting('transaction_isolation'),current_setting('transaction_read_only')").Scan(&isolation, &readOnly); err != nil {
				return "", err
			}
			if isolation != "repeatable read" || readOnly != "on" {
				t.Fatal(isolation, readOnly)
			}
			// These writes commit on other connections after the read snapshot began.
			f.session(t, 200, "POST", "/coordinator/attendance-reports/"+claim+"/confirm", map[string]string{"resolution_note": "Confirmed after review"}, monitorCoordinator)
			f.exec(t, "UPDATE tutor_profiles SET is_active=false WHERE student_id=$1", fixture(monitorTutor))
			return faculty, nil
		}
		service := coord.NewMonitoring(coordpg.NewMonitoring(f.db, deps))
		old, err := service.Tutor(f.ctx, identitydomain.Principal{ID: fixture(monitorCoordinator), CoordinatorFaculty: fixture(3101)}, fixture(monitorTutor), tutoringdomain.Page{Limit: 25})
		if err != nil || !old.Active || old.CompletedSessions != 3 || old.RatingCount != 2 {
			t.Fatal(old, err)
		}
		current := data(detail, monitorCoordinator)
		checkStats(current, 2, 3.5, 1, float64(5))
		if current["active"] != false {
			t.Fatal(current)
		}
		checkStats(data(statsPath, monitorCoordinator), 3, 4, 2, float64(3))
		f.count(t, 1, "SELECT count(*) FROM feedback WHERE session_id=$1 AND rating=5", completed[0])
		found := false
		for _, item := range read(schedule, monitorCoordinator)["data"].([]any) {
			v := item.(map[string]any)
			if v["id"] == completed[0] {
				found = v["status"] == "NO_SHOW"
			}
		}
		if !found {
			t.Fatal("corrected schedule status absent")
		}
		f.exec(t, "UPDATE tutor_profiles SET is_active=true WHERE student_id=$1", fixture(monitorTutor))
	})
	t.Run("program changes preserve historical faculty", func(t *testing.T) {
		// Both programs retain both subjects, so existing offering eligibility remains valid.
		deps := monitoringDependencies()
		deps.Faculty = func(ctx context.Context, tx pgx.Tx, actor string) (string, error) {
			faculty, err := identitypg.ReadCoordinatorFaculty(ctx, tx, actor)
			if err == nil {
				f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(monitorTutor), fixture(3112))
			}
			return faculty, err
		}
		old, err := coord.NewMonitoring(coordpg.NewMonitoring(f.db, deps)).Tutor(f.ctx,
			identitydomain.Principal{ID: fixture(monitorCoordinator), CoordinatorFaculty: fixture(3101)},
			fixture(monitorTutor), tutoringdomain.Page{Limit: 25})
		if err != nil || old.FacultyID != fixture(3101) || old.CompletedSessions != 2 {
			t.Fatal(old, err)
		}
		for _, path := range []string{detail, schedule, detail + "/feedback"} {
			f.a.call(t, 404, "GET", path, nil, f.cookie[monitorCoordinator])
		}
		checkStats(data(detail, monitorOtherCoordinator), 0, 0, 0, nil)
		checkStats(data(statsPath, monitorCoordinator), 3, 4, 2, float64(3))
		for _, item := range read(schedule, monitorOtherCoordinator)["data"].([]any) {
			if item.(map[string]any)["kind"] == "SESSION" {
				t.Fatal("old faculty session leaked")
			}
		}
		feedback := fields(t, f.a.call(t, 200, "GET", detail+"/feedback", nil, f.cookie[monitorOtherCoordinator]))
		if len(feedback["data"].([]any)) != 0 || feedback["rating_count"] != float64(0) {
			t.Fatal(feedback)
		}
		// Historical session access remains with the stored tutor faculty.
		f.a.call(t, 200, "GET", "/sessions/"+completed[0]+"/feedback", nil, f.cookie[monitorCoordinator])
		f.a.call(t, 404, "GET", "/sessions/"+completed[0]+"/feedback", nil, f.cookie[monitorOtherCoordinator])
		f.exec(t, "UPDATE students SET program_id=$2 WHERE user_id=$1", fixture(monitorTutor), fixture(3111))
	})
	t.Run("fresh permissions multi-role and account eligibility", func(t *testing.T) {
		// Student verification must not disable a valid independent coordinator role.
		f.exec(t, "INSERT INTO students VALUES ($1,$2,$3)", fixture(monitorEmptyCoordinator), "monitor-dual", fixture(3111))
		read(statsPath, monitorEmptyCoordinator)
		f.exec(t, "INSERT INTO faculties (id,name) VALUES ($1,'Monitoring Reassigned')", fixture(3104))
		f.exec(t, "UPDATE coordinators SET faculty_id=$2 WHERE user_id=$1", fixture(monitorEmptyCoordinator), fixture(3104))
		if data(statsPath, monitorEmptyCoordinator)["faculty_id"] != fixture(3104) {
			t.Fatal("stale faculty")
		}
		service := coord.NewMonitoring(coordpg.NewMonitoring(f.db, monitoringDependencies()))
		stale := identitydomain.Principal{ID: fixture(monitorEmptyCoordinator), CoordinatorFaculty: fixture(3101)}
		got, err := service.Statistics(f.ctx, stale)
		if err != nil || got.FacultyID != fixture(3104) || got.CompletedSessions != 0 {
			t.Fatal(got, err)
		}
		for _, test := range []struct{ change, restore string }{
			{"UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$1,suspension_reason='Fixture' WHERE id=$1", "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1"},
			{"UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1"},
			{"DELETE FROM coordinators WHERE user_id=$1", "INSERT INTO coordinators VALUES ($1,'MON-restored','00000000-0000-0000-0000-000000003104')"},
		} {
			f.exec(t, test.change, fixture(monitorEmptyCoordinator))
			for _, path := range []string{list, detail, schedule, statsPath} {
				f.a.call(t, 403, "GET", path, nil, f.cookie[monitorEmptyCoordinator])
			}
			if _, err := service.Statistics(f.ctx, stale); !errors.Is(err, coorddomain.ErrForbidden) {
				t.Fatal("stale principal authorized", err)
			}
			f.exec(t, test.restore, fixture(monitorEmptyCoordinator))
		}
		for _, test := range []struct{ change, restore string }{
			{"UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id='00000000-0000-0000-0000-000000003206',suspension_reason='Fixture' WHERE id=$1", "UPDATE users SET is_suspended=false,suspended_at=NULL,suspended_by_coordinator_id=NULL,suspension_reason=NULL WHERE id=$1"},
			{"UPDATE users SET closed_at=now() WHERE id=$1", "UPDATE users SET closed_at=NULL WHERE id=$1"},
			{"UPDATE tutor_profiles SET approval_ended_at=now() WHERE student_id=$1", "UPDATE tutor_profiles SET approval_ended_at=NULL WHERE student_id=$1"},
		} {
			f.exec(t, test.change, fixture(monitorTutor))
			if data(detail, monitorCoordinator)["active"] != false {
				t.Fatal("ineligible tutor active")
			}
			f.exec(t, test.restore, fixture(monitorTutor))
		}
		f.exec(t, `INSERT INTO student_verifications (student_id,faculty_id,status) VALUES ($1,$2,'PENDING')`, fixture(monitorTutor), fixture(3101))
		if data(detail, monitorCoordinator)["active"] != false {
			t.Fatal("latest pending verification ignored")
		}
		f.a.call(t, 204, "POST", "/auth/logout", nil, f.cookie[monitorEmptyCoordinator])
		for _, path := range []string{list, detail, schedule, statsPath} {
			f.a.call(t, 401, "GET", path, nil, f.cookie[monitorEmptyCoordinator])
		}
	})
}
