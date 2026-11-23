package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	events "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

const testOrigin = "https://tutti.example"

func testID(n int) string { return fmt.Sprintf("00000000-0000-0000-0000-%012d", n) }

type fakeSessions struct {
	mu       sync.Mutex
	users    map[string]domain.Principal
	failures map[string]error
}

func (s *fakeSessions) SessionCheck(r *http.Request) (func(context.Context) (domain.Principal, error), error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	return func(ctx context.Context) (domain.Principal, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return domain.Principal{}, err
		}
		if err := s.failures[cookie.Value]; err != nil {
			return domain.Principal{}, err
		}
		p, ok := s.users[cookie.Value]
		if !ok {
			return domain.Principal{}, domain.ErrUnauthenticated
		}
		return p, nil
	}, nil
}
func newTestHub(t *testing.T, edit func(*Config)) (*Hub, *fakeSessions, *httptest.Server) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AuthInterval = 20 * time.Millisecond
	cfg.PingInterval = 40 * time.Millisecond
	cfg.PingTimeout = 100 * time.Millisecond
	if edit != nil {
		edit(&cfg)
	}
	h, err := NewHub(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSessions{users: map[string]domain.Principal{
		"student": {ID: testID(1), Student: true, Verification: domain.Approved},
		"other":   {ID: testID(2), Student: true, Verification: domain.Approved},
		"coordA":  {ID: testID(3), CoordinatorFaculty: testID(11)},
		"coordB":  {ID: testID(4), CoordinatorFaculty: testID(12)},
	}, failures: map[string]error{}}
	server := httptest.NewServer(NewHandler(h, s, testOrigin))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	return h, s, server
}
func dial(t *testing.T, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {testOrigin}, "Cookie": {"session=" + token}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

type stream struct {
	events chan events.Event
	done   chan struct{}
}

func readStream(c *websocket.Conn) stream {
	s := stream{make(chan events.Event, 128), make(chan struct{})}
	go func() {
		defer close(s.done)
		for {
			_, wire, err := c.Read(context.Background())
			if err != nil {
				return
			}
			e, err := events.Decode(wire)
			if err != nil {
				return
			}
			s.events <- e
		}
	}()
	return s
}
func receive(t *testing.T, s stream, want events.Event) {
	t.Helper()
	select {
	case got := <-s.events:
		if got != want {
			t.Fatalf("unexpected event %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("event not received")
	}
}
func disconnected(t *testing.T, s stream) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not close")
	}
}
func testEvent(t *testing.T) events.Event {
	t.Helper()
	e, err := events.NewEvent(events.BookingChanged, testID(100), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func allow(_ context.Context, _ domain.Principal, _ events.Event) (bool, error) { return true, nil }
func publish(t *testing.T, h *Hub, e events.Event, users []string, policy events.Authorize) {
	t.Helper()
	if err := h.Publish(context.Background(), e, users, policy); err != nil {
		t.Fatal(err)
	}
}

func TestHandshakeSecurity(t *testing.T) {
	_, s, server := newTestHub(t, nil)
	s.failures["revoked"] = domain.ErrUnauthenticated
	s.failures["suspended"] = domain.ErrForbidden
	s.failures["unavailable"] = errors.New("private Redis failure")
	for _, tc := range []struct {
		name, path, token, origin string
		want                      int
	}{
		{"missing_cookie", "/ws", "", testOrigin, 401}, {"invalid_cookie", "/ws", "unknown", testOrigin, 401},
		{"revoked", "/ws", "revoked", testOrigin, 401}, {"suspended", "/ws", "suspended", testOrigin, 403},
		{"unavailable", "/ws", "unavailable", testOrigin, 503}, {"missing_origin", "/ws", "student", "", 403},
		{"wrong_origin", "/ws", "student", "https://evil.example", 403}, {"scheme", "/ws", "student", "http://tutti.example", 403},
		{"null_origin", "/ws", "student", "null", 403}, {"claimed_scope", "/ws?faculty_id=" + testID(11), "student", testOrigin, 400},
		{"url_secret", "/ws?token=private", "student", testOrigin, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := http.Header{"Origin": {tc.origin}}
			if tc.token != "" {
				head.Set("Cookie", "session="+tc.token)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			c, r, err := websocket.Dial(ctx, server.URL+tc.path, &websocket.DialOptions{HTTPHeader: head})
			if c != nil {
				c.CloseNow()
			}
			if err == nil || r == nil || r.StatusCode != tc.want {
				t.Fatalf("got %v %v, want %d", r, err, tc.want)
			}
		})
	}
	for _, method := range []string{http.MethodHead, http.MethodPost} {
		req, _ := http.NewRequest(method, server.URL+"/ws", nil)
		req.Header.Set("Origin", testOrigin)
		r, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 405 {
			t.Fatal(r.StatusCode)
		}
	}
	// Repeated headers and subprotocols cannot smuggle an alternate scope/secret.
	for _, head := range []http.Header{
		{"Origin": {testOrigin, testOrigin}, "Cookie": {"session=student"}},
		{"Origin": {testOrigin}, "Cookie": {"session=student"}, "Sec-WebSocket-Protocol": {"faculty.any"}},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		c, _, err := websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{HTTPHeader: head})
		cancel()
		if c != nil {
			c.CloseNow()
		}
		if err == nil {
			t.Fatal("untrusted header accepted")
		}
	}
}

func TestUserAndFacultyIsolationMultipleTabs(t *testing.T) {
	h, _, server := newTestHub(t, nil)
	a := readStream(dial(t, server, "student"))
	tab := readStream(dial(t, server, "student"))
	other := readStream(dial(t, server, "other"))
	ca := readStream(dial(t, server, "coordA"))
	cb := readStream(dial(t, server, "coordB"))
	e := testEvent(t)
	policy := func(_ context.Context, p domain.Principal, event events.Event) (bool, error) {
		return event.EntityID == testID(100) && (p.ID == testID(1) || p.CanAccessFaculty(testID(11))), nil
	}
	// Even accidentally over-inclusive server candidates cannot bypass policy.
	publish(t, h, e, []string{testID(1), testID(1), testID(3), testID(4)}, policy)
	receive(t, a, e)
	receive(t, tab, e)
	receive(t, ca, e)
	select {
	case <-other.events:
		t.Fatal("cross-user leak")
	case <-cb.events:
		t.Fatal("cross-faculty leak")
	case <-a.events:
		t.Fatal("duplicate recipient delivery")
	case <-time.After(100 * time.Millisecond):
	}
	// Transport intentionally permits repeated event IDs for reconnect-safe consumers.
	publish(t, h, e, []string{testID(1)}, allow)
	receive(t, a, e)
	receive(t, tab, e)
}

func TestSessionLossAndPermissionsRevalidated(t *testing.T) {
	for _, kind := range []string{"logout", "expiry", "suspended", "closed", "faculty", "role", "program", "verification", "redis"} {
		t.Run(kind, func(t *testing.T) {
			h, s, server := newTestHub(t, func(c *Config) { c.AuthInterval = time.Minute })
			conn := dial(t, server, "student")
			stream := readStream(conn)
			// Ensure the completed-upgrade authentication has run before mutation.
			e := testEvent(t)
			publish(t, h, e, []string{testID(1)}, allow)
			receive(t, stream, e)
			s.mu.Lock()
			p := s.users["student"]
			switch kind {
			case "logout", "expiry":
				delete(s.users, "student")
			case "suspended":
				p.Suspended = true
			case "closed":
				p.Closed = true
			case "faculty":
				p.CoordinatorFaculty = testID(11)
			case "role":
				p.Tutor = true
			case "program":
				p.ProgramID = testID(12)
			case "verification":
				p.Verification = domain.Rejected
			case "redis":
				s.failures["student"] = errors.New("Redis unavailable")
			}
			if kind != "logout" && kind != "expiry" {
				s.users["student"] = p
			}
			s.mu.Unlock()
			publish(t, h, testEvent(t), []string{testID(1)}, allow)
			disconnected(t, stream)
			select {
			case <-stream.events:
				t.Fatal("event delivered after access loss")
			default:
			}
		})
	}
	// Quiet sockets also lose access without requiring an event publisher.
	_, s, server := newTestHub(t, nil)
	stream := readStream(dial(t, server, "student"))
	s.mu.Lock()
	delete(s.users, "student")
	s.mu.Unlock()
	disconnected(t, stream)
}

func TestNoClientSubscriptions(t *testing.T) {
	for _, wire := range []string{`{"subscribe":{"userId":"other","facultyId":"any"}}`, `{}`, strings.Repeat("x", events.MaxMessageBytes+1)} {
		_, _, server := newTestHub(t, nil)
		c := dial(t, server, "student")
		s := readStream(c)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Write(ctx, websocket.MessageText, []byte(wire))
		cancel()
		disconnected(t, s)
	}
}

func TestLimitsBackpressureAndConcurrentCleanup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConnections = 2
	cfg.MaxPerUser = 1
	cfg.QueueSize = 1
	h, err := NewHub(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Principal{ID: testID(1), Student: true}
	check := func(context.Context) (domain.Principal, error) { return p, nil }
	c, err := h.reserve(p, check)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.reserve(p, check); !errors.Is(err, ErrLimit) {
		t.Fatal("per-user limit missing")
	}
	other, err := h.reserve(domain.Principal{ID: testID(2)}, check)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.reserve(domain.Principal{ID: testID(3)}, check); !errors.Is(err, ErrLimit) {
		t.Fatal("global limit missing")
	}
	e := testEvent(t)
	publish(t, h, e, []string{p.ID}, allow)
	publish(t, h, e, []string{p.ID}, allow)
	if c.ctx.Err() == nil {
		t.Fatal("slow consumer not disconnected")
	}
	h.remove(c)
	h.remove(other)
	if err := h.Publish(context.Background(), e, []string{p.ID}, nil); !errors.Is(err, ErrPolicy) {
		t.Fatal("missing policy accepted")
	}
	var tasks sync.WaitGroup
	for i := 0; i < 12; i++ {
		tasks.Add(1)
		go func() {
			defer tasks.Done()
			for n := 0; n < 40; n++ {
				c, err := h.reserve(p, check)
				if err == nil {
					_ = h.Publish(context.Background(), e, []string{p.ID}, allow)
					h.remove(c)
				}
			}
		}()
	}
	tasks.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := h.reserve(p, check); !errors.Is(err, ErrClosed) {
		t.Fatal("shutdown accepted connection")
	}
	if err := h.Publish(ctx, e, []string{p.ID}, allow); !errors.Is(err, ErrClosed) {
		t.Fatal("shutdown accepted publication")
	}
}

func TestHeartbeatAndShutdown(t *testing.T) {
	h, _, server := newTestHub(t, nil)
	active := readStream(dial(t, server, "student"))
	// No Read means no automatic pong; this peer must be removed by heartbeat.
	idle := dial(t, server, "other")
	time.Sleep(250 * time.Millisecond)
	e := testEvent(t)
	publish(t, h, e, []string{testID(1)}, allow)
	receive(t, active, e)
	h.mu.Lock()
	remaining := len(h.users[testID(2)])
	h.mu.Unlock()
	if remaining != 0 {
		t.Fatal("inactive client retained")
	}
	idle.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	disconnected(t, active)
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal("shutdown not idempotent")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total != 0 || len(h.users) != 0 {
		t.Fatal("connections leaked")
	}
}
