package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

func TestRealtimeProductionBootstrap(t *testing.T) {
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t)
	// This test uses the production clock, unlike the deterministic booking
	// fixture. Supply a moving calendar/window so it also works after 2027.
	now := time.Now().UTC().Truncate(time.Hour)
	f.exec(t, "DELETE FROM availability_slots") // fresh isolated synthetic fixture only
	f.exec(t, "INSERT INTO semesters(id,name,starts_at,ends_at) VALUES($1,'Bootstrap realtime term',$2,$3)", fixture(87002), now.Add(-24*time.Hour), now.Add(31*24*time.Hour))
	f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,$3,$4,'ONLINE')", fixture(bookingSlot), fixture(bookingTutor), now.Add(72*time.Hour), now.Add(120*time.Hour))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	for key, value := range map[string]string{
		"REALTIME_NAMESPACE": "bootstrap-test", "REALTIME_MONITOR_ADDR": "",
		"DATABASE_URL": os.Getenv("TUTTI_TEST_DATABASE_URL"), "REDIS_URL": os.Getenv("TUTTI_TEST_REDIS_URL"),
		"HTTP_ADDR": addr, "PUBLIC_ORIGIN": origin, "UNIVERSITY_EMAIL_DOMAINS": "uni.example", "OIDC_ENABLED": "false",
		"ARGON2_MEMORY_KIB": "19456", "ARGON2_ITERATIONS": "2", "ARGON2_THREADS": "1", "ARGON2_CONCURRENCY": "2",
	} {
		t.Setenv(key, value)
	}
	run, cancel := context.WithCancel(f.ctx)
	result := make(chan error, 1)
	go func() { result <- bootstrap.Run(run, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-result:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("API runtime did not shut down")
		}
	})
	client := &http.Client{Timeout: time.Second}
	base := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := client.Get(base + "/api/v1/auth/me")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 401 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("production API failed to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cookie := f.cookie[bookingStudent]
	socket, _, err := websocket.Dial(f.ctx, base+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.CloseNow()
	stream := realtimeRead(socket)
	input := f.payload(100*time.Hour, bookingOffer, bookingSlot)
	input["starts_at"] = now.Add(96 * time.Hour).Format(time.RFC3339)
	payload, _ := json.Marshal(input)
	request, _ := http.NewRequest("POST", base+"/api/v1/sessions", bytes.NewReader(payload))
	request.AddCookie(cookie)
	request.Header.Set("Origin", origin)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		body, _ := io.ReadAll(response.Body)
		t.Fatal(response.StatusCode, string(body))
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-stream.events:
			if e.Type == app.BookingChanged && e.EntityID == created.Data.ID {
				return
			}
		case <-timer.C:
			t.Fatal("automatic bootstrap dispatcher did not deliver committed booking")
		}
	}
}
