package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	jose "github.com/go-jose/go-jose/v4"
)

type signingProvider struct {
	mu                               sync.Mutex
	server                           *httptest.Server
	key, other                       *rsa.PrivateKey
	kid, mode, algorithm, headerType string
	claims                           map[string]any
	calls, keys                      int
	nonce, pkce                      string
}

func newSigningProvider(t *testing.T) *signingProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	m := &signingProvider{key: key, other: other, kid: "first", algorithm: "RS256", headerType: "JWT", nonce: "expected-nonce", pkce: "expected-verifier"}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(m.server.Close)
	m.claims = map[string]any{"iss": m.server.URL, "sub": "synthetic-subject", "aud": "tutti-backend", "azp": "tutti-backend", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": m.nonce, "typ": "ID"}
	return m
}
func (m *signingProvider) config() application.OIDCConfig {
	return application.OIDCConfig{Issuer: m.server.URL, ClientID: "tutti-backend", ClientSecret: "synthetic-secret", PublicOrigin: "http://localhost:8080", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", TransactionTTL: 5 * time.Minute}
}
func (m *signingProvider) serve(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]any{"issuer": m.server.URL, "authorization_endpoint": m.server.URL + "/authorize", "token_endpoint": m.server.URL + "/token", "jwks_uri": m.server.URL + "/keys", "code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
	case "/keys":
		m.keys++
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &m.key.PublicKey, KeyID: m.kid, Algorithm: "RS256", Use: "sig"}}})
	case "/token":
		m.calls++
		if m.mode == "timeout" {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
			return
		}
		if m.mode == "outage" {
			w.WriteHeader(503)
			json.NewEncoder(w).Encode(map[string]string{"error": "private-provider-detail"})
			return
		}
		r.ParseForm()
		client, secret, ok := r.BasicAuth()
		if !ok || client != "tutti-backend" || secret != "synthetic-secret" {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "synthetic-code" || r.Form.Get("code_verifier") != m.pkce || r.Form.Get("redirect_uri") != m.config().RedirectURL {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		key := m.key
		if m.mode == "bad-signature" {
			key = m.other
		}
		options := (&jose.SignerOptions{}).WithType(jose.ContentType(m.headerType)).WithHeader(jose.HeaderKey("kid"), m.kid)
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.SignatureAlgorithm(m.algorithm), Key: key}, options)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		payload, _ := json.Marshal(m.claims)
		signed, _ := signer.Sign(payload)
		raw, _ := signed.CompactSerialize()
		response := map[string]any{"access_token": "private-access-token", "token_type": "Bearer", "expires_in": 300, "id_token": raw}
		if m.mode == "missing-id" {
			delete(response, "id_token")
		}
		if m.mode == "missing-access" {
			delete(response, "access_token")
		}
		if m.mode == "missing-type" {
			delete(response, "token_type")
		}
		if m.mode == "wrong-type" {
			response["token_type"] = "MAC"
		}
		if m.mode == "malformed-id" {
			response["id_token"] = "not-a-token"
		}
		json.NewEncoder(w).Encode(response)
	default:
		w.WriteHeader(404)
	}
}

func TestExchangeAndIDTokenVerification(t *testing.T) {
	m := newSigningProvider(t)
	p, err := New(context.Background(), m.config())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := p.ExchangeAndVerify(context.Background(), "synthetic-code", m.pkce, m.nonce)
	if err != nil || identity.Issuer != m.server.URL || identity.Subject != "synthetic-subject" {
		t.Fatal("valid identity not verified", err)
	}
	// Unknown key ID must refresh JWKS without bypassing signature verification.
	m.mu.Lock()
	m.key = m.other
	m.kid = "rotated"
	before := m.keys
	m.mu.Unlock()
	if _, err := p.ExchangeAndVerify(context.Background(), "synthetic-code", m.pkce, m.nonce); err != nil {
		t.Fatal("key rotation failed", err)
	}
	m.mu.Lock()
	if m.keys <= before {
		t.Error("JWKS not refreshed")
	}
	m.mu.Unlock()
}

func TestRejectIDTokenAndExchangeFailures(t *testing.T) {
	m := newSigningProvider(t)
	original := map[string]any{}
	for k, v := range m.claims {
		original[k] = v
	}
	for _, tc := range []struct {
		name                  string
		mutate                func(*signingProvider)
		code, verifier, nonce string
	}{
		{name: "missing ID token", mutate: func(m *signingProvider) { m.mode = "missing-id" }},
		{name: "malformed ID token", mutate: func(m *signingProvider) { m.mode = "malformed-id" }},
		{name: "missing access token", mutate: func(m *signingProvider) { m.mode = "missing-access" }},
		{name: "missing token type", mutate: func(m *signingProvider) { m.mode = "missing-type" }},
		{name: "wrong token type", mutate: func(m *signingProvider) { m.mode = "wrong-type" }},
		{name: "bad signature", mutate: func(m *signingProvider) { m.mode = "bad-signature" }},
		{name: "wrong issuer", mutate: func(m *signingProvider) { m.claims["iss"] = "https://other.example" }},
		{name: "wrong audience", mutate: func(m *signingProvider) { m.claims["aud"] = "other-client" }},
		{name: "wrong azp", mutate: func(m *signingProvider) { m.claims["azp"] = "other-client" }},
		{name: "empty azp", mutate: func(m *signingProvider) { m.claims["azp"] = "" }},
		{name: "null azp", mutate: func(m *signingProvider) { m.claims["azp"] = nil }},
		{name: "multiple audiences missing azp", mutate: func(m *signingProvider) {
			m.claims["aud"] = []string{"tutti-backend", "other"}
			delete(m.claims, "azp")
		}},
		{name: "multiple audiences wrong azp", mutate: func(m *signingProvider) {
			m.claims["aud"] = []string{"tutti-backend", "other"}
			m.claims["azp"] = "other"
		}},
		{name: "unapproved extra audience", mutate: func(m *signingProvider) { m.claims["aud"] = []string{"tutti-backend", "other"} }},
		{name: "expired", mutate: func(m *signingProvider) { m.claims["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{name: "missing expiry", mutate: func(m *signingProvider) { delete(m.claims, "exp") }},
		{name: "future iat", mutate: func(m *signingProvider) { m.claims["iat"] = time.Now().Add(time.Hour).Unix() }},
		{name: "missing iat", mutate: func(m *signingProvider) { delete(m.claims, "iat") }},
		{name: "future nbf", mutate: func(m *signingProvider) { m.claims["nbf"] = time.Now().Add(2 * time.Minute).Unix() }},
		{name: "missing nonce", mutate: func(m *signingProvider) { delete(m.claims, "nonce") }},
		{name: "wrong nonce", mutate: func(m *signingProvider) { m.claims["nonce"] = "other-nonce" }},
		{name: "empty subject", mutate: func(m *signingProvider) { m.claims["sub"] = "" }},
		{name: "access token payload type", mutate: func(m *signingProvider) { m.claims["typ"] = "Bearer" }},
		{name: "access JWT header type", mutate: func(m *signingProvider) { m.headerType = "at+jwt" }},
		{name: "unexpected algorithm", mutate: func(m *signingProvider) { m.algorithm = "RS512" }},
		{name: "bad PKCE", verifier: "wrong-verifier"},
		{name: "invalid code", code: "wrong-code"},
		{name: "outage", mutate: func(m *signingProvider) { m.mode = "outage" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.mu.Lock()
			m.mode = ""
			m.algorithm = "RS256"
			m.headerType = "JWT"
			m.claims = map[string]any{}
			for k, v := range original {
				m.claims[k] = v
			}
			if tc.mutate != nil {
				tc.mutate(m)
			}
			before := m.calls
			m.mu.Unlock()
			p, err := New(context.Background(), m.config())
			if err != nil {
				t.Fatal(err)
			}
			code, verifier, nonce := "synthetic-code", m.pkce, m.nonce
			if tc.code != "" {
				code = tc.code
			}
			if tc.verifier != "" {
				verifier = tc.verifier
			}
			if tc.nonce != "" {
				nonce = tc.nonce
			}
			identity, err := p.ExchangeAndVerify(context.Background(), code, verifier, nonce)
			if err == nil || identity.Subject != "" {
				t.Fatal("invalid identity accepted")
			}
			m.mu.Lock()
			if m.calls != before+1 {
				t.Error("token exchange retried")
			}
			m.mu.Unlock()
		})
	}
	// Wrong confidential secret, bounded request timeout and cancellation.
	m.mu.Lock()
	m.mode = ""
	m.mu.Unlock()
	cfg := m.config()
	cfg.ClientSecret = "wrong-secret"
	p, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExchangeAndVerify(context.Background(), "synthetic-code", m.pkce, m.nonce); err == nil {
		t.Fatal("wrong client secret accepted")
	}
	p, err = New(context.Background(), m.config())
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.mode = "timeout"
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := p.ExchangeAndVerify(ctx, "synthetic-code", m.pkce, m.nonce); err == nil {
		t.Fatal("timeout accepted")
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if _, err := p.ExchangeAndVerify(cancelled, "synthetic-code", m.pkce, m.nonce); err == nil {
		t.Fatal("cancelled request accepted")
	}
}
