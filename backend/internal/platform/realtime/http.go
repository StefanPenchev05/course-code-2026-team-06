package realtime

import (
	"context"
	"errors"
	"net/http"

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

func NewHandler(hub *Hub, sessions Sessions, origin string) *Handler {
	return &Handler{hub: hub, sessions: sessions, origin: origin}
}

func reject(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"WebSocket connection refused"}`))
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		reject(w, 405)
		return
	}
	if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != h.origin {
		reject(w, 403)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery || len(r.Header.Values("Sec-WebSocket-Protocol")) != 0 {
		reject(w, 400)
		return
	}
	check, err := h.sessions.SessionCheck(r)
	if err != nil {
		reject(w, 401)
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
			reject(w, 401)
		case err == nil || errors.Is(err, domain.ErrForbidden):
			reject(w, 403)
		default:
			reject(w, 503)
		}
		return
	}
	c, err := h.hub.reserve(p, check)
	if err != nil {
		if errors.Is(err, ErrLimit) {
			reject(w, 429)
		} else {
			reject(w, 503)
		}
		return
	}
	defer h.hub.remove(c)
	socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{h.origin}, CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	h.hub.serve(c, socket)
}
