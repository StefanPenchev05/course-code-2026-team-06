package identityhttp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type oidcHTTPProvider struct{}

func (oidcHTTPProvider) AuthorizationURL(context.Context, string, string, string) (string, error) {
	return "https://idp.example/authorize?nonce=protocol-nonce", nil
}

type oidcHTTPStore struct {
	application.OIDCTransactions
	calls int
	err   error
	tx    domain.OIDCTransaction
}

func (s *oidcHTTPStore) Save(_ context.Context, _ string, tx domain.OIDCTransaction, _ time.Duration) error {
	s.calls++
	s.tx = tx
	return s.err
}

func TestOIDCLoginHTTP(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "local HTTP", true: "HTTPS"}[secure], func(t *testing.T) {
			origin := "http://localhost:8080"
			if secure {
				origin = "https://app.example"
			}
			logs := &bytes.Buffer{}
			h := New(nil, origin, secure, slog.New(slog.NewTextHandler(logs, nil)))
			store := &oidcHTTPStore{}
			login := application.NewOIDCLogin(oidcHTTPProvider{}, store, 5*time.Minute)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			h.RegisterOIDC(mux, login)
			r := httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil)
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 302 || w.Header().Get("Location") == "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("incorrect redirect response")
			}
			cookies := w.Result().Cookies()
			name := "tutti_oidc_tx"
			if secure {
				name = "__Host-" + name
			}
			if len(cookies) != 1 || cookies[0].Name != name || cookies[0].Domain != "" || cookies[0].Path != "/" || !cookies[0].HttpOnly || cookies[0].Secure != secure || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 300 {
				t.Fatal("unsafe browser cookie")
			}
			if domain.OIDCHash(cookies[0].Value) != store.tx.BindingHash {
				t.Fatal("browser cookie not bound to record")
			}
			if store.calls != 1 {
				t.Fatal("missing transaction")
			}
			for _, tc := range []struct {
				method, path, origin, site string
				status                     int
			}{
				{"GET", "/api/v1/auth/oidc/login?return_to=https://evil.example", "", "", 400},
				{"GET", "/api/v1/auth/oidc/login", "https://evil.example", "", 403},
				{"GET", "/api/v1/auth/oidc/login", "", "cross-site", 403},
				{"HEAD", "/api/v1/auth/oidc/login", "", "", 405},
				{"POST", "/api/v1/auth/oidc/login", "", "", 405},
				{"GET", "/api/v1/auth/oidc/callback", "", "", 404},
				{"GET", "/api/v1/auth/me", "", "", 401},
				{"GET", "/api/v1/auth/login", "", "", 405},
				{"POST", "/api/v1/auth/login", "", "", 403}, // Existing same-origin middleware.
			} {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				if tc.site != "" {
					req.Header.Set("Sec-Fetch-Site", tc.site)
				}
				out := httptest.NewRecorder()
				mux.ServeHTTP(out, req)
				if out.Code != tc.status || len(out.Result().Cookies()) != 0 {
					t.Fatalf("unexpected %s %s: %d", tc.method, tc.path, out.Code)
				}
			}
			if store.calls != 1 {
				t.Fatal("rejected requests created transactions")
			}
			store.err = errors.New("sensitive Redis diagnostics")
			out := httptest.NewRecorder()
			mux.ServeHTTP(out, r)
			if out.Code != 503 || out.Header().Get("Location") != "" || len(out.Result().Cookies()) != 0 || strings.Contains(out.Body.String(), "sensitive") {
				t.Fatal("unsafe failure")
			}
			for _, secret := range []string{cookies[0].Value, store.tx.Verifier, store.tx.Nonce, "protocol-nonce", "sensitive"} {
				if strings.Contains(logs.String(), secret) {
					t.Fatal("credential/nonce leaked to logs")
				}
			}
		})
	}
}
