package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	oidcprovider "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/oidc"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/security"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

func TestOIDCVerifiedCallback(t *testing.T) {
	isolatedRealtimeDatabase(t)
	f := newBookingFixture(t)
	address := os.Getenv("TUTTI_TEST_REDIS_URL")
	if address == "" {
		t.Skip("use test_identity.sh for disposable Redis")
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge string
	var exchanges atomic.Int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "mock-key", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			exchanges.Add(1)
			r.ParseForm()
			id, secret, ok := r.BasicAuth()
			if !ok || id != "tutti-backend" || secret != "synthetic-client-secret" || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != challenge || r.Form.Get("code") != "synthetic-code" || r.Form.Get("redirect_uri") != "http://localhost:8080/api/v1/auth/oidc/callback" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "mock-key"))
			claims, _ := json.Marshal(map[string]any{"iss": issuer, "sub": "synthetic-private-subject", "aud": "tutti-backend", "azp": "tutti-backend", "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "nonce": nonce, "typ": "ID"})
			signed, _ := signer.Sign(claims)
			token, _ := signed.CompactSerialize()
			json.NewEncoder(w).Encode(map[string]any{"access_token": "private-access-token", "token_type": "Bearer", "id_token": token, "expires_in": 60})
		default:
			w.WriteHeader(404)
		}
	}))
	defer mock.Close()
	issuer = mock.URL
	config := application.OIDCConfig{Issuer: issuer, ClientID: "tutti-backend", ClientSecret: "synthetic-client-secret", PublicOrigin: "http://localhost:8080", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", TransactionTTL: 5 * time.Minute}
	provider, err := oidcprovider.New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	store := sessionredis.NewOIDCTransactions(client)
	repo := identitypg.New(f.db)
	passwords, _ := security.NewPasswords(security.Cost{Memory: 19 * 1024, Time: 2, Threads: 1}, 2)
	service, err := application.New(ctx, repo, sessionredis.New(client), passwords, application.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := application.NewOIDCIdentityResolver(repo, issuer)
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := application.NewTrustedOIDCProvisioner(repo, issuer)
	if err != nil {
		t.Fatal(err)
	}
	if err := provisioner.LinkOIDCIdentity(ctx, fixture(bookingTutor), domain.VerifiedIdentity{Issuer: issuer, Subject: "synthetic-private-subject"}); err != nil {
		t.Fatal(err)
	}
	h := identityhttp.New(service, config.PublicOrigin, true, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	h.RegisterOIDC(mux, application.NewOIDCLogin(provider, store, config.TransactionTTL), sessionredis.NewOIDCLimiter(client).Allow)
	h.RegisterOIDCCallback(mux, application.NewOIDCAuthentication(application.NewOIDCCallback(provider, store, config.Issuer), resolver, service), "/sessions")
	malformed := httptest.NewRecorder()
	mux.ServeHTTP(malformed, httptest.NewRequest("GET", "/api/v1/auth/oidc/callback", nil))
	if malformed.Code != 400 {
		t.Fatal("malformed callback accepted")
	}
	start := httptest.NewRecorder()
	mux.ServeHTTP(start, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	if start.Code != 302 {
		t.Fatal(start.Code)
	}
	target, _ := url.Parse(start.Header().Get("Location"))
	state := target.Query().Get("state")
	nonce = target.Query().Get("nonce")
	challenge = target.Query().Get("code_challenge")
	cookie := start.Result().Cookies()[0]
	var retained domain.OIDCTransaction
	raw, err := client.Get(ctx, "tutti:identity:oidc:"+domain.OIDCHash(state)).Bytes()
	if err != nil || json.Unmarshal(raw, &retained) != nil {
		t.Fatal("missing server-side transaction fixture")
	}

	query := url.Values{"state": {state}, "code": {"synthetic-code"}, "iss": {issuer}}
	request := func(binding string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?"+query.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: cookie.Name, Value: binding})
		r.AddCookie(f.cookie[bookingTutor])
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if request(oauth2.GenerateVerifier()).Code != 400 || exchanges.Load() != 0 {
		t.Fatal("wrong browser consumed transaction")
	}
	// Concurrent callbacks must have one cryptographically verified success and one exchange.
	done := make(chan *httptest.ResponseRecorder, 6)
	for range 6 {
		go func() { done <- request(cookie.Value) }()
	}
	success := 0
	var appCookie *http.Cookie
	for range 6 {
		out := <-done
		if out.Code == 303 {
			success++
			if len(out.Result().Cookies()) != 2 || out.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("temporary cookie not cleared")
			}
			if out.Header().Get("Location") != "/sessions" {
				t.Fatal("untrusted destination")
			}
			appCookie = out.Result().Cookies()[1]
			if appCookie.Name != "__Host-tutti_session" || !appCookie.Secure || !appCookie.HttpOnly || appCookie.SameSite != http.SameSiteLaxMode || appCookie.Value == f.cookie[bookingTutor].Value || appCookie.MaxAge != 3600 {
				t.Fatal("unsafe/fixed application cookie")
			}
			for _, private := range []string{nonce, "synthetic-private-subject", "private-access-token", "synthetic-client-secret", "synthetic-code"} {
				if strings.Contains(out.Body.String(), private) {
					t.Fatal("credential leakage")
				}
			}
		} else if out.Code != 400 {
			t.Fatal("unexpected concurrent callback result", out.Code)
		}
	}
	if success != 1 || exchanges.Load() != 1 {
		t.Fatal("callback not single-use", success, exchanges.Load())
	}
	if request(cookie.Value).Code != 400 {
		t.Fatal("callback replay")
	}
	me := func(cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	current := me(appCookie)
	if current.Code != 200 || fields(t, current)["id"] != fixture(bookingTutor) {
		t.Fatal("OIDC session not authenticated", current.Code)
	}
	if me(f.cookie[bookingTutor]).Code != 401 {
		t.Fatal("old session not revoked")
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(appCookie)
	r.Header.Set("Origin", config.PublicOrigin)
	loggedOut := httptest.NewRecorder()
	mux.ServeHTTP(loggedOut, r)
	if loggedOut.Code != 204 || me(appCookie).Code != 401 {
		t.Fatal("OIDC logout did not revoke session")
	}
	failedAttempt := func(t *testing.T, want int) {
		t.Helper()
		before := len(client.Keys(ctx, "tutti:identity:session:*").Val())
		state := oauth2.GenerateVerifier()
		query.Set("state", state)
		if err := store.Save(ctx, state, retained, time.Minute); err != nil {
			t.Fatal(err)
		}
		out := request(cookie.Value)
		if out.Code != want || len(out.Result().Cookies()) != 1 || out.Result().Cookies()[0].MaxAge != -1 || out.Header().Get("Location") != "" {
			t.Fatal("failure issued session or retained consumed cookie", out.Code)
		}
		if len(client.Keys(ctx, "tutti:identity:session:*").Val()) != before {
			t.Fatal("failed callback persisted a session")
		}
		if strings.TrimSpace(out.Body.String()) != `{"error":"OIDC authentication unavailable"}` {
			t.Fatal("unsafe callback error")
		}
	}
	t.Run("PostgreSQL resolution failure", func(t *testing.T) {
		f.exec(t, "ALTER TABLE oidc_identities RENAME TO unavailable_oidc_identities")
		defer f.exec(t, "ALTER TABLE unavailable_oidc_identities RENAME TO oidc_identities")
		failedAttempt(t, 503)
	})
	t.Run("Redis rotation failure after valid resolution", func(t *testing.T) {
		failedCache := redis.NewClient(options)
		failedCache.Close()
		failedService, err := application.New(ctx, repo, sessionredis.New(failedCache), passwords, application.Config{Domains: []string{"uni.example"}, SessionTTL: time.Hour, ActivationTTL: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		failedHandler := identityhttp.New(failedService, config.PublicOrigin, true, slog.Default())
		mux = http.NewServeMux()
		failedHandler.RegisterOIDCCallback(mux, application.NewOIDCAuthentication(application.NewOIDCCallback(provider, store, issuer), resolver, failedService), "/sessions")
		failedAttempt(t, 503)
	})

}
