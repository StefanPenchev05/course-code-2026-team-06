package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func (p *Provider) ExchangeAndVerify(ctx context.Context, code, verifier, nonce string) (domain.VerifiedIdentity, error) {
	var identity domain.VerifiedIdentity
	if ctx.Err() != nil {
		return identity, domain.ErrOIDCUnavailable
	}
	endpoint := p.protocol.Endpoint()
	// Fix client authentication to HTTP Basic: avoid OAuth2's automatic retry/probe.
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	config := oauth2.Config{ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, RedirectURL: p.config.RedirectURL, Endpoint: endpoint}
	token, err := config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, p.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		var rejected *oauth2.RetrieveError
		if errors.As(err, &rejected) && rejected.Response != nil && rejected.Response.StatusCode < 500 && rejected.Response.StatusCode != 429 {
			return identity, domain.ErrOIDCAuthentication
		}
		return identity, domain.ErrOIDCUnavailable
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" || len(raw) > 32768 || token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") || (!token.Expiry.IsZero() && !time.Now().Before(token.Expiry)) {
		return identity, domain.ErrOIDCAuthentication
	}
	verified, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return identity, domain.ErrOIDCAuthentication
	}
	// The library verifies signature, issuer, audience, expiry and (with leeway) nbf.
	// Enforce our Keycloak contract and the claims it explicitly leaves to callers.
	var claims struct {
		AZP  json.RawMessage `json:"azp"`
		NBF  *int64          `json:"nbf"`
		Type string          `json:"typ"`
	}
	if err := verified.Claims(&claims); err != nil {
		return identity, domain.ErrOIDCAuthentication
	}
	now := time.Now()
	if verified.Issuer != p.config.Issuer || strings.TrimSpace(verified.Subject) == "" || len(verified.Subject) > 255 || verified.IssuedAt.IsZero() || verified.IssuedAt.Unix() <= 0 || verified.IssuedAt.After(now.Add(time.Minute)) || !now.Before(verified.Expiry) || !verified.IssuedAt.Before(verified.Expiry) {
		return identity, domain.ErrOIDCAuthentication
	}
	var authorizedParty string
	if len(claims.AZP) > 0 {
		if err := json.Unmarshal(claims.AZP, &authorizedParty); err != nil || authorizedParty != p.config.ClientID {
			return identity, domain.ErrOIDCAuthentication
		}
	} else if len(verified.Audience) > 1 {
		return identity, domain.ErrOIDCAuthentication
	}
	// No additional resource audiences have been approved for this client.
	for _, aud := range verified.Audience {
		if aud != p.config.ClientID {
			return identity, domain.ErrOIDCAuthentication
		}
	}
	if claims.NBF != nil && (time.Unix(*claims.NBF, 0).After(now.Add(time.Minute)) || *claims.NBF >= verified.Expiry.Unix()) {
		return identity, domain.ErrOIDCAuthentication
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(verified.Nonce), []byte(nonce)) != 1 {
		return identity, domain.ErrOIDCAuthentication
	}
	if claims.Type != "" && claims.Type != "ID" {
		return identity, domain.ErrOIDCAuthentication
	}
	// JWT is optional in JOSE, but access-token-specific or other types are rejected.
	signed, err := jose.ParseSigned(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return identity, domain.ErrOIDCAuthentication
	}
	headerType, exists := signed.Signatures[0].Header.ExtraHeaders[jose.HeaderType]
	if exists && headerType != "JWT" {
		return identity, domain.ErrOIDCAuthentication
	}
	if verified.AccessTokenHash != "" {
		if err := verified.VerifyAccessToken(token.AccessToken); err != nil {
			return identity, domain.ErrOIDCAuthentication
		}
	}
	if ctx.Err() != nil {
		return identity, domain.ErrOIDCUnavailable
	}
	return domain.VerifiedIdentity{Issuer: verified.Issuer, Subject: verified.Subject}, nil
}
