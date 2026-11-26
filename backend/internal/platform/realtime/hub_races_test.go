package realtime

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	events "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

// Pause after authentication/reservation but before Accept returns to the hub.
// This exercises the real HTTP upgrade without adding production test hooks.
type gatedUpgrade struct {
	http.ResponseWriter
	entered chan struct{}
	release chan struct{}
}

func (w gatedUpgrade) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	close(w.entered)
	<-w.release
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

// A finished handler has returned from serve (which joins its reader/watchdog)
// and released its reservation. Do not infer goroutine cleanup from sleeps or
// process-wide goroutine counts, which include unrelated HTTP client workers.
func raceGateway(t *testing.T, entered, release chan struct{}) (*Hub, *fakeSessions, *httptest.Server, <-chan struct{}) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AuthInterval, cfg.PingInterval = time.Minute, time.Minute
	h, err := NewHub(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSessions{users: map[string]domain.Principal{
		"student": {ID: testID(1), Student: true, Verification: domain.Approved},
		"coordA":  {ID: testID(3), CoordinatorFaculty: testID(11)},
	}, failures: map[string]error{}}
	finished := make(chan struct{}, 32)
	handler := NewHandler(h, s, testOrigin)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { finished <- struct{}{} }()
		if entered != nil {
			w = gatedUpgrade{w, entered, release}
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		server.Close()
	})
	return h, s, server, finished
}

func emptyHub(t *testing.T, h *Hub) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total != 0 || len(h.users) != 0 {
		t.Fatalf("connections leaked: total=%d users=%d", h.total, len(h.users))
	}
}

func shutdownAsync(h *Hub) <-chan error {
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- h.Shutdown(ctx)
	}()
	return done
}

func awaitShutdown(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("shutdown did not join connections:", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not return within its context budget")
	}
}

// The dial must run concurrently with the blocked upgrade. Cleanup also joins
// this test-owned goroutine if an assertion fails before the handshake resumes.
func dialAsync(t *testing.T, server *httptest.Server, token string) func() *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	done := make(chan struct{})
	var conn *websocket.Conn
	var err error
	go func() {
		defer close(done)
		conn, _, err = websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": {testOrigin}, "Cookie": {"session=" + token}},
		})
	}()
	t.Cleanup(func() {
		cancel()
		waitSignal(t, done, "dial cleanup")
		if conn != nil {
			_ = conn.CloseNow()
		}
	})
	return func() *websocket.Conn {
		waitSignal(t, done, "upgrade")
		if err != nil {
			t.Fatal(err)
		}
		return conn
	}
}

func TestRegistrationDuringShutdown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	h, _, server, finished := raceGateway(t, entered, release)
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	completeDial := dialAsync(t, server, "student")
	waitSignal(t, entered, "reserved upgrade")
	h.mu.Lock()
	var reserved *connection
	for c := range h.users[testID(1)] {
		reserved = c
	}
	h.mu.Unlock()
	if reserved == nil {
		t.Fatal("upgrade was not reserved")
	}
	shutdown := shutdownAsync(h)
	waitSignal(t, reserved.ctx.Done(), "shutdown cancellation")
	select {
	case err := <-shutdown:
		t.Fatalf("shutdown returned before the reserved handler completed: %v", err)
	default:
	}
	// Shutdown has started but the earlier reserved upgrade is still in flight.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, response, err := websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {testOrigin}, "Cookie": {"session=student"}},
	})
	if c != nil {
		_ = c.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("new registration during shutdown: response=%v error=%v", response, err)
	}
	unblock()
	reader := readStream(completeDial())
	awaitShutdown(t, shutdown)
	disconnected(t, reader)
	for i := 0; i < 2; i++ {
		waitSignal(t, finished, "handler cleanup")
	}
	emptyHub(t, h)
}

func TestAccessChangeDuringUpgrade(t *testing.T) {
	for _, kind := range []string{"revoked", "suspended", "faculty"} {
		for _, queued := range []bool{false, true} {
			name := kind + "/quiet"
			if queued {
				name = kind + "/queued event"
			}
			t.Run(name, func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				h, sessions, server, finished := raceGateway(t, entered, release)
				unblock := sync.OnceFunc(func() { close(release) })
				t.Cleanup(unblock)
				token, user := "student", testID(1)
				if kind == "faculty" {
					token, user = "coordA", testID(3)
				}
				completeDial := dialAsync(t, server, token)
				waitSignal(t, entered, "initial authentication and reservation")
				sessions.mu.Lock()
				p := sessions.users[token]
				switch kind {
				case "revoked":
					delete(sessions.users, token)
				case "suspended":
					p.Suspended = true
					sessions.users[token] = p
				case "faculty":
					p.CoordinatorFaculty = testID(12)
					sessions.users[token] = p
				}
				sessions.mu.Unlock()
				var policies atomic.Int32
				if queued {
					publish(t, h, testEvent(t), []string{user}, func(context.Context, domain.Principal, events.Event) (bool, error) {
						policies.Add(1)
						return true, nil
					})
				}
				unblock()
				reader := readStream(completeDial())
				disconnected(t, reader)
				waitSignal(t, finished, "revoked connection cleanup")
				emptyHub(t, h)
				if len(reader.events) != 0 || policies.Load() != 0 {
					t.Fatal("lost access reached resource policy or event delivery")
				}
			})
		}
	}
}

func TestPublicationDuringShutdown(t *testing.T) {
	h, _, server, finished := raceGateway(t, nil, nil)
	reader := readStream(dial(t, server, "student"))
	entered, cancelled := make(chan struct{}), make(chan struct{})
	e := testEvent(t)
	publish(t, h, e, []string{testID(1)}, func(ctx context.Context, _ domain.Principal, _ events.Event) (bool, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return false, ctx.Err()
	})
	waitSignal(t, entered, "in-flight resource authorization")
	start := make(chan struct{})
	results := make(chan error, 16)
	for i := 0; i < cap(results); i++ {
		go func() {
			<-start
			results <- h.Publish(context.Background(), e, []string{testID(1)}, allow)
		}()
	}
	shutdown := shutdownAsync(h)
	close(start)
	waitSignal(t, cancelled, "policy cancellation during shutdown")
	for i := 0; i < cap(results); i++ {
		select {
		case err := <-results:
			if err != nil && !errors.Is(err, ErrClosed) {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("publisher blocked during shutdown")
		}
	}
	awaitShutdown(t, shutdown)
	disconnected(t, reader)
	waitSignal(t, finished, "publisher connection cleanup")
	emptyHub(t, h)
	if len(reader.events) != 0 {
		t.Fatal("event escaped cancelled resource authorization")
	}
	if err := h.Publish(context.Background(), e, []string{testID(1)}, allow); !errors.Is(err, ErrClosed) {
		t.Fatalf("publication after shutdown: %v", err)
	}
}

func TestConcurrentConnectionClosures(t *testing.T) {
	for _, withShutdown := range []bool{false, true} {
		name := "client cleanup"
		if withShutdown {
			name = "client cleanup and server shutdown"
		}
		t.Run(name, func(t *testing.T) {
			h, _, server, finished := raceGateway(t, nil, nil)
			var clients []*websocket.Conn
			var readers []stream
			for i := 0; i < 4; i++ {
				c := dial(t, server, "student")
				clients = append(clients, c)
				readers = append(readers, readStream(c))
			}
			e := testEvent(t)
			publish(t, h, e, []string{testID(1)}, allow)
			for _, reader := range readers {
				receive(t, reader, e)
			}
			start, closed := make(chan struct{}), make(chan struct{}, 12)
			for _, c := range clients {
				for i := 0; i < 3; i++ {
					go func() {
						<-start
						_ = c.CloseNow()
						closed <- struct{}{}
					}()
				}
			}
			var shutdowns []<-chan error
			if withShutdown {
				for i := 0; i < 3; i++ {
					shutdowns = append(shutdowns, shutdownAsync(h))
				}
			}
			close(start)
			for i := 0; i < cap(closed); i++ {
				waitSignal(t, closed, "concurrent close")
			}
			for _, shutdown := range shutdowns {
				awaitShutdown(t, shutdown)
			}
			for _, reader := range readers {
				disconnected(t, reader)
				waitSignal(t, finished, "reader/watchdog and reservation cleanup")
			}
			emptyHub(t, h)
			if !withShutdown {
				// Cleanup releases the per-user limit without closing the hub.
				fresh := readStream(dial(t, server, "student"))
				publish(t, h, e, []string{testID(1)}, allow)
				receive(t, fresh, e)
				awaitShutdown(t, shutdownAsync(h))
				disconnected(t, fresh)
				waitSignal(t, finished, "replacement connection cleanup")
				emptyHub(t, h)
			}
		})
	}
}

func TestQueuedResourceAuthorizationUsesCurrentAccess(t *testing.T) {
	for _, token := range []string{"student", "coordA"} {
		t.Run(token, func(t *testing.T) {
			h, sessions, server, finished := raceGateway(t, nil, nil)
			reader := readStream(dial(t, server, token))
			p := sessions.users[token]
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			publish(t, h, testEvent(t), []string{p.ID}, func(ctx context.Context, _ domain.Principal, _ events.Event) (bool, error) {
				close(entered)
				select {
				case <-release:
					return false, nil
				case <-ctx.Done():
					return false, ctx.Err()
				}
			})
			waitSignal(t, entered, "blocked writer policy")
			var denied atomic.Bool
			var checks atomic.Int32
			protected := testEvent(t)
			publish(t, h, protected, []string{p.ID}, func(_ context.Context, current domain.Principal, event events.Event) (bool, error) {
				checks.Add(1)
				if current != p || event != protected {
					t.Error("resource policy received the wrong principal or event")
				}
				// The resource moves to another user/faculty while it is queued.
				owner, faculty := p.ID, p.CoordinatorFaculty
				if denied.Load() {
					owner, faculty = testID(2), testID(12)
				}
				return current.ID == owner || current.CanAccessFaculty(faculty), nil
			})
			denied.Store(true)
			unblock()
			// A later allowed event is a FIFO barrier: receiving it proves the
			// earlier denied event was processed, without a negative sleep window.
			sentinel := testEvent(t)
			publish(t, h, sentinel, []string{p.ID}, allow)
			receive(t, reader, sentinel)
			if checks.Load() != 1 {
				t.Fatalf("resource policy checks=%d, want 1", checks.Load())
			}
			awaitShutdown(t, shutdownAsync(h))
			disconnected(t, reader)
			waitSignal(t, finished, "resource policy connection cleanup")
			emptyHub(t, h)
		})
	}
}

func TestStaleConnectionCannotSubscribe(t *testing.T) {
	for _, token := range []string{"student", "coordA"} {
		t.Run(token, func(t *testing.T) {
			h, sessions, server, finished := raceGateway(t, nil, nil)
			conn := dial(t, server, token)
			reader := readStream(conn)
			p := sessions.users[token]
			ready := testEvent(t)
			publish(t, h, ready, []string{p.ID}, allow)
			receive(t, reader, ready)
			sessions.mu.Lock()
			delete(sessions.users, token)
			sessions.mu.Unlock()
			// This established socket has not had a periodic check yet. Client
			// input still cannot grant it another user's or faculty's access.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = conn.Write(ctx, websocket.MessageText, []byte(`{"subscribe":{"userId":"`+testID(2)+`","facultyId":"`+testID(12)+`"}}`))
			var policies atomic.Int32
			publish(t, h, testEvent(t), []string{p.ID, testID(2)}, func(context.Context, domain.Principal, events.Event) (bool, error) {
				policies.Add(1)
				return true, nil
			})
			disconnected(t, reader)
			waitSignal(t, finished, "stale subscriber cleanup")
			emptyHub(t, h)
			if len(reader.events) != 0 || policies.Load() != 0 {
				t.Fatal("stale subscriber reached resource policy or event delivery")
			}
		})
	}
}
