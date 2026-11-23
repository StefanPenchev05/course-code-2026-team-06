package identityhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

func (h *Handler) oidcCookieName() string {
	if h.secure {
		return "__Host-tutti_oidc_tx"
	}
	return "tutti_oidc_tx"
}

func callbackResponse(r *http.Request) (application.OIDCResponse, error) {
	var response application.OIDCResponse
	if len(r.URL.RawQuery) > 8192 {
		return response, domain.ErrOIDCTransaction
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return response, domain.ErrOIDCTransaction
	}
	for key, entries := range values {
		if len(entries) != 1 || len(entries[0]) > 4096 || !utf8.ValidString(entries[0]) || strings.IndexFunc(entries[0], func(ch rune) bool { return ch < 0x20 || ch == 0x7f }) >= 0 {
			return response, domain.ErrOIDCTransaction
		}
		switch key {
		case "state", "code", "error", "error_description", "error_uri", "session_state", "iss":
		default:
			return response, domain.ErrOIDCTransaction
		}
	}
	if issuer, exists := values["iss"]; exists && issuer[0] == "" {
		return response, domain.ErrOIDCTransaction
	}
	response = application.OIDCResponse{State: values.Get("state"), Code: values.Get("code"), Error: values.Get("error"), Issuer: values.Get("iss")}
	_, hasCode := values["code"]
	_, hasError := values["error"]
	if !domain.ValidOIDCToken(response.State) || hasCode == hasError || (hasCode && strings.TrimSpace(response.Code) == "") || (hasError && (response.Error == "" || len(response.Error) > 128)) {
		return response, domain.ErrOIDCTransaction
	}
	return response, nil
}

func (h *Handler) RegisterOIDCCallback(mux *http.ServeMux, callback *application.OIDCCallback) {
	mux.HandleFunc("GET /api/v1/auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		response, err := callbackResponse(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid authentication response"})
			return
		}
		cookies := []*http.Cookie{}
		for _, cookie := range r.Cookies() {
			if cookie.Name == h.oidcCookieName() {
				cookies = append(cookies, cookie)
			}
		}
		if len(cookies) != 1 || !domain.ValidOIDCToken(cookies[0].Value) {
			writeJSON(w, 400, map[string]string{"error": "invalid authentication response"})
			return
		}
		result, err := callback.Complete(r.Context(), response, cookies[0].Value)
		if result.Consumed {
			http.SetCookie(w, &http.Cookie{Name: h.oidcCookieName(), Value: "", Path: "/", Secure: h.secure, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
		}
		if r.Context().Err() == context.Canceled {
			return
		}
		if err != nil {
			status, message := 401, "OIDC authentication failed"
			if errors.Is(err, domain.ErrOIDCTransaction) {
				status, message = 400, "invalid authentication response"
			}
			if errors.Is(err, domain.ErrOIDCUnavailable) {
				status, message = 503, "OIDC authentication unavailable"
			}
			// Never log provider bodies/errors, callback URLs, code, state or identity.
			writeJSON(w, status, map[string]string{"error": message})
			return
		}
		// Stage 3 will bind result.Identity to an authorized Tutti account. Do not
		// serialize the subject, issue cookies or make /auth/me authenticated here.
		writeJSON(w, 200, map[string]string{"status": "oidc_identity_verified", "message": "OIDC identity verified; Tutti account linking and sessions are not implemented."})
	})
}
