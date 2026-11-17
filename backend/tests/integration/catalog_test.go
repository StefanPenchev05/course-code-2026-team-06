package integration_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	catalogpostgres "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	cataloghttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/interfaces/http"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identitypostgres "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
)

//go:embed catalog_fixture.sql
var catalogFixture string

func TestCatalogAPI(t *testing.T) {
	dsn, redisURL := os.Getenv("TUTTI_TEST_DATABASE_URL"), os.Getenv("TUTTI_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Skip("run tests/integration/test_identity.sh for disposable PostgreSQL and Redis")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, catalogFixture); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	hasher, err := security.NewPasswords(security.Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	authService, err := identity.New(ctx, identitypostgres.New(pool), sessionredis.New(client), hasher,
		identity.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	auth := identityhttp.New(authService, origin, true, slog.Default())
	auth.RegisterRoutes(mux)
	service := catalog.New(catalogpostgres.New(pool))
	cataloghttp.New(service, slog.Default()).RegisterRoutes(mux, auth.RequireAuthentication)
	a := api{mux}

	faculty := "/faculties/" + fixture(101)
	program := "/programs/" + fixture(111)
	subject := "/subjects/" + fixture(121)
	for _, path := range []string{"/faculties", faculty + "/programs", program, program + "/subjects", "/subjects", subject} {
		a.call(t, 401, "GET", path, nil, nil)
	}

	// Registration can discover actual IDs before creating an account.
	publicFaculties := a.call(t, 200, "GET", "/public/faculties", nil, nil)
	facultyRows := fields(t, publicFaculties)["data"].([]any)
	var demoIDs []string
	for _, value := range facultyRows {
		row := value.(map[string]any)
		if len(row) != 2 || row["name"] == nil || row["id"] == nil {
			t.Fatalf("unexpected public faculty fields: %v", row)
		}
		id := row["id"].(string)
		if id == fixture(101) || id == fixture(102) || id == fixture(103) {
			demoIDs = append(demoIDs, id)
		}
	}
	if !reflect.DeepEqual(demoIDs, []string{fixture(101), fixture(102), fixture(103)}) {
		t.Fatal(demoIDs)
	}
	publicPrograms := a.call(t, 200, "GET", "/public"+faculty+"/programs?level=BACHELOR", nil, nil)
	list := fields(t, publicPrograms)["data"].([]any)
	if len(list) != 1 {
		t.Fatal(list)
	}
	item := list[0].(map[string]any)
	if len(item) != 4 || item["faculty_id"] != fixture(101) || item["id"] != fixture(111) ||
		item["name"] != "Computing" || item["level"] != "BACHELOR" {
		t.Fatal(item)
	}
	a.call(t, 201, "POST", "/auth/register", registration("catalog@uni.example", "CAT101", item["id"].(string)), nil)
	login := a.call(t, 200, "POST", "/auth/login", map[string]string{"email": "catalog@uni.example", "password": password}, nil)
	cookie := login.Result().Cookies()[0]

	if got := a.call(t, 200, "GET", "/faculties", nil, cookie).Body.String(); got != publicFaculties.Body.String() {
		t.Fatal(got)
	}
	if got := a.call(t, 200, "GET", faculty+"/programs?level=BACHELOR", nil, cookie).Body.String(); got != publicPrograms.Body.String() {
		t.Fatal(got)
	}
	a.call(t, 405, "POST", "/public/faculties", nil, nil)
	a.call(t, 405, "POST", faculty+"/programs", nil, cookie)
	a.call(t, 405, "DELETE", subject, nil, cookie)
	a.call(t, 404, "GET", "/public/subjects", nil, nil)
	a.call(t, 404, "GET", "/public/faculties/"+fixture(199)+"/programs", nil, nil)
	if f := fields(t, a.call(t, 200, "GET", "/public/faculties/"+fixture(103)+"/programs", nil, nil)); len(f["data"].([]any)) != 0 {
		t.Fatal(f)
	}
	programs := fields(t, a.call(t, 200, "GET", faculty+"/programs", nil, cookie))["data"].([]any)
	if len(programs) != 2 || programs[0].(map[string]any)["id"] != fixture(113) || programs[1].(map[string]any)["id"] != fixture(111) {
		t.Fatal(programs)
	}
	masters := fields(t, a.call(t, 200, "GET", faculty+"/programs?level=MASTER", nil, cookie))["data"].([]any)
	if len(masters) != 1 || masters[0].(map[string]any)["id"] != fixture(113) {
		t.Fatal(masters)
	}
	a.call(t, 422, "GET", faculty+"/programs?level=PHD", nil, cookie)
	detail := fields(t, a.call(t, 200, "GET", program, nil, cookie))["data"].(map[string]any)
	if detail["id"] != fixture(111) || detail["faculty"].(map[string]any)["id"] != fixture(101) {
		t.Fatal(detail)
	}
	a.call(t, 404, "GET", "/programs/"+fixture(199), nil, cookie)
	a.call(t, 404, "GET", "/programs/"+fixture(199)+"/subjects", nil, cookie)
	a.call(t, 404, "GET", "/subjects/"+fixture(199), nil, cookie)
	a.call(t, 400, "GET", "/programs/bad", nil, cookie)

	type page struct {
		Data       []catalogdomain.Subject
		Pagination struct {
			Limit, Offset int
			HasMore       bool `json:"has_more"`
		}
	}
	getPage := func(path string) page {
		t.Helper()
		var p page
		if err := json.Unmarshal(a.call(t, 200, "GET", path, nil, cookie).Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Data == nil {
			t.Fatalf("null collection for %s", path)
		}
		return p
	}
	shared := getPage(program + "/subjects").Data
	if len(shared) != 2 || shared[1].ID != fixture(121) {
		t.Fatal(shared)
	}
	other := getPage("/programs/" + fixture(112) + "/subjects?q=deMO202").Data
	if len(other) != 1 || other[0] != shared[1] {
		t.Fatal(other)
	}
	if p := getPage("/programs/" + fixture(113) + "/subjects"); len(p.Data) != 0 || p.Pagination.HasMore {
		t.Fatal(p)
	}
	if p := getPage(program + "/subjects?offset=10"); len(p.Data) != 0 {
		t.Fatal(p)
	}
	if p := getPage("/subjects?q=%25"); len(p.Data) != 0 {
		t.Fatal(p)
	}
	p1 := getPage("/subjects?q=aLgOrItHm&limit=1")
	p2 := getPage("/subjects?q=aLgOrItHm&limit=1&offset=1")
	if len(p1.Data) != 1 || p1.Data[0].ID != fixture(122) || !p1.Pagination.HasMore ||
		len(p2.Data) != 1 || p2.Data[0].ID != fixture(121) || p2.Pagination.HasMore || p2.Pagination.Offset != 1 || p2.Pagination.Limit != 1 {
		t.Fatalf("incorrect pages: %+v, %+v", p1, p2)
	}
	if p := getPage("/subjects?q=demO202"); len(p.Data) != 1 || p.Data[0] != shared[1] {
		t.Fatal(p)
	}
	canonical := fields(t, a.call(t, 200, "GET", subject, nil, cookie))["data"].(map[string]any)
	if len(canonical) != 3 || canonical["id"] != fixture(121) || canonical["code"] != "DEMO202" {
		t.Fatal(canonical)
	}

	for _, path := range []string{"/subjects?limit=0", "/subjects?limit=101", "/subjects?offset=-1", "/subjects?q=" + strings.Repeat("a", 101)} {
		a.call(t, 422, "GET", path, nil, cookie)
	}
	for _, path := range []string{"/subjects?limit=wat", "/subjects?limit=", "/subjects?q=x&q=y", "/subjects?q=%XX"} {
		a.call(t, 400, "GET", path, nil, cookie)
	}

	var eligibility catalog.AcademicCatalog = service
	for id, want := range map[string]bool{fixture(111): true, fixture(112): true, fixture(113): false, fixture(199): false} {
		if got, err := eligibility.SubjectBelongsToProgram(ctx, id, fixture(121)); err != nil || got != want {
			t.Fatalf("membership for %s: %v, %v; want %v", id, got, err, want)
		}
	}
	if id, err := eligibility.GetProgramFaculty(ctx, fixture(112)); err != nil || id != fixture(102) {
		t.Fatalf("faculty: %s, %v", id, err)
	}
	if _, err := eligibility.GetProgramFaculty(ctx, fixture(199)); !errors.Is(err, catalogdomain.ErrProgramNotFound) {
		t.Fatal(err)
	}
}
