package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/realtime"
	events "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type realtimeReader struct {
	events chan events.Event
	done   chan struct{}
}

func realtimeRead(c *websocket.Conn) realtimeReader {
	s := realtimeReader{make(chan events.Event, 16), make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			_, wire, err := c.Read(context.Background())
			if err != nil {
				return
			}
			event, err := events.Decode(wire)
			if err != nil {
				return
			}
			s.events <- event
		}
	}()
	return s
}
func realtimeReceive(t *testing.T, s realtimeReader, e events.Event) {
	t.Helper()
	select {
	case got := <-s.events:
		if got != e {
			t.Fatal("wrong scoped event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event not delivered")
	}
}
func realtimeClosed(t *testing.T, s realtimeReader) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
		t.Fatal("revoked socket remained open")
	}
}

func TestRealtimeIdentityIntegration(t *testing.T) {
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
	service, err := application.New(ctx, postgres.New(pool), sessionredis.New(client), hasher, application.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	identity := identityhttp.New(service, origin, true, slog.Default())
	cfg := realtime.DefaultConfig()
	cfg.AuthInterval = 30 * time.Millisecond
	cfg.AuthTimeout = 200 * time.Millisecond
	cfg.PingInterval = 100 * time.Millisecond
	cfg.PingTimeout = 300 * time.Millisecond
	hub, err := realtime.NewHub(cfg)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	identity.RegisterRoutes(mux)
	mux.Handle("/ws", realtime.NewHandler(hub, identity, origin))
	server := httptest.NewServer(mux)
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := hub.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	a := api{mux}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO faculties(id,name) VALUES($1,'Realtime A'),($2,'Realtime B')", fixture(85001), fixture(85002))
	exec("INSERT INTO programs(id,faculty_id,name,level) VALUES($1,$2,'Realtime A','BACHELOR'),($3,$4,'Realtime B','BACHELOR')", fixture(85003), fixture(85001), fixture(85004), fixture(85002))
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	for n := 85101; n <= 85107; n++ {
		exec("INSERT INTO users(id,name,email,password_hash) VALUES($1,'Realtime fixture',$2,$3)", fixture(n), fixture(n)+"@uni.example", hash)
	}
	exec("INSERT INTO faculties(id,name) VALUES($1,'Realtime C'),($2,'Realtime D')", fixture(85005), fixture(85006))
	exec("INSERT INTO coordinators(user_id,employee_number,faculty_id) VALUES($1,'REALTIME-C',$2)", fixture(85107), fixture(85005))
	exec("INSERT INTO coordinators(user_id,employee_number,faculty_id) VALUES($1,'REALTIME-A',$2),($3,'REALTIME-B',$4)", fixture(85103), fixture(85001), fixture(85104), fixture(85002))
	for _, n := range []int{85101, 85102, 85105, 85106} {
		program, faculty, coordinator := 85003, 85001, 85103
		if n == 85102 {
			program, faculty, coordinator = 85004, 85002, 85104
		}
		exec("INSERT INTO students(user_id,student_number,program_id) VALUES($1,$2,$3)", fixture(n), "REALTIME-"+fixture(n), fixture(program))
		if n == 85105 {
			exec("INSERT INTO student_verifications(student_id,faculty_id) VALUES($1,$2)", fixture(n), fixture(faculty))
		} else {
			exec("INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$3)", fixture(n), fixture(faculty), fixture(coordinator))
		}
	}
	login := func(n int) *http.Cookie {
		t.Helper()
		return a.call(t, 200, "POST", "/auth/login", map[string]string{"email": fixture(n) + "@uni.example", "password": password}, nil).Result().Cookies()[0]
	}
	dial := func(cookie *http.Cookie, headers http.Header) (*websocket.Conn, *http.Response, error) {
		if headers == nil {
			headers = http.Header{"Origin": {origin}}
		}
		if cookie != nil {
			headers.Set("Cookie", cookie.Name+"="+cookie.Value)
		}
		return websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{HTTPHeader: headers})
	}
	connect := func(cookie *http.Cookie) (*websocket.Conn, realtimeReader) {
		t.Helper()
		c, _, err := dial(cookie, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.CloseNow() })
		return c, realtimeRead(c)
	}
	allow := func(context.Context, domain.Principal, events.Event) (bool, error) { return true, nil }
	event, _ := events.NewEvent(events.BookingChanged, fixture(85020), time.Now())
	publish := func(users []string, policy events.Authorize) {
		t.Helper()
		if err := hub.Publish(ctx, event, users, policy); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("cookie Origin duplicate cookie and existing REST", func(t *testing.T) {
		cookie := login(85101)
		for _, tc := range []struct {
			cookie  *http.Cookie
			headers http.Header
			want    int
		}{
			{nil, nil, 401}, {cookie, http.Header{}, 403}, {cookie, http.Header{"Origin": {"https://other.example"}}, 403},
			{cookie, http.Header{"Origin": {origin, origin}}, 403},
		} {
			c, r, err := dial(tc.cookie, tc.headers)
			if c != nil {
				c.CloseNow()
			}
			if err == nil || r == nil || r.StatusCode != tc.want {
				t.Fatalf("handshake want %d, got %v %v", tc.want, r, err)
			}
		}
		head := http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value + "; " + cookie.Name + "=" + cookie.Value}}
		c, r, err := dial(nil, head)
		if c != nil {
			c.CloseNow()
		}
		if err == nil || r.StatusCode != 401 {
			t.Fatal("duplicate session cookies accepted")
		}
		a.call(t, 200, "GET", "/auth/me", nil, cookie)
	})
	t.Run("multiple tabs and faculty boundaries", func(t *testing.T) {
		cookie := login(85101)
		c, s := connect(cookie)
		defer c.CloseNow()
		tab, ts := connect(cookie)
		defer tab.CloseNow()
		ca, as := connect(login(85103))
		defer ca.CloseNow()
		cb, bs := connect(login(85104))
		defer cb.CloseNow()
		policy := func(_ context.Context, p domain.Principal, e events.Event) (bool, error) {
			return e.EntityID == fixture(85020) && (p.ID == fixture(85101) || p.CanAccessFaculty(fixture(85001))), nil
		}
		publish([]string{fixture(85101), fixture(85103), fixture(85104)}, policy)
		realtimeReceive(t, s, event)
		realtimeReceive(t, ts, event)
		realtimeReceive(t, as, event)
		select {
		case <-bs.events:
			t.Fatal("cross-faculty event leak")
		case <-time.After(75 * time.Millisecond):
		}
	})
	t.Run("pending account gets no privileged invalidations", func(t *testing.T) {
		c, s := connect(login(85105))
		defer c.CloseNow()
		publish([]string{fixture(85105)}, func(_ context.Context, p domain.Principal, _ events.Event) (bool, error) {
			return p.VerifiedStudent(), nil
		})
		select {
		case <-s.events:
			t.Fatal("pending student received privileged event")
		case <-time.After(75 * time.Millisecond):
		}
	})
	t.Run("logout affects only that browser session", func(t *testing.T) {
		cookie := login(85101)
		c, s := connect(cookie)
		defer c.CloseNow()
		second, other := connect(login(85101))
		defer second.CloseNow()
		a.call(t, 204, "POST", "/auth/logout", nil, cookie)
		realtimeClosed(t, s)
		publish([]string{fixture(85101)}, allow)
		realtimeReceive(t, other, event)
		c2, r, err := dial(cookie, nil)
		if c2 != nil {
			c2.CloseNow()
		}
		if err == nil || r.StatusCode != 401 {
			t.Fatal("revoked session upgraded")
		}
	})
	t.Run("actual Redis expiration", func(t *testing.T) {
		cookie := login(85102)
		c, s := connect(cookie)
		defer c.CloseNow()
		digest := sha256.Sum256([]byte(cookie.Value))
		key := "tutti:identity:session:" + base64.RawURLEncoding.EncodeToString(digest[:])
		if err := client.PExpire(ctx, key, 20*time.Millisecond).Err(); err != nil {
			t.Fatal(err)
		}
		realtimeClosed(t, s)
		c2, r, err := dial(cookie, nil)
		if c2 != nil {
			c2.CloseNow()
		}
		if err == nil || r.StatusCode != 401 {
			t.Fatal("expired session upgraded")
		}
	})
	t.Run("suspension and access change", func(t *testing.T) {
		cookie := login(85106)
		c, s := connect(cookie)
		defer c.CloseNow()
		exec("UPDATE users SET is_suspended=true,suspended_at=now(),suspended_by_coordinator_id=$2,suspension_reason='Realtime fixture' WHERE id=$1", fixture(85106), fixture(85103))
		realtimeClosed(t, s)
		c2, r, err := dial(cookie, nil)
		if c2 != nil {
			c2.CloseNow()
		}
		if err == nil || r.StatusCode != 403 {
			t.Fatal("suspended session upgraded")
		}
		coordCookie := login(85107)
		coord, cs := connect(coordCookie)
		defer coord.CloseNow()
		exec("UPDATE coordinators SET faculty_id=$2 WHERE user_id=$1", fixture(85107), fixture(85006))
		realtimeClosed(t, cs)
	})
	t.Run("PostgreSQL failure closes quiet socket", func(t *testing.T) {
		isolated, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer isolated.Close()
		pgService, err := application.New(ctx, postgres.New(isolated), sessionredis.New(client), hasher, application.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		pgHub, err := realtime.NewHub(cfg)
		if err != nil {
			t.Fatal(err)
		}
		pgServer := httptest.NewServer(realtime.NewHandler(pgHub, identityhttp.New(pgService, origin, true, slog.Default()), origin))
		defer pgServer.Close()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := pgHub.Shutdown(shutdown); err != nil {
				t.Error(err)
			}
		}()
		cookie := login(85101)
		head := http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}
		c, _, err := websocket.Dial(ctx, pgServer.URL+"/ws", &websocket.DialOptions{HTTPHeader: head})
		if err != nil {
			t.Fatal(err)
		}
		defer c.CloseNow()
		s := realtimeRead(c)
		isolated.Close()
		realtimeClosed(t, s)
		c2, r, err := websocket.Dial(ctx, pgServer.URL+"/ws", &websocket.DialOptions{HTTPHeader: head})
		if c2 != nil {
			c2.CloseNow()
		}
		if err == nil || r == nil || r.StatusCode != 503 {
			t.Fatal("unavailable database accepted handshake")
		}
	})
	t.Run("Redis failure closes quiet socket", func(t *testing.T) {
		c, s := connect(login(85101))
		defer c.CloseNow()
		_ = client.Close()
		realtimeClosed(t, s)
	})
	var tasks sync.WaitGroup
	for i := 0; i < 3; i++ {
		tasks.Add(1)
		go func() {
			defer tasks.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := hub.Shutdown(shutdown); err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}()
	}
	tasks.Wait()
}
