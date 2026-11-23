package application

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// OIDCIdentityReader resolves already verified provider identities only.
// It must read the mapping and current Principal from one consistent snapshot.
type OIDCIdentityReader interface {
	LookupOIDCIdentity(context.Context, domain.VerifiedIdentity) (domain.Principal, error)
}

// OIDCIdentityWriter is a trusted provisioning capability, never a public API.
// Implementations lock/recheck the account and create the binding atomically.
type OIDCIdentityWriter interface {
	BindOIDCIdentity(context.Context, string, domain.VerifiedIdentity) error
}

type OIDCIdentityResolver struct {
	reader OIDCIdentityReader
	issuer string
}

func NewOIDCIdentityResolver(reader OIDCIdentityReader, issuer string) (*OIDCIdentityResolver, error) {
	if err := validateBindingIssuer(issuer); err != nil {
		return nil, err
	}
	return &OIDCIdentityResolver{reader: reader, issuer: issuer}, nil
}

func (s *OIDCIdentityResolver) ResolveOIDCIdentity(ctx context.Context, identity domain.VerifiedIdentity) (domain.Principal, error) {
	if err := validateBindingIdentity(identity, s.issuer); err != nil {
		return domain.Principal{}, err
	}
	principal, err := s.reader.LookupOIDCIdentity(ctx, identity)
	if err != nil {
		return domain.Principal{}, err
	}
	// Match existing Identity login eligibility; pending/rejected students retain
	// restricted student access, never verified-student or tutor privileges.
	if !principal.Active() || (!principal.Student && !principal.Coordinator()) {
		return domain.Principal{}, domain.ErrForbidden
	}
	return principal, nil
}

// TrustedOIDCProvisioner must only be constructed/injected into trusted internal
// setup or an explicitly authorized future linking workflow. Stage 3B callbacks
// receive the resolver capability, not this writer or a caller-chosen user ID.
type TrustedOIDCProvisioner struct {
	writer OIDCIdentityWriter
	issuer string
}

func NewTrustedOIDCProvisioner(writer OIDCIdentityWriter, issuer string) (*TrustedOIDCProvisioner, error) {
	if err := validateBindingIssuer(issuer); err != nil {
		return nil, err
	}
	return &TrustedOIDCProvisioner{writer: writer, issuer: issuer}, nil
}

func (s *TrustedOIDCProvisioner) LinkOIDCIdentity(ctx context.Context, userID string, identity domain.VerifiedIdentity) error {
	if !uuid.MatchString(userID) {
		return domain.ErrInvalid
	}
	if err := validateBindingIdentity(identity, s.issuer); err != nil {
		return err
	}
	return s.writer.BindOIDCIdentity(ctx, userID, identity)
}

func validateBindingIssuer(issuer string) error {
	parsed, err := OIDCURL(issuer)
	if err != nil || len(issuer) > 2048 || !utf8.ValidString(issuer) || strings.HasSuffix(issuer, "/") || parsed.String() != issuer {
		return fmt.Errorf("%w: invalid trusted OIDC issuer", domain.ErrInvalid)
	}
	return nil
}

func validateBindingIdentity(identity domain.VerifiedIdentity, issuer string) error {
	if identity.Issuer != issuer || strings.TrimSpace(identity.Subject) == "" || len(identity.Subject) > 255 || !utf8.ValidString(identity.Subject) || strings.IndexFunc(identity.Subject, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: invalid verified OIDC identity", domain.ErrInvalid)
	}
	return nil
}
