package integration_test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

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
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	schedulinghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/interfaces/http"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutorpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
)

func TestSchedulingAvailability(t *testing.T) {
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
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	tutorService := tutoring.New(tutorpg.New(db, tutorpg.Dependencies{}), catalog.New(catalogpg.New(db)), feedback.New(feedbackpg.New(db, feedbackpg.Dependencies{}), nil))
	mux := http.NewServeMux()
	auth := identityhttp.New(idService, origin, true, slog.Default())
	auth.RegisterRoutes(mux)
	schedulinghttp.New(scheduling.New(schedulingpg.New(db, identitypg.LockStudent), tutorService,
		func() time.Time { return now }), slog.Default()).RegisterRoutes(mux, auth)
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
			t.Fatalf("%s: got %d (%v), want %d", sql, n, err, want)
		}
	}
	faculty, program, subject := fixture(301), fixture(302), fixture(303)
	term, policy := fixture(304), fixture(305)
	tutor, other, student, coordinator := fixture(311), fixture(312), fixture(313), fixture(314)
	offering := fixture(331)
	exec("INSERT INTO faculties (id,name) VALUES ($1,'Availability')", faculty)
	exec("INSERT INTO programs VALUES ($1,$2,'Availability','BACHELOR')", program, faculty)
	exec("INSERT INTO subjects VALUES ($1,'AVAIL','Availability')", subject)
	exec("INSERT INTO program_subjects VALUES ($1,$2)", program, subject)
	exec("INSERT INTO semesters VALUES ($1,'Availability term','2026-11-30 00:00Z','2026-12-21 16:00Z')", term)
	exec("INSERT INTO session_policies (id) VALUES ($1)", policy)
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	accounts := map[string]string{tutor: "schedule-a@uni.example", other: "schedule-b@uni.example", student: "schedule-student@uni.example", coordinator: "schedule-coord@uni.example"}
	for id, email := range accounts {
		exec("INSERT INTO users (id,name,email,password_hash) VALUES ($1,'Availability account',$2,$3)", id, email, hash)
	}
	exec("INSERT INTO coordinators VALUES ($1,'AV-C',$2)", coordinator, faculty)
	for id, number := range map[string]string{tutor: "AV-A", other: "AV-B", student: "AV-S"} {
		exec("INSERT INTO students VALUES ($1,$2,$3)", id, number, program)
		exec(`INSERT INTO student_verifications (student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES ($1,$2,'APPROVED',now(),$3)`, id, faculty, coordinator)
	}
	for _, f := range []struct{ user, app, q, offer string }{
		{tutor, fixture(321), fixture(323), offering}, {other, fixture(322), fixture(324), fixture(332)},
	} {
		exec("INSERT INTO tutor_profiles (student_id,description,approved_at) VALUES ($1,'Availability',now())", f.user)
		exec(`INSERT INTO tutor_applications (id,student_id,faculty_id,description,status,reviewed_at,reviewed_by_coordinator_id)
			VALUES ($1,$2,$3,'Availability','APPROVED',now(),$4)`, f.app, f.user, faculty, coordinator)
		exec("INSERT INTO tutor_qualifications (id,application_id,student_id,subject_id,grade) VALUES ($1,$2,$3,$4,18.50)", f.q, f.app, f.user, subject)
		exec("INSERT INTO tutor_offerings (id,tutor_id,subject_id,qualification_id,description) VALUES ($1,$2,$3,$4,'Availability')", f.offer, f.user, subject, f.q)
	}
	cookies := make(map[string]*http.Cookie)
	for _, id := range []string{tutor, other, student} {
		cookies[id] = a.call(t, 200, "POST", "/auth/login", map[string]string{"email": accounts[id], "password": password}, nil).Result().Cookies()[0]
	}
	const own = "/tutors/me/availability"
	freePath := "/tutors/" + tutor + "/availability"
	rules := own + "/rules"
	slots := own + "/slots"
	ruleBody := func(weekday int, start, end, mode string) map[string]any {
		return map[string]any{"semester_id": term, "iso_weekday": weekday, "local_start_time": start, "local_end_time": end,
			"time_zone": "Europe/Luxembourg", "allowed_mode": mode}
	}
	window := func(start, end string) map[string]string {
		return map[string]string{"starts_at": start, "ends_at": end, "allowed_mode": "ONSITE"}
	}
	data := func(code int, method, path string, input any, who string) map[string]any {
		t.Helper()
		return fields(t, a.call(t, code, method, path, input, cookies[who]))["data"].(map[string]any)
	}
	list := func(path, who string) []any {
		t.Helper()
		return fields(t, a.call(t, 200, "GET", path, nil, cookies[who]))["data"].([]any)
	}
	a.call(t, 401, "GET", freePath, nil, nil)
	a.call(t, 403, "POST", rules, ruleBody(1, "14:00", "17:00", "BOTH"), cookies[student])
	invalid := ruleBody(1, "14:00", "17:00", "BOTH")
	invalid["semester_id"] = fixture(399)
	a.call(t, 422, "POST", rules, invalid, cookies[tutor])
	a.call(t, 422, "POST", slots, window("2026-12-21T15:30:00Z", "2026-12-21T16:30:00Z"), cookies[tutor])
	a.call(t, 422, "POST", slots, window("2026-11-30T12:00:00Z", "2026-11-30T13:00:00Z"), cookies[tutor])
	id := data(201, "POST", rules, ruleBody(1, "14:00", "17:00", "BOTH"), tutor)["id"].(string)
	rulePath := rules + "/" + id
	exceptions := rulePath + "/exceptions"
	if got := list(rules, tutor); len(got) != 1 || got[0].(map[string]any)["semester_id"] != term {
		t.Fatal(got)
	}
	count(3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", id)
	count(0, "SELECT count(*) FROM availability_slots WHERE rule_id=$1 AND (starts_at<=$2 OR ends_at>'2026-12-21 16:00Z')", id, now)
	a.call(t, 409, "POST", rules, ruleBody(1, "15:00", "18:00", "ONLINE"), cookies[tutor])
	count(1, "SELECT count(*) FROM availability_rules WHERE tutor_id=$1", tutor)
	ownList := list(own, tutor)
	first := ownList[0].(map[string]any)
	firstID := first["id"].(string)
	if len(ownList) != 3 || first["starts_at"] != "2026-12-07T13:00:00Z" || first["is_recurring"] != true {
		t.Fatal(ownList)
	}
	a.call(t, 404, "PATCH", rulePath, map[string]any{"local_end_time": "18:00"}, cookies[other])
	a.call(t, 404, "DELETE", slots+"/"+firstID, nil, cookies[other])
	a.call(t, 404, "GET", exceptions, nil, cookies[other])
	a.call(t, 200, "PATCH", rulePath, map[string]any{"local_end_time": "17:00"}, cookies[tutor])
	count(1, "SELECT count(*) FROM availability_slots WHERE id=$1", firstID)
	a.call(t, 204, "DELETE", slots+"/"+firstID, nil, cookies[tutor])
	if got := list(exceptions, tutor); len(got) != 1 || got[0].(map[string]any)["local_date"] != "2026-12-07" {
		t.Fatal(got)
	}
	count(2, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", id)
	a.call(t, 200, "PATCH", rulePath, map[string]any{"local_end_time": "17:00"}, cookies[tutor])
	count(2, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", id)
	replacement := data(201, "POST", slots, window("2026-12-07T15:00:00Z", "2026-12-07T17:00:00Z"), tutor)
	if replacement["is_recurring"] != false {
		t.Fatal(replacement)
	}
	a.call(t, 409, "DELETE", exceptions+"/2026-12-07", nil, cookies[tutor])
	count(1, "SELECT count(*) FROM availability_exceptions WHERE rule_id=$1", id)
	a.call(t, 204, "DELETE", slots+"/"+replacement["id"].(string), nil, cookies[tutor])
	a.call(t, 204, "DELETE", exceptions+"/2026-12-07", nil, cookies[tutor])
	if len(list(exceptions, tutor)) != 0 {
		t.Fatal("restored exception remains")
	}
	adjacent := data(201, "POST", slots, window("2026-12-07T16:00:00Z", "2026-12-07T17:00:00Z"), tutor)
	a.call(t, 204, "DELETE", slots+"/"+adjacent["id"].(string), nil, cookies[tutor])

	// Two requests with overlapping Thursday schedules can leave only one rule.
	begin := make(chan struct{})
	status := make(chan int, 2)
	for _, start := range []string{"10:00", "10:30"} {
		go func() {
			<-begin
			status <- a.request("POST", rules, origin, ruleBody(4, start, "12:00", "ONLINE"), cookies[tutor]).Code
		}()
	}
	close(begin)
	x, y := <-status, <-status
	if !((x == 201 && y == 409) || (x == 409 && y == 201)) {
		t.Fatalf("concurrent overlap: %d, %d", x, y)
	}
	count(1, "SELECT count(*) FROM availability_rules WHERE tutor_id=$1 AND iso_weekday=4", tutor)

	// This fixture uses the real session guards and reserves only part of a slot.
	var bookedSlot string
	if err := db.QueryRow(ctx, "SELECT id::text FROM availability_slots WHERE rule_id=$1 AND starts_at='2026-12-07 13:00Z'", id).Scan(&bookedSlot); err != nil {
		t.Fatal(err)
	}
	bookingSQL := `INSERT INTO tutoring_sessions (id,student_id,tutor_id,offering_id,availability_slot_id,policy_id,semester_id,
		faculty_id,starts_at,ends_at,duration_minutes,mode,purpose,description,requested_at,expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,30,'ONLINE','INTRODUCTION','Fixture',
		        $11,least($11::timestamptz + interval '48 hours',$9::timestamptz - interval '24 hours'))`
	bookingArgs := func(session, slot, starts, ends, requested string) []any {
		parse := func(v string) time.Time {
			value, err := time.Parse(time.RFC3339, v)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
		return []any{session, student, tutor, offering, slot, policy, term, faculty, parse(starts), parse(ends), parse(requested)}
	}
	session := fixture(341)
	exec(bookingSQL, bookingArgs(session, bookedSlot, "2026-12-07T14:00:00Z", "2026-12-07T14:30:00Z", "2026-12-01T00:00:00Z")...)
	public := list(freePath+"?from=2026-12-07&to=2026-12-08", student)
	if len(public) != 2 || public[0].(map[string]any)["ends_at"] != "2026-12-07T14:00:00Z" ||
		public[1].(map[string]any)["starts_at"] != "2026-12-07T14:30:00Z" {
		t.Fatal(public)
	}
	if len(list(own+"?from=2026-12-07&to=2026-12-08", tutor)) != 1 {
		t.Fatal("owner should see the published window")
	}
	if w := a.call(t, 409, "DELETE", slots+"/"+bookedSlot, nil, cookies[tutor]); !strings.Contains(w.Body.String(), "active session") {
		t.Fatal(w.Body.String())
	}
	exec("UPDATE tutoring_sessions SET status='ACCEPTED',meeting_link='fixture' WHERE id=$1", session)
	a.call(t, 409, "PATCH", rulePath, map[string]any{"allowed_mode": "ONLINE"}, cookies[tutor])
	a.call(t, 409, "DELETE", rulePath, nil, cookies[tutor])
	count(3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", id)
	a.call(t, 200, "PATCH", rulePath, map[string]any{"local_start_time": "14:00"}, cookies[tutor])
	count(1, "SELECT count(*) FROM availability_slots WHERE id=$1", bookedSlot)

	// Concurrent booking and delete must never both succeed. Hold the booking's
	// transaction open while attempting the delete through the API.
	raceWindow := window("2026-12-10T18:00:00Z", "2026-12-10T19:00:00Z")
	raceWindow["allowed_mode"] = "ONLINE"
	raceSlot := data(201, "POST", slots, raceWindow, tutor)["id"].(string)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	raceSession := fixture(342)
	if _, err := tx.Exec(ctx, bookingSQL, bookingArgs(raceSession, raceSlot, "2026-12-10T18:00:00Z", "2026-12-10T18:30:00Z", "2026-12-01T00:00:00Z")...); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan int, 1)
	go func() { deleted <- a.request("DELETE", slots+"/"+raceSlot, origin, nil, cookies[tutor]).Code }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		err := db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE query LIKE 'DELETE FROM availability_slots WHERE id=ANY%' AND cardinality(pg_blocking_pids(pid))>0)`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("delete did not wait for the booking's slot lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if code := <-deleted; code != 409 {
		t.Fatalf("delete raced past active session: %d", code)
	}

	exec("UPDATE tutoring_sessions SET status='COMPLETED',completed_at=ends_at,completed_by_tutor_id=tutor_id WHERE id=$1", session)
	a.call(t, 200, "PATCH", rulePath, map[string]any{"local_end_time": "16:30"}, cookies[tutor])
	count(1, "SELECT count(*) FROM tutoring_sessions WHERE id=$1 AND availability_slot_id IS NULL AND status='COMPLETED'", session)
	count(3, "SELECT count(*) FROM availability_slots WHERE rule_id=$1 AND ends_at-starts_at=interval '2.5 hours'", id)
	historySlot := fixture(351)
	exec(`INSERT INTO availability_slots (id,tutor_id,rule_id,starts_at,ends_at,allowed_mode)
		VALUES ($1,$2,$3,'2026-11-30 13:00Z','2026-11-30 16:00Z','BOTH')`, historySlot, tutor, id)
	a.call(t, 204, "DELETE", rulePath, nil, cookies[tutor])
	count(0, "SELECT count(*) FROM availability_slots WHERE rule_id=$1", id)
	count(1, "SELECT count(*) FROM availability_slots WHERE id=$1 AND rule_id IS NULL", historySlot)
	exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='fixture' WHERE id=$1", tutor, coordinator)
	a.call(t, 403, "POST", rules, ruleBody(1, "14:00", "17:00", "BOTH"), cookies[tutor])
	a.call(t, 404, "GET", freePath, nil, cookies[student])
}
