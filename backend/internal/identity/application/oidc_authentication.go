package application

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// The callback receives only the reader capability, never trusted provisioning.
type OIDCResolver interface {
	ResolveOIDCIdentity(context.Context, domain.VerifiedIdentity) (domain.Principal, error)
}

type OIDCAuthentication struct {
	callback *OIDCCallback
	resolver OIDCResolver
	service  *Service
}

type OIDCAuthenticationResult struct {
	Consumed bool
	Token    string
	TTL      time.Duration
}

func NewOIDCAuthentication(callback *OIDCCallback, resolver OIDCResolver, service *Service) *OIDCAuthentication {
	return &OIDCAuthentication{callback: callback, resolver: resolver, service: service}
}

func (s *OIDCAuthentication) Complete(ctx context.Context, response OIDCResponse, binding, previousToken string) (OIDCAuthenticationResult, error) {
	verified, err := s.callback.Complete(ctx, response, binding)
	result := OIDCAuthenticationResult{Consumed: verified.Consumed}
	if err != nil {
		return result, err
	}
	principal, err := s.resolver.ResolveOIDCIdentity(ctx, verified.Identity)
	if err != nil {
		return result, oidcAccountError(err)
	}
	// Re-read current application authorization immediately before issuing the
	// opaque session. Every subsequent request also reloads current permissions.
	principal, err = s.service.repo.Principal(ctx, principal.ID)
	if err != nil {
		return result, oidcAccountError(err)
	}
	if !principal.Active() || (!principal.Student && !principal.Coordinator()) {
		return result, domain.ErrForbidden
	}
	token := randomToken()
	oldKey := ""
	if validToken(previousToken) {
		oldKey = sessionKey(previousToken)
	}
	if err := s.service.sessions.Rotate(ctx, sessionKey(token), principal.ID, oldKey, s.service.config.SessionTTL); err != nil {
		return result, domain.ErrOIDCUnavailable
	}
	result.Token = token
	result.TTL = s.service.config.SessionTTL
	return result, nil
}

func oidcAccountError(err error) error {
	if errors.Is(err, domain.ErrForbidden) {
		return domain.ErrForbidden
	}
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrOIDCIdentityUnlinked) || errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalid) {
		return domain.ErrOIDCAuthentication
	}
	return domain.ErrOIDCUnavailable
}
