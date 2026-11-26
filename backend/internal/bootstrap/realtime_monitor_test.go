package bootstrap

import (
	"net/http/httptest"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
)

func TestMonitorConfigurationAndReadiness(t *testing.T) {
	t.Setenv("REALTIME_NAMESPACE", "test")
	for _, address := range []string{"0.0.0.0:9091", "[::]:9091", "localhost:9091", "127.0.0.1:0", "127.0.0.1:65536", "example.com:9091"} {
		t.Setenv("REALTIME_MONITOR_ADDR", address)
		if _, err := realtimeSettings(); err == nil {
			t.Fatal("accepted public or invalid listener", address)
		}
	}
	for _, address := range []string{"", "127.0.0.1:9091", "[::1]:9091"} {
		t.Setenv("REALTIME_MONITOR_ADDR", address)
		if _, err := realtimeSettings(); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("REALTIME_NAMESPACE", "")
	if _, err := realtimeSettings(); err == nil {
		t.Fatal("missing deployment namespace accepted")
	}
	m := observability.New()
	h := monitorHandler(m)
	check := func(path string, want int) {
		t.Helper()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != want {
			t.Fatal(path, r.Code)
		}
	}
	check("/livez", 200)
	check("/readyz", 503)
	check("/metrics", 200)
	m.Health(true, true, true, observability.Outbox{})
	m.Subscribed(true)
	check("/readyz", 200)
	m.Subscribed(false)
	check("/readyz", 503)
	check("/livez", 200)
	m.Subscribed(true)
	m.Health(true, false, true, observability.Outbox{})
	check("/readyz", 503)
}
