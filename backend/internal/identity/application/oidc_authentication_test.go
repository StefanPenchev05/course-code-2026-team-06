package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"golang.org/x/oauth2"
)

type authenticationTx struct{ OIDCTransactions }

func (authenticationTx) Consume(context.Context, string, string) (domain.OIDCTransaction, error) {
	return domain.OIDCTransaction{Verifier: oauth2.GenerateVerifier(), Nonce: oauth2.GenerateVerifier(), ExpiresAt: time.Now().Add(time.Minute)}, nil
}

type authenticationExchange struct{}

func (authenticationExchange) ExchangeAndVerify(context.Context, string, string, string) (domain.VerifiedIdentity, error) {
	return domain.VerifiedIdentity{Issuer: bindingIssuer, Subject: "verified-subject"}, nil
}

type authenticationRepo struct {
	Repository
	principal domain.Principal
	err       error
	calls     int
}

func (r *authenticationRepo) Principal(context.Context, string) (domain.Principal, error) {
	r.calls++
	return r.principal, r.err
}

type authenticationSessions struct {
	Sessions
	err                 error
	key, previous, user string
	ttl                 time.Duration
	calls               int
}

func (s *authenticationSessions) Rotate(_ context.Context, key, user, previous string, ttl time.Duration) error {
	s.calls++
	s.key, s.previous, s.user, s.ttl = key, previous, user, ttl
	return s.err
}

func TestOIDCAuthenticationResolutionAndSessionFailures(t *testing.T) {
	for _, scenario := range []string{"tutor", "pending", "rejected", "coordinator", "unlinked", "conflict", "database lookup", "database recheck", "suspended", "closed", "suspended after resolution", "Redis"} {
		t.Run(scenario, func(t *testing.T) {
			principal := domain.Principal{ID: bindingUser, Student: true, Tutor: true, Verification: domain.Approved}
			lookup := &bindingFake{principal: principal}
			repo := &authenticationRepo{principal: principal}
			sessions := &authenticationSessions{}
			want := error(nil)
			switch scenario {
			case "pending":
				lookup.principal.Verification = domain.Pending
				repo.principal = lookup.principal
			case "rejected":
				lookup.principal.Verification = domain.Rejected
				repo.principal = lookup.principal
			case "coordinator":
				lookup.principal = domain.Principal{ID: bindingUser, CoordinatorFaculty: "faculty"}
				repo.principal = lookup.principal
			case "unlinked":
				lookup.err = domain.ErrOIDCIdentityUnlinked
				want = domain.ErrOIDCAuthentication
			case "conflict":
				lookup.err = domain.ErrConflict
				want = domain.ErrOIDCAuthentication
			case "database lookup":
				lookup.err = errors.New("private database password")
				want = domain.ErrOIDCUnavailable
			case "database recheck":
				repo.err = errors.New("private database password")
				want = domain.ErrOIDCUnavailable
			case "suspended":
				lookup.principal.Suspended = true
				want = domain.ErrForbidden
			case "closed":
				lookup.principal.Closed = true
				want = domain.ErrForbidden
			case "suspended after resolution":
				repo.principal.Suspended = true
				want = domain.ErrForbidden
			case "Redis":
				sessions.err = errors.New("private Redis password")
				want = domain.ErrOIDCUnavailable
			}
			resolver, err := NewOIDCIdentityResolver(lookup, bindingIssuer)
			if err != nil {
				t.Fatal(err)
			}
			service := &Service{repo: repo, sessions: sessions, config: Config{SessionTTL: time.Hour}}
			flow := NewOIDCAuthentication(NewOIDCCallback(authenticationExchange{}, authenticationTx{}, bindingIssuer), resolver, service)
			old := oauth2.GenerateVerifier()
			result, err := flow.Complete(context.Background(), OIDCResponse{State: oauth2.GenerateVerifier(), Code: "synthetic-code"}, oauth2.GenerateVerifier(), old)
			if !errors.Is(err, want) || !result.Consumed {
				t.Fatal("incorrect outcome", err)
			}
			if want != nil {
				if result.Token != "" || (scenario != "Redis" && sessions.calls != 0) {
					t.Fatal("failed authentication issued a session")
				}
				return
			}
			if !validToken(result.Token) || result.Token == old || sessions.previous != sessionKey(old) || sessions.key != sessionKey(result.Token) || sessions.user != bindingUser || sessions.ttl != time.Hour || result.TTL != time.Hour {
				t.Fatal("session not fresh, opaque, rotated and bounded")
			}
			if repo.calls != 1 {
				t.Fatal("missing current account recheck")
			}
		})
	}
}
