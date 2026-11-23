package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identityapp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type suspensionFixture struct {
	calls          []string
	target         identity.Principal
	home, faculty  string
	err, commitErr error
}

func (f *suspensionFixture) Transact(_ context.Context, work func(SuspensionTx) error) error {
	f.calls = append(f.calls, "transaction")
	if err := work(f); err != nil {
		return err
	}
	return f.commitErr
}
func (f *suspensionFixture) Accounts() identityapp.SuspensionAccounts { return f }
func (f *suspensionFixture) Catalog() catalog.AcademicCatalog         { return f }
func (f *suspensionFixture) Sessions() SuspensionSessions             { return f }
func (f *suspensionFixture) LockAdministration(context.Context, string, string) (identity.Principal, string, error) {
	f.calls = append(f.calls, "authority")
	return f.target, f.faculty, f.err
}
func (f *suspensionFixture) GetProgramFaculty(context.Context, string) (string, error) {
	f.calls = append(f.calls, "home")
	return f.home, nil
}
func (f *suspensionFixture) SubjectBelongsToProgram(context.Context, string, string) (bool, error) {
	panic("unexpected eligibility mutation")
}
func (f *suspensionFixture) Change(_ context.Context, p identity.Principal, faculty, actor, reason string, suspend bool, at time.Time) (identity.SuspensionDecision, error) {
	f.calls = append(f.calls, "account")
	return identity.SuspensionDecision{ID: "decision", UserID: p.ID, FacultyID: faculty, ActorID: actor, Reason: reason, IsSuspended: suspend, WasSuspended: p.Suspended, At: at}, nil
}
func (f *suspensionFixture) CancelFuture(context.Context, string, string, string, time.Time) (int, error) {
	f.calls = append(f.calls, "sessions")
	return 3, nil
}
func TestSuspensionAuthorizationAndAtomicResult(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000025"
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	actor := identity.Principal{ID: id, CoordinatorFaculty: "stale"}
	for _, p := range []identity.Principal{{}, {ID: id, Student: true}, {ID: id, Tutor: true}, {ID: id, CoordinatorFaculty: "a", Suspended: true}, {ID: id, CoordinatorFaculty: "a", Closed: true}} {
		f := &suspensionFixture{}
		if _, err := NewSuspension(f, func() time.Time { return at }).Change(context.Background(), p, id, "reason", true); !errors.Is(err, identity.ErrForbidden) || len(f.calls) != 0 {
			t.Fatal(err, f.calls)
		}
	}
	for _, suspend := range []bool{true, false} {
		f := &suspensionFixture{target: identity.Principal{ID: id, Student: true, Suspended: !suspend}, home: "current", faculty: "current"}
		s := NewSuspension(f, func() time.Time { return at })
		got, err := s.Change(context.Background(), actor, id, " Révision ", suspend)
		want := "transaction,authority,home,account"
		if suspend {
			want += ",sessions"
		}
		if err != nil || got.Decision.Reason != "Révision" || strings.Join(f.calls, ",") != want {
			t.Fatal(got, err, f.calls)
		}
		f.home = "other"
		if _, err := s.Change(context.Background(), actor, id, "reason", suspend); !errors.Is(err, identity.ErrNotFound) {
			t.Fatal(err)
		}
		f.home = "current"
		f.commitErr = errors.New("commit failed")
		if got, err := s.Change(context.Background(), actor, id, "reason", suspend); err == nil || got.Decision.ID != "" {
			t.Fatal(got, err)
		}
		f.commitErr = nil
		f.err = identity.ErrForbidden
		if _, err := s.Change(context.Background(), actor, id, "reason", suspend); !errors.Is(err, identity.ErrForbidden) {
			t.Fatal(err)
		}
	}
}
