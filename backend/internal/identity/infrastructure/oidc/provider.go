// Package oidc discovers the configured provider and builds authorization redirects.
// It does not exchange codes or verify ID tokens in Stage 1.
package oidc

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Provider struct {
	config application.OIDCConfig
	client *http.Client
}

func New(ctx context.Context, c application.OIDCConfig) (*Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	p := &Provider{config: c, client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OIDC discovery redirect rejected") }}}
	if _, err := p.discover(ctx); err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	return p, nil
}

func (p *Provider) discover(ctx context.Context) (*coreoidc.Provider, error) {
	provider, err := coreoidc.NewProvider(coreoidc.ClientContext(ctx, p.client), p.config.Issuer)
	if err != nil {
		return nil, err
	} // NewProvider verifies the exact discovered issuer.
	var claims struct {
		Methods   []string `json:"code_challenge_methods_supported"`
		Responses []string `json:"response_types_supported"`
	}
	if err := provider.Claims(&claims); err != nil {
		return nil, err
	}
	if !slices.Contains(claims.Methods, "S256") || !slices.Contains(claims.Responses, "code") {
		return nil, errors.New("provider requires code flow and S256 support")
	}
	endpoint, err := application.OIDCURL(provider.Endpoint().AuthURL)
	if err != nil {
		return nil, err
	}
	issuer, _ := application.OIDCURL(p.config.Issuer)
	if endpoint.Scheme != issuer.Scheme || endpoint.Host != issuer.Host {
		return nil, errors.New("authorization endpoint must share the configured issuer origin")
	}
	return provider, nil
}

func (p *Provider) AuthorizationURL(ctx context.Context, state, verifier, nonce string) (string, error) {
	// Refresh on each initiation: no transaction/cookie is issued if discovery fails.
	provider, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	c := oauth2.Config{ClientID: p.config.ClientID, RedirectURL: p.config.RedirectURL, Endpoint: provider.Endpoint(), Scopes: []string{coreoidc.ScopeOpenID, "profile", "email"}}
	return c.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), coreoidc.Nonce(nonce)), nil
}
