package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/realtime"
	events "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

func TestRESTTimeoutAndPersistentUpgrade(t *testing.T) {
	const limit = 30 * time.Millisecond
	rest := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fast" {
			w.WriteHeader(204)
			return
		}
		<-r.Context().Done()
	})
	socket := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, wire, err := c.Read(ctx)
		if err != nil {
			return
		}
		_ = c.Write(ctx, websocket.MessageText, wire)
	})
	server := httptest.NewUnstartedServer(apiHandler(rest, socket, limit))
	// Exercise inherited HTTP IO deadlines as well as TimeoutHandler itself.
	server.Config.ReadTimeout = 50 * time.Millisecond
	server.Config.WriteTimeout = 80 * time.Millisecond
	server.Start()
	defer server.Close()
	for path, want := range map[string]int{"/fast": 204, "/slow": 503, "/ws/": 503, "/%77s": 503} {
		r, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != want {
			t.Fatalf("%s=%d, want %d", path, r.StatusCode, want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, server.URL+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	time.Sleep(150 * time.Millisecond)
	if err := c.Write(ctx, websocket.MessageText, []byte("still alive")); err != nil {
		t.Fatal(err)
	}
	_, wire, err := c.Read(ctx)
	if err != nil || string(wire) != "still alive" {
		t.Fatalf("socket inherited REST timeout: %s %v", wire, err)
	}
}

type realtimeTestSessions struct{}

func (realtimeTestSessions) SessionCheck(*http.Request) (func(context.Context) (domain.Principal, error), error) {
	return func(context.Context) (domain.Principal, error) {
		return domain.Principal{ID: "00000000-0000-0000-0000-000000000001", Student: true, Verification: domain.Approved}, nil
	}, nil
}

func TestSocketSurvivesProductionRESTDeadline(t *testing.T) {
	hub, err := realtime.NewHub(realtime.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://tutti.example"
	server := httptest.NewUnstartedServer(apiHandler(http.NotFoundHandler(), realtime.NewHandler(hub, realtimeTestSessions{}, origin), 10*time.Second))
	server.Config.ReadTimeout = 10 * time.Second
	server.Config.WriteTimeout = 15 * time.Second
	server.Start()
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := hub.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, server.URL+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	time.Sleep(11 * time.Second)
	e, _ := events.NewEvent(events.AccountAccessChanged, "", time.Now())
	if err := hub.Publish(ctx, e, []string{"00000000-0000-0000-0000-000000000001"}, func(context.Context, domain.Principal, events.Event) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	_, wire, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := events.Decode(wire)
	if err != nil || got != e {
		t.Fatal("socket failed after the real ten-second deadline", err)
	}
}
