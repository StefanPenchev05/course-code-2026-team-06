package bootstrap

import (
	"net/http"
	"time"
)

// Only the exact WebSocket path bypasses TimeoutHandler, which does not expose
// http.Hijacker. All other paths, including unknown ones, keep REST timeouts.
func apiHandler(rest, socket http.Handler, timeout time.Duration) http.Handler {
	bounded := http.TimeoutHandler(rest, timeout, `{"error":"request timed out"}`)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ws" && r.URL.RawPath == "" {
			socket.ServeHTTP(w, r)
			return
		}
		bounded.ServeHTTP(w, r)
	})
}
