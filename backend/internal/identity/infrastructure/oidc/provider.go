// Package oidc discovers the configured provider and builds authorization redirects.
// Stage 2 exchanges codes and verifies provider identity without creating Tutti accounts.
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
	config   application.OIDCConfig
	client   *http.Client
	protocol *coreoidc.Provider
	verifier *coreoidc.IDTokenVerifier
}

func New(ctx context.Context, c application.OIDCConfig) (*Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	p := &Provider{config: c, client: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("OIDC discovery redirect rejected") }}}
	protocol, err := p.discover(ctx)
	if err != nil {
		return nil, errors.New("OIDC discovery failed")
	}
	p.protocol = protocol
	p.verifier = protocol.VerifierContext(coreoidc.ClientContext(context.Background(), p.client), &coreoidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{coreoidc.RS256}})
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
	// Token/JWKS URLs are trusted only on the exact configured issuer origin.
	var endpoints struct {
		Token string `json:"token_endpoint"`
		Keys  string `json:"jwks_uri"`
	}
	if err := provider.Claims(&endpoints); err != nil {
		return nil, err
	}
	for _, raw := range []string{endpoints.Token, endpoints.Keys} {
		u, err := application.OIDCURL(raw)
		if err != nil || u.Scheme != issuer.Scheme || u.Host != issuer.Host {
			return nil, errors.New("invalid provider endpoint origin")
		}
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
