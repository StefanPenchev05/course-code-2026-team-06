package identityhttp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"golang.org/x/oauth2"
)

type callbackStore struct {
	application.OIDCTransactions
	mu             sync.Mutex
	state, binding string
	tx             domain.OIDCTransaction
	spent          bool
	err            error
}

func (s *callbackStore) Consume(_ context.Context, state, binding string) (domain.OIDCTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.OIDCTransaction{}, s.err
	}
	if s.spent || state != s.state || binding != s.binding || !time.Now().Before(s.tx.ExpiresAt) {
		return domain.OIDCTransaction{}, domain.ErrOIDCTransaction
	}
	s.spent = true
	return s.tx, nil
}

type callbackExchange struct {
	calls atomic.Int32
	err   error
}

func (p *callbackExchange) ExchangeAndVerify(_ context.Context, code, verifier, nonce string) (domain.VerifiedIdentity, error) {
	p.calls.Add(1)
	return domain.VerifiedIdentity{Issuer: "https://idp.example", Subject: "private-subject"}, p.err
}

// Test double for HTTP cookie/error mapping around the already-tested protocol stage.
// Real identity resolution and Redis sessions are exercised by integration tests.
type protocolAuthentication struct{ callback *application.OIDCCallback }

func (p protocolAuthentication) Complete(ctx context.Context, r application.OIDCResponse, binding, previous string) (application.OIDCAuthenticationResult, error) {
	result, err := p.callback.Complete(ctx, r, binding)
	out := application.OIDCAuthenticationResult{Consumed: result.Consumed}
	if err == nil {
		out.Token = oauth2.GenerateVerifier()
		out.TTL = time.Hour
	}
	return out, err
}
func allowOIDCTest(context.Context, string) (bool, error) { return true, nil }

func TestOIDCCallbackHTTP(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "loopback", true: "HTTPS"}[secure], func(t *testing.T) {
			state, binding := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
			store := &callbackStore{state: state, binding: binding, tx: domain.OIDCTransaction{Verifier: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), ExpiresAt: time.Now().Add(time.Minute)}}
			provider := &callbackExchange{}
			var logs bytes.Buffer
			h := New(nil, "http://localhost:8080", secure, slog.New(slog.NewTextHandler(&logs, nil)))
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			h.RegisterOIDCCallback(mux, protocolAuthentication{application.NewOIDCCallback(provider, store, "https://idp.example")}, "/")
			normal := "state=" + state + "&code=private-authorization-code"
			request := func(method, query, browser string, duplicate bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest(method, "/api/v1/auth/oidc/callback?"+query, nil)
				// This is an IdP navigation, with no Origin header and cross-site fetch metadata.
				r.Header.Set("Sec-Fetch-Site", "cross-site")
				if browser != "" {
					r.AddCookie(&http.Cookie{Name: h.oidcCookieName(), Value: browser})
				}
				if duplicate {
					r.AddCookie(&http.Cookie{Name: h.oidcCookieName(), Value: browser})
				}
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			for _, tc := range []struct {
				name, method, query, browser string
				duplicate                    bool
			}{
				{name: "missing state", query: "code=private-authorization-code", browser: binding},
				{name: "malformed state", query: "state=bad&code=secret", browser: binding},
				{name: "missing cookie", query: normal},
				{name: "wrong browser", query: normal, browser: oauth2.GenerateVerifier()},
				{name: "duplicate cookies", query: normal, browser: binding, duplicate: true},
				{name: "duplicate state", query: normal + "&state=" + state, browser: binding},
				{name: "duplicate code", query: normal + "&code=other", browser: binding},
				{name: "missing code", query: "state=" + state, browser: binding},
				{name: "empty code", query: "state=" + state + "&code=", browser: binding},
				{name: "error and code", query: normal + "&error=access_denied", browser: binding},
				{name: "bad encoding", query: normal + "%zz", browser: binding},
				{name: "control code", query: "state=" + state + "&code=%0a", browser: binding},
				{name: "oversize code", query: "state=" + state + "&code=" + strings.Repeat("x", 4097), browser: binding},
				{name: "oversize query", query: normal + "&error_description=" + strings.Repeat("x", 8192), browser: binding},
				{name: "empty response issuer", query: normal + "&iss=", browser: binding},
				{name: "wrong response issuer", query: normal + "&iss=https%3A%2F%2Fevil.example", browser: binding},
				{name: "unknown parameter", query: normal + "&return_to=https://evil.example", browser: binding},
				{name: "HEAD", method: "HEAD", query: normal, browser: binding},
			} {
				t.Run(tc.name, func(t *testing.T) {
					method := tc.method
					if method == "" {
						method = "GET"
					}
					w := request(method, tc.query, tc.browser, tc.duplicate)
					want := 400
					if method == "HEAD" {
						want = 405
					}
					if w.Code != want || len(w.Result().Cookies()) != 0 || w.Header().Get("Location") != "" {
						t.Fatalf("invalid callback accepted or affected cookie: %d", w.Code)
					}
					if provider.calls.Load() != 0 || store.spent {
						t.Fatal("malformed or wrong-browser response consumed valid state")
					}
				})
			}
			w := request("GET", normal, binding, false)
			if w.Code != 303 || !store.spent || provider.calls.Load() != 1 {
				t.Fatal("valid callback rejected", w.Code)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 2 || cookies[0].Name != h.oidcCookieName() || cookies[0].MaxAge != -1 || cookies[0].Value != "" || cookies[0].Secure != secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" {
				t.Fatal("temporary cookie not cleared safely")
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("unsafe cache/referrer headers")
			}
			for _, private := range []string{state, binding, store.tx.Verifier, store.tx.Nonce, "private-authorization-code", "private-subject", "https://idp.example"} {
				if strings.Contains(w.Body.String()+logs.String(), private) {
					t.Fatal("private callback data leaked")
				}
			}
			if request("GET", normal, binding, false).Code != 400 || provider.calls.Load() != 1 {
				t.Fatal("callback replay succeeded")
			}
			me := httptest.NewRecorder()
			mux.ServeHTTP(me, httptest.NewRequest("GET", "/api/v1/auth/me", nil))
			if me.Code != 401 {
				t.Fatal("missing application cookie authenticated a request")
			}
		})
	}
}

func TestOIDCCallbackDenialsAndConcurrency(t *testing.T) {
	for _, scenario := range []string{"denied", "Redis failure", "token failure", "provider unavailable", "expired", "concurrent"} {
		t.Run(scenario, func(t *testing.T) {
			state, binding := oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
			store := &callbackStore{state: state, binding: binding, tx: domain.OIDCTransaction{Verifier: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), ExpiresAt: time.Now().Add(time.Minute)}}
			provider := &callbackExchange{}
			if scenario == "Redis failure" {
				store.err = errors.New("private Redis password detail")
			}
			if scenario == "token failure" {
				provider.err = domain.ErrOIDCAuthentication
			}
			if scenario == "provider unavailable" {
				provider.err = domain.ErrOIDCUnavailable
			}
			if scenario == "expired" {
				store.tx.ExpiresAt = time.Now().Add(-time.Second)
			}
			var logs bytes.Buffer
			h := New(nil, "http://localhost:8080", false, slog.New(slog.NewTextHandler(&logs, nil)))
			mux := http.NewServeMux()
			h.RegisterOIDCCallback(mux, protocolAuthentication{application.NewOIDCCallback(provider, store, "https://idp.example")}, "/")
			query := url.Values{"state": {state}, "code": {"private-code"}}
			if scenario == "denied" {
				delete(query, "code")
				query.Set("error", "access_denied")
				query.Set("error_description", "private-provider-detail")
			}
			invoke := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?"+query.Encode(), nil)
				r.AddCookie(&http.Cookie{Name: "tutti_oidc_tx", Value: binding})
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			if scenario == "concurrent" {
				done := make(chan int, 8)
				for range 8 {
					go func() { done <- invoke().Code }()
				}
				success := 0
				for range 8 {
					code := <-done
					if code == 303 {
						success++
					} else if code != 400 {
						t.Fatal(code)
					}
				}
				if success != 1 || provider.calls.Load() != 1 {
					t.Fatal("duplicate callback exchanged more than once")
				}
				return
			}
			w := invoke()
			want := 401
			if scenario == "Redis failure" || scenario == "provider unavailable" {
				want = 503
			}
			if scenario == "expired" {
				want = 400
			}
			if w.Code != want || strings.Contains(w.Body.String()+logs.String(), "private-") {
				t.Fatal("unsafe denial", w.Code)
			}
			shouldClear := scenario != "Redis failure" && scenario != "expired"
			if (len(w.Result().Cookies()) == 1) != shouldClear {
				t.Fatal("incorrect cookie cleanup")
			}
			if scenario == "denied" && provider.calls.Load() != 0 {
				t.Fatal("exchanged provider-denied callback")
			}
		})
	}
}
