package identityhttp

import (
	"net/http"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
)

// RegisterOIDC adds only public login initiation. Legacy routes stay unchanged.
func (h *Handler) RegisterOIDC(mux *http.ServeMux, login *application.OIDCLogin) {
	mux.HandleFunc("GET /api/v1/auth/oidc/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Navigation GETs need not carry Origin; reject explicit foreign origins and
		// cross-site browser initiation. Never accept return URLs from the caller.
		origins := r.Header.Values("Origin")
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" || len(origins) > 1 || (len(origins) == 1 && origins[0] != h.origin) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if r.URL.RawQuery != "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "OIDC login does not accept query parameters"})
			return
		}
		result, err := login.Begin(r.Context())
		if err != nil {
			h.logger.ErrorContext(r.Context(), "OIDC initiation unavailable", "route", r.Pattern)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "OIDC authentication unavailable"})
			return
		}
		http.SetCookie(w, &http.Cookie{Name: h.oidcCookieName(), Value: result.BrowserBinding, Path: "/", Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(result.TTL.Seconds())})
		http.Redirect(w, r, result.URL, http.StatusFound)
	})
}
