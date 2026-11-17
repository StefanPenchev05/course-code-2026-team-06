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
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

const origin = "https://tutti.example"
const password = "a sufficiently long passphrase"

func fixture(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

type capturedActivation struct{ email, token string }

func (m *capturedActivation) SendActivation(_ context.Context, email, token string, _ time.Duration) error {
	m.email, m.token = email, token
	return nil
}

type api struct{ handler http.Handler }

func (a api) request(method, path, from string, data any, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body []byte
	if data != nil {
		body, _ = json.Marshal(data)
	}
	r := httptest.NewRequest(method, origin+"/api/v1"+path, bytes.NewReader(body))
	if data != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if from != "" {
		r.Header.Set("Origin", from)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.handler.ServeHTTP(w, r)
	return w
}

func (a api) call(t *testing.T, code int, method, path string, data any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	w := a.request(method, path, origin, data, cookie)
	if w.Code != code {
		t.Fatalf("%s %s = %d; want %d: %s", method, path, w.Code, code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), password) || strings.Contains(w.Body.String(), "argon2id") {
		t.Fatal("response contains credential data")
	}
	return w
}

func fields(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	return obj
}

func registration(email, number, program string) map[string]string {
	return map[string]string{"name": "Test Student", "email": email, "student_number": number, "program_id": program, "password": password}
}

func TestIdentityAPI(t *testing.T) {
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_identity.sh for disposable PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	store := sessionredis.New(client)
	hasher, err := security.NewPasswords(security.Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 2)
	if err != nil {
		t.Fatal(err)
	}
	service, err := application.New(ctx, postgres.New(pool), store, hasher,
		application.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := identityhttp.New(service, origin, true, slog.Default())
	h.RegisterRoutes(mux)
	mux.Handle("GET /api/v1/test/tutoring", h.RequireAuthentication(h.RequireVerifiedStudent(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))))
	mux.Handle("GET /api/v1/test/faculty", h.RequireAuthentication(h.RequireFacultyAccess(
		func(*http.Request) (string, error) { return fixture(1), nil }, http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))))
	a := api{mux}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO faculties (id,name) VALUES ($1,'Science'),($2,'Law')", fixture(1), fixture(2))
	exec("INSERT INTO programs (id,faculty_id,name,level) VALUES ($1,$2,'Computing','BACHELOR'),($3,$4,'Law','MASTER')",
		fixture(3), fixture(1), fixture(4), fixture(2))
	exec("INSERT INTO users (id,name,email) VALUES ($1,'Coordinator A','a@uni.example'),($2,'Coordinator B','b@uni.example')", fixture(5), fixture(6))
	exec("INSERT INTO coordinators (user_id,employee_number,faculty_id) VALUES ($1,'A',$2),($3,'B',$4)", fixture(5), fixture(1), fixture(6), fixture(2))

	login := func(code int, email, secret string) *httptest.ResponseRecorder {
		t.Helper()
		return a.call(t, code, "POST", "/auth/login", map[string]string{"email": email, "password": secret}, nil)
	}
	for _, person := range []struct{ id, email string }{{fixture(5), "a@uni.example"}, {fixture(6), "b@uni.example"}} {
		mail := &capturedActivation{}
		if err := service.IssueActivation(ctx, person.id, mail); err != nil {
			t.Fatal(err)
		}
		if mail.email != person.email || mail.token == "" {
			t.Fatal("activation did not reach the coordinator address")
		}
		var digest []byte
		if err := pool.QueryRow(ctx, "SELECT token_hash FROM account_activation_tokens WHERE coordinator_id=$1", person.id).Scan(&digest); err != nil {
			t.Fatal(err)
		}
		if string(digest) == mail.token || len(digest) != 32 {
			t.Fatal("activation token not stored as a digest")
		}
		if person.id == fixture(6) {
			exec("UPDATE account_activation_tokens SET created_at=now()-interval '2 hours', expires_at=now()-interval '1 hour' WHERE coordinator_id=$1", person.id)
			a.call(t, 401, "POST", "/auth/activate-coordinator", map[string]string{"token": mail.token, "password": password}, nil)
			if err := service.IssueActivation(ctx, person.id, mail); err != nil {
				t.Fatal(err)
			}
		}
		payload := map[string]string{"token": mail.token, "password": password}
		a.call(t, 204, "POST", "/auth/activate-coordinator", payload, nil)
		a.call(t, 401, "POST", "/auth/activate-coordinator", payload, nil)
	}
	ca := login(200, "a@uni.example", password).Result().Cookies()[0]
	cb := login(200, "b@uni.example", password).Result().Cookies()[0]
	a.call(t, 204, "GET", "/test/faculty", nil, ca)
	a.call(t, 404, "GET", "/test/faculty", nil, cb)
	a.call(t, 401, "GET", "/auth/me", nil, nil)
	a.call(t, 401, "GET", "/coordinator/verifications", nil, nil)
	a.call(t, 422, "POST", "/auth/register", registration("missing@uni.example", "S0", fixture(99)), nil)

	input := registration("STUDENT@UNI.EXAMPLE", "S1", fixture(3))
	if w := a.request("POST", "/auth/register", "https://wrong.example", input, nil); w.Code != 403 {
		t.Fatalf("CSRF response %d", w.Code)
	}
	if w := a.request("POST", "/auth/register", "", input, nil); w.Code != 403 {
		t.Fatalf("missing Origin response %d", w.Code)
	}
	first := fields(t, a.call(t, 201, "POST", "/auth/register", input, nil))
	id := first["verification_id"].(string)
	if first["verification_status"] != "PENDING" {
		t.Fatal(first)
	}
	a.call(t, 409, "POST", "/auth/register", registration("student@uni.example", "S2", fixture(3)), nil)
	a.call(t, 409, "POST", "/auth/register", registration("other@uni.example", "S1", fixture(3)), nil)
	var leaked int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email='other@uni.example'").Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("failed registration left a partial account: %d, %v", leaked, err)
	}
	login(401, "student@uni.example", "wrong")
	login(401, "unknown@uni.example", password)
	response := login(200, "Student@UNI.EXAMPLE", password)
	student := response.Result().Cookies()[0]
	if !student.HttpOnly || !student.Secure || student.SameSite != http.SameSiteLaxMode || student.MaxAge != 3600 || student.Domain != "" {
		t.Fatal("unexpected cookie flags")
	}
	profile := fields(t, response)
	userID := profile["id"].(string)
	var stored string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM users WHERE id=$1", userID).Scan(&stored); err != nil || !strings.HasPrefix(stored, "$argon2id$") {
		t.Fatalf("password was not securely stored: %v", err)
	}
	a.call(t, 403, "GET", "/test/tutoring", nil, student)
	a.call(t, 403, "GET", "/coordinator/verifications", nil, student)
	a.call(t, 409, "POST", "/auth/resubmit-verification", nilRegistrationPassword(input), student)
	route := "/coordinator/verifications/" + id
	a.call(t, 404, "GET", route, nil, cb)
	a.call(t, 404, "POST", route+"/approve", nil, cb)
	a.call(t, 404, "POST", route+"/reject", map[string]string{"reason": "cross faculty"}, cb)
	if got := a.call(t, 200, "GET", "/coordinator/verifications", nil, cb).Body.String(); strings.TrimSpace(got) != "[]" {
		t.Fatal(got)
	}
	if got := a.call(t, 200, "GET", "/coordinator/verifications", nil, ca).Body.String(); !strings.Contains(got, id) {
		t.Fatal(got)
	}
	if f := fields(t, a.call(t, 200, "GET", route, nil, ca)); f["status"] != "PENDING" || f["student_number"] != "S1" {
		t.Fatal(f)
	}
	a.call(t, 422, "POST", route+"/reject", map[string]string{"reason": "   "}, ca)
	a.call(t, 204, "POST", route+"/reject", map[string]string{"reason": "Check your program"}, ca)
	if f := fields(t, a.call(t, 200, "GET", "/auth/me", nil, student)); f["verification_status"] != "REJECTED" || f["rejection_reason"] != "Check your program" {
		t.Fatal(f)
	}
	resubmit := nilRegistrationPassword(registration("updated@uni.example", "S3", fixture(4)))
	second := fields(t, a.call(t, 201, "POST", "/auth/resubmit-verification", resubmit, student))
	newID := second["verification_id"].(string)
	a.call(t, 409, "POST", "/auth/resubmit-verification", resubmit, student)
	if v := fields(t, a.call(t, 200, "GET", route, nil, ca)); v["status"] != "REJECTED" || v["email"] != nil {
		t.Fatal(v)
	}
	newRoute := "/coordinator/verifications/" + newID
	a.call(t, 404, "GET", newRoute, nil, ca)

	// A decision racing to transition the same pending row must lose with 409.
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var workers sync.WaitGroup
	for _, action := range []string{"approve", "approve"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			statuses <- a.request("POST", newRoute+"/"+action, origin, nil, cb).Code
		}()
	}
	close(start)
	workers.Wait()
	x, y := <-statuses, <-statuses
	if !((x == 204 && y == 409) || (x == 409 && y == 204)) {
		t.Fatalf("concurrent decisions: %d, %d", x, y)
	}
	a.call(t, 204, "GET", "/test/tutoring", nil, student)
	a.call(t, 409, "POST", "/auth/resubmit-verification", resubmit, student)
	exec("INSERT INTO tutor_profiles (student_id,description,approved_at) VALUES ($1,'Tutor',now())", userID)
	profile = fields(t, login(200, "updated@uni.example", password))
	roles := profile["roles"].([]any)
	if len(roles) != 2 || roles[0] != "student" || roles[1] != "tutor" || profile["verified_student"] != true {
		t.Fatal(profile)
	}

	if w := a.request("POST", "/auth/logout", "https://wrong.example", nil, student); w.Code != 403 {
		t.Fatalf("logout CSRF response %d", w.Code)
	}
	a.call(t, 204, "GET", "/test/tutoring", nil, student)
	a.call(t, 204, "POST", "/auth/logout", nil, student)
	a.call(t, 401, "GET", "/auth/me", nil, student)
	student = login(200, "updated@uni.example", password).Result().Cookies()[0]
	exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='fixture' WHERE id=$1", userID, fixture(6))
	login(403, "updated@uni.example", password)
	a.call(t, 403, "GET", "/auth/me", nil, student)
	a.call(t, 403, "GET", "/test/tutoring", nil, student)

	if err := store.Create(ctx, "expiry-test", userID, time.Second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Lookup(ctx, "expiry-test"); err != nil || got != userID {
		t.Fatalf("session lookup: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := store.Lookup(ctx, "expiry-test")
		if errors.Is(err, domain.ErrUnauthenticated) {
			break
		}
		if err != nil || time.Now().After(deadline) {
			t.Fatalf("session failed to expire: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func nilRegistrationPassword(input map[string]string) map[string]string {
	delete(input, "password")
	return input
}
