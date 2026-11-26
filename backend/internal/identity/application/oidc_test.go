package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type oidcProviderFake struct {
	state, verifier, nonce string
	err                    error
}

func (p *oidcProviderFake) AuthorizationURL(_ context.Context, state, verifier, nonce string) (string, error) {
	p.state, p.verifier, p.nonce = state, verifier, nonce
	return "https://idp.example/authorize", p.err
}

type oidcStoreFake struct {
	OIDCTransactions
	state string
	tx    domain.OIDCTransaction
	ttl   time.Duration
	err   error
}

func (s *oidcStoreFake) Save(_ context.Context, state string, tx domain.OIDCTransaction, ttl time.Duration) error {
	s.state, s.tx, s.ttl = state, tx, ttl
	return s.err
}
func TestOIDCStart(t *testing.T) {
	p := &oidcProviderFake{}
	store := &oidcStoreFake{}
	s := NewOIDCLogin(p, store, 5*time.Minute)
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	seen := map[string]bool{}
	for range 4 {
		result, err := s.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{p.state, p.verifier, p.nonce, result.BrowserBinding} {
			if !domain.ValidOIDCToken(v) || seen[v] {
				t.Fatal("invalid or repeated random value")
			}
			seen[v] = true
		}
		if store.state != p.state || store.tx.Nonce != p.nonce || store.tx.Verifier != p.verifier || store.tx.BindingHash != domain.OIDCHash(result.BrowserBinding) {
			t.Fatal("transaction binding mismatch")
		}
		if !store.tx.ExpiresAt.Equal(now.Add(5*time.Minute)) || store.ttl != 5*time.Minute || result.TTL != 5*time.Minute {
			t.Fatal("incorrect expiration")
		}
	}
	p.err = errors.New("provider secret detail")
	store.state = ""
	if _, err := s.Begin(context.Background()); !errors.Is(err, domain.ErrOIDCUnavailable) || store.state != "" {
		t.Fatal("provider failure wrote a transaction")
	}
	p.err = nil
	store.err = errors.New("Redis secret detail")
	if result, err := s.Begin(context.Background()); !errors.Is(err, domain.ErrOIDCUnavailable) || result.URL != "" || result.BrowserBinding != "" {
		t.Fatal("Redis failure returned credentials")
	}
}
func validOIDCConfig() OIDCConfig {
	return OIDCConfig{Issuer: "http://localhost:8180/realms/tutti-demo", ClientID: "tutti-backend", ClientSecret: "synthetic-client-secret", PublicOrigin: "http://localhost:8080", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", TransactionTTL: 5 * time.Minute}
}
func TestOIDCConfig(t *testing.T) {
	good := validOIDCConfig()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*OIDCConfig){
		func(c *OIDCConfig) { c.RequestTimeout = -time.Second }, func(c *OIDCConfig) { c.RequestTimeout = 11 * time.Second }, func(c *OIDCConfig) { c.PostLoginPath = "//evil.example" }, func(c *OIDCConfig) { c.PostLoginPath = "https://evil.example" }, func(c *OIDCConfig) { c.PostLoginPath = "/\\evil.example" }, func(c *OIDCConfig) { c.PostLoginPath = "/%2f%2fevil.example" }, func(c *OIDCConfig) { c.PostLoginPath = "/\n" },
		func(c *OIDCConfig) { c.Issuer = "" }, func(c *OIDCConfig) { c.ClientID = "" }, func(c *OIDCConfig) { c.ClientID = " bad" },
		func(c *OIDCConfig) { c.Issuer = "http://idp.example/realm" }, func(c *OIDCConfig) { c.Issuer = "https://user:password@idp.example/realm" },
		func(c *OIDCConfig) { c.Issuer += "?issuer=other" }, func(c *OIDCConfig) { c.Issuer += "/" },
		func(c *OIDCConfig) { c.RedirectURL = "https://evil.example/callback" }, func(c *OIDCConfig) { c.RedirectURL = "" },
		func(c *OIDCConfig) { c.PublicOrigin = "" }, func(c *OIDCConfig) { c.PublicOrigin = "http://app.example" },
		func(c *OIDCConfig) { c.TransactionTTL = 0 }, func(c *OIDCConfig) { c.TransactionTTL = 11 * time.Minute },
	} {
		c := good
		change(&c)
		if err := c.Validate(); err == nil {
			t.Fatalf("accepted invalid OIDC configuration")
		}
	}
}
