package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
