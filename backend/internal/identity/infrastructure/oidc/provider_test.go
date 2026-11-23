package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
)

func TestDiscoveryAndAuthorization(t *testing.T) {
	var issuer string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Error("unexpected request", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"}})
	}))
	defer server.Close()
	issuer = server.URL
	cfg := application.OIDCConfig{Issuer: issuer, ClientID: "tutti-backend", ClientSecret: "synthetic-client-secret", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", PublicOrigin: "http://localhost:8080", TransactionTTL: 5 * time.Minute}
	p, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	verifier := "cryptographically-random-in-real-service-verifier"
	target, err := p.AuthorizationURL(context.Background(), "the-state", verifier, "the-nonce")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(target)
	q := u.Query()
	hash := sha256.Sum256([]byte(verifier))
	want := map[string]string{"response_type": "code", "client_id": "tutti-backend", "redirect_uri": cfg.RedirectURL, "scope": "openid profile email", "state": "the-state", "nonce": "the-nonce", "code_challenge_method": "S256", "code_challenge": base64.RawURLEncoding.EncodeToString(hash[:])}
	for key, value := range want {
		if q.Get(key) != value || len(q[key]) != 1 {
			t.Fatalf("incorrect %s", key)
		}
	}
	if len(q) != len(want) || q.Get("code_verifier") != "" || q.Get("client_secret") != "" {
		t.Fatal("unexpected authorization parameters")
	}
	issuer = server.URL + "/wrong" // Exact issuer validation must not be bypassed.
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatal("accepted mismatched issuer")
	}
	server.Close()
	if _, err := p.AuthorizationURL(context.Background(), "s", "v", "n"); err == nil {
		t.Fatal("unavailable provider accepted")
	}
	if _, err := New(context.Background(), cfg); err == nil {
		t.Fatal("unavailable provider at startup accepted")
	}
}

func TestInvalidMetadata(t *testing.T) {
	for _, scenario := range []string{"no S256", "unsafe endpoint", "other origin", "redirect", "no code"} {
		t.Run(scenario, func(t *testing.T) {
			var address string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "redirect" {
					http.Redirect(w, r, "https://other.example/discovery", 302)
					return
				}
				data := map[string]any{"issuer": address, "authorization_endpoint": address + "/auth", "token_endpoint": address + "/token", "jwks_uri": address + "/keys", "code_challenge_methods_supported": []string{"S256"}, "response_types_supported": []string{"code"}}
				switch scenario {
				case "no S256":
					data["code_challenge_methods_supported"] = []string{"plain"}
				case "no code":
					data["response_types_supported"] = []string{"token"}
				case "unsafe endpoint":
					data["authorization_endpoint"] = "javascript:alert(1)"
				case "other origin":
					data["authorization_endpoint"] = "https://other.example/auth"
				}
				json.NewEncoder(w).Encode(data)
			}))
			defer server.Close()
			address = server.URL
			cfg := application.OIDCConfig{Issuer: address, ClientID: "client", ClientSecret: "synthetic-client-secret", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", PublicOrigin: "http://localhost:8080", TransactionTTL: 5 * time.Minute}
			if _, err := New(context.Background(), cfg); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}
