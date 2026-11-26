package realtime

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/coder/websocket"
)

// Sessions is implemented by Identity's HTTP adapter, using its cookie selection
// and application authentication service. No Redis or SQL access lives here.
type Sessions interface {
	SessionCheck(*http.Request) (func(context.Context) (domain.Principal, error), error)
}

type Handler struct {
	hub      *Hub
	sessions Sessions
	origin   string
}

// Keep the hijacked connection only to enforce a hard bound on close handshakes.
// Conn.CloseNow waits when the library's Close is already in progress.
type upgradeWriter struct {
	http.ResponseWriter
	connection net.Conn
}

func (w *upgradeWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.connection = conn
	}
	return conn, buffer, err
}

func NewHandler(hub *Hub, sessions Sessions, origin string) *Handler {
	return &Handler{hub: hub, sessions: sessions, origin: origin}
}

func reject(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"WebSocket connection refused"}`))
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	accepted := false
	defer func() {
		if !accepted {
			h.hub.metrics.Observe("handshake", time.Since(started))
		}
	}()
	refuse := func(status int, category string) {
		h.hub.metrics.Inc("connections_rejected")
		h.hub.metrics.Inc(category)
		reject(w, status)
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		refuse(405, "handshake_request")
		return
	}
	if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != h.origin {
		refuse(403, "handshake_origin")
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Sec-WebSocket-Protocol")) != 0 {
		refuse(400, "handshake_request")
		return
	}
	check, err := h.sessions.SessionCheck(r)
	if err != nil {
		refuse(401, "handshake_session")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.hub.config.AuthTimeout)
	p, err := check(ctx)
	if err == nil {
		err = ctx.Err()
	}
	cancel()
	if err != nil || !p.Active() || (!p.Student && !p.Coordinator()) {
		switch {
		case errors.Is(err, domain.ErrUnauthenticated):
			refuse(401, "handshake_session")
		case err == nil || errors.Is(err, domain.ErrForbidden):
			refuse(403, "handshake_session")
		default:
			refuse(503, "handshake_dependency")
		}
		return
	}
	c, err := h.hub.reserve(p, check)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			refuse(429, "connections_limit_rejected")
		} else {
			refuse(503, "handshake_dependency")
		}
		return
	}
	defer h.hub.remove(c)
	upgrade := &upgradeWriter{ResponseWriter: w}
	socket, err := websocket.Accept(upgrade, r, &websocket.AcceptOptions{
		OriginPatterns: []string{h.origin}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		h.hub.metrics.Inc("connections_rejected")
		h.hub.metrics.Inc("handshake_upgrade")
		return
	}
	accepted = true
	h.hub.metrics.Observe("handshake", time.Since(started))
	h.hub.metrics.Inc("connections_accepted")
	h.hub.metrics.Connection(1)
	defer func() { h.hub.metrics.Connection(-1); h.hub.metrics.Inc("connections_disconnected") }()
	h.hub.serve(c, socket, func() { _ = upgrade.connection.Close() })
}
