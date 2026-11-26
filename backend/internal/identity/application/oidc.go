package application

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"golang.org/x/oauth2"
)

type OIDCConfig struct {
	Issuer, ClientID, RedirectURL, PublicOrigin string
	ClientSecret                                string
	TransactionTTL                              time.Duration
	RequestTimeout                              time.Duration
	PostLoginPath                               string
}

// OIDCURL permits cleartext only on literal loopback addresses/localhost.
func OIDCURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("invalid OIDC URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, fmt.Errorf("OIDC URLs require HTTPS or loopback HTTP")
	}
	return u, nil
}

func (c OIDCConfig) Validate() error {
	origin, err := OIDCURL(c.PublicOrigin)
	if err != nil || origin.Path != "" || c.PublicOrigin != origin.Scheme+"://"+origin.Host {
		return fmt.Errorf("invalid OIDC public origin")
	}
	issuer, err := OIDCURL(c.Issuer)
	if err != nil || strings.HasSuffix(c.Issuer, "/") {
		return fmt.Errorf("invalid OIDC issuer")
	}
	if origin.Scheme == "https" && issuer.Scheme != "https" {
		return fmt.Errorf("HTTPS application requires HTTPS OIDC issuer")
	}
	if c.RedirectURL != c.PublicOrigin+"/api/v1/auth/oidc/callback" {
		return fmt.Errorf("OIDC redirect must be the configured application callback")
	}
	if c.ClientSecret == "" || strings.TrimSpace(c.ClientSecret) != c.ClientSecret || len(c.ClientSecret) > 4096 {
		return fmt.Errorf("OIDC client secret is required")
	}
	if c.ClientID == "" || len(c.ClientID) > 200 || strings.ContainsAny(c.ClientID, " \t\r\n") {
		return fmt.Errorf("invalid OIDC client ID")
	}
	if c.TransactionTTL < time.Minute || c.TransactionTTL > 10*time.Minute || c.TransactionTTL%time.Second != 0 {
		return fmt.Errorf("OIDC transaction TTL must be whole seconds from 1m to 10m")
	}
	if c.RequestTimeout != 0 && (c.RequestTimeout < time.Second || c.RequestTimeout > 10*time.Second || c.RequestTimeout%time.Second != 0) {
		return fmt.Errorf("OIDC HTTP timeout must be whole seconds from 1s to 10s")
	}
	path := c.SuccessPath()
	// A fixed, simple same-origin path, never a caller-provided redirect URL.
	if len(path) > 512 || strings.HasPrefix(path, "//") || !oidcSuccessPath.MatchString(path) {
		return fmt.Errorf("invalid OIDC post-login path")
	}
	return nil
}

var oidcSuccessPath = regexp.MustCompile(`^/[A-Za-z0-9/_-]*$`)

func (c OIDCConfig) SuccessPath() string {
	if c.PostLoginPath == "" {
		return "/"
	}
	return c.PostLoginPath
}

type OIDCTransactions interface {
	Save(context.Context, string, domain.OIDCTransaction, time.Duration) error
	// Consume must compare browser binding and remove a matching transaction atomically.
	// A mismatched browser must not consume another browser's transaction.
	Consume(context.Context, string, string) (domain.OIDCTransaction, error)
}

type OIDCAuthorization interface {
	AuthorizationURL(ctx context.Context, state, verifier, nonce string) (string, error)
}

type OIDCLogin struct {
	provider     OIDCAuthorization
	transactions OIDCTransactions
	ttl          time.Duration
	now          func() time.Time
}

func NewOIDCLogin(provider OIDCAuthorization, transactions OIDCTransactions, ttl time.Duration) *OIDCLogin {
	return &OIDCLogin{provider: provider, transactions: transactions, ttl: ttl, now: time.Now}
}

type OIDCStart struct {
	URL, BrowserBinding string
	TTL                 time.Duration
}

func (s *OIDCLogin) Begin(ctx context.Context) (OIDCStart, error) {
	state, binding, nonce := randomToken(), randomToken(), randomToken()
	verifier := oauth2.GenerateVerifier()
	target, err := s.provider.AuthorizationURL(ctx, state, verifier, nonce)
	if err != nil {
		return OIDCStart{}, domain.ErrOIDCUnavailable
	}
	tx := domain.OIDCTransaction{BindingHash: domain.OIDCHash(binding), Verifier: verifier, Nonce: nonce, ExpiresAt: s.now().UTC().Add(s.ttl)}
	if err := s.transactions.Save(ctx, state, tx, s.ttl); err != nil {
		return OIDCStart{}, domain.ErrOIDCUnavailable
	}
	return OIDCStart{URL: target, BrowserBinding: binding, TTL: s.ttl}, nil
}
