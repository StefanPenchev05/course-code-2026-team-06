package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

const bindingIssuer = "http://localhost:8180/realms/tutti-demo"
const bindingUser = "00000000-0000-0000-0000-00000000ab01"

type bindingFake struct {
	principal domain.Principal
	err       error
	calls     int
	identity  domain.VerifiedIdentity
	user      string
}

func (f *bindingFake) LookupOIDCIdentity(_ context.Context, id domain.VerifiedIdentity) (domain.Principal, error) {
	f.calls++
	f.identity = id
	return f.principal, f.err
}
func (f *bindingFake) BindOIDCIdentity(_ context.Context, user string, id domain.VerifiedIdentity) error {
	f.calls++
	f.identity = id
	f.user = user
	return f.err
}

func TestOIDCBindingValidation(t *testing.T) {
	f := &bindingFake{}
	resolver, err := NewOIDCIdentityResolver(f, bindingIssuer)
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := NewTrustedOIDCProvisioner(f, bindingIssuer)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []domain.VerifiedIdentity{
		{}, {Issuer: bindingIssuer}, {Issuer: "https://other.example", Subject: "same-subject"},
		{Issuer: bindingIssuer, Subject: " "}, {Issuer: bindingIssuer, Subject: "\u2003"},
		{Issuer: bindingIssuer, Subject: strings.Repeat("s", 256)}, {Issuer: bindingIssuer, Subject: "bad\x00subject"},
		{Issuer: bindingIssuer, Subject: string([]byte{0xff})},
	} {
		if _, err := resolver.ResolveOIDCIdentity(context.Background(), id); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
		if err := provisioner.LinkOIDCIdentity(context.Background(), bindingUser, id); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid identity reached persistence")
	}
	good := domain.VerifiedIdentity{Issuer: bindingIssuer, Subject: "stable-subject"}
	if err := provisioner.LinkOIDCIdentity(context.Background(), "not-a-UUID", good); !errors.Is(err, domain.ErrInvalid) || f.calls != 0 {
		t.Fatal("invalid account reached persistence")
	}
	for _, issuer := range []string{"", "http://remote.example/realm", bindingIssuer + "/", bindingIssuer + "?x=1", bindingIssuer + " ", strings.Repeat("s", 2049)} {
		if _, err := NewOIDCIdentityResolver(f, issuer); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid resolver issuer")
		}
		if _, err := NewTrustedOIDCProvisioner(f, issuer); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("invalid provisioner issuer")
		}
	}
	if err := provisioner.LinkOIDCIdentity(context.Background(), bindingUser, good); err != nil || f.user != bindingUser || f.identity != good {
		t.Fatal("binding key not passed exactly", err)
	}
	f.err = domain.ErrConflict
	if err := provisioner.LinkOIDCIdentity(context.Background(), bindingUser, good); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	f.err = domain.ErrForbidden
	if err := provisioner.LinkOIDCIdentity(context.Background(), bindingUser, good); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
}

func TestOIDCResolutionPreservesPermissions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		p         domain.Principal
		forbidden bool
	}{
		{"approved student", domain.Principal{ID: bindingUser, Student: true, Verification: domain.Approved}, false},
		{"pending student", domain.Principal{ID: bindingUser, Student: true, Verification: domain.Pending}, false},
		{"rejected student", domain.Principal{ID: bindingUser, Student: true, Tutor: true, Verification: domain.Rejected}, false},
		{"approved tutor", domain.Principal{ID: bindingUser, Student: true, Tutor: true, Verification: domain.Approved}, false},
		{"coordinator", domain.Principal{ID: bindingUser, CoordinatorFaculty: "faculty"}, false},
		{"suspended", domain.Principal{ID: bindingUser, Student: true, Suspended: true}, true},
		{"closed", domain.Principal{ID: bindingUser, CoordinatorFaculty: "faculty", Closed: true}, true},
		{"roleless", domain.Principal{ID: bindingUser}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &bindingFake{principal: tc.p}
			resolver, err := NewOIDCIdentityResolver(f, bindingIssuer)
			if err != nil {
				t.Fatal(err)
			}
			p, err := resolver.ResolveOIDCIdentity(context.Background(), domain.VerifiedIdentity{Issuer: bindingIssuer, Subject: "subject"})
			if tc.forbidden {
				if !errors.Is(err, domain.ErrForbidden) || p.ID != "" {
					t.Fatal("restricted account resolved")
				}
				return
			}
			if err != nil || p != tc.p {
				t.Fatal("authority was changed", err)
			}
			if p.Verification != domain.Approved && (p.VerifiedStudent() || p.CanTutor()) {
				t.Fatal("OIDC elevated academic privileges")
			}
		})
	}
	for _, failure := range []error{domain.ErrOIDCIdentityUnlinked, domain.ErrNotFound, errors.New("database error"), context.Canceled} {
		f := &bindingFake{err: failure}
		resolver, _ := NewOIDCIdentityResolver(f, bindingIssuer)
		if p, err := resolver.ResolveOIDCIdentity(context.Background(), domain.VerifiedIdentity{Issuer: bindingIssuer, Subject: "unknown"}); !errors.Is(err, failure) || p.ID != "" {
			t.Fatal("resolution error not preserved")
		}
	}
}
