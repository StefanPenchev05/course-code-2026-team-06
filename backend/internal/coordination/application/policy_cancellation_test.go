package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type policyFixture struct {
	calls        []string
	faculty      string
	authorityErr error
	session      scheduling.Session
}

func (f *policyFixture) Transact(_ context.Context, work func(PolicyCancellationTx) error) error {
	f.calls = append(f.calls, "transaction")
	return work(f)
}
func (f *policyFixture) CoordinatorFaculty(context.Context, string) (string, error) {
	f.calls = append(f.calls, "authority")
	return f.faculty, f.authorityErr
}
func (f *policyFixture) Sessions() PolicySessions { return f }
func (f *policyFixture) LockFacultySession(_ context.Context, _ string, faculty string) (scheduling.Session, error) {
	f.calls = append(f.calls, "session")
	if faculty != f.session.FacultyID {
		return scheduling.Session{}, scheduling.ErrNotFound
	}
	return f.session, nil
}
func (f *policyFixture) CancelForPolicy(_ context.Context, s scheduling.Session, actor, reason string, now time.Time) (scheduling.Session, error) {
	f.calls = append(f.calls, "cancel")
	return s.CancelForPolicy(actor, reason, now)
}
func TestPolicyCancellationCurrentFacultyAndRole(t *testing.T) {
	const id = "00000000-0000-0000-0000-000000000024"
	valid := identity.Principal{ID: id, CoordinatorFaculty: "stale faculty", Student: true}
	now := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, actor := range []identity.Principal{{}, {ID: id, Student: true}, {ID: id, Tutor: true}, {ID: id, CoordinatorFaculty: "a", Suspended: true}, {ID: id, CoordinatorFaculty: "a", Closed: true}} {
		f := &policyFixture{}
		_, err := NewPolicyCancellation(f, func() time.Time { return now }).Cancel(context.Background(), actor, id, "Reason")
		if !errors.Is(err, domain.ErrForbidden) || len(f.calls) != 0 {
			t.Fatal(actor, err, f.calls)
		}
	}
	f := &policyFixture{faculty: "current faculty", session: scheduling.Session{ID: id, FacultyID: "current faculty", Status: scheduling.Requested}}
	s := NewPolicyCancellation(f, func() time.Time { return now })
	got, err := s.Cancel(context.Background(), valid, id, " Reason ")
	if err != nil || got.CancelledBy != id || got.CancellationReason != "Reason" || strings.Join(f.calls, ",") != "transaction,authority,session,cancel" {
		t.Fatal(got, err, f.calls)
	}
	f.calls = nil
	f.authorityErr = domain.ErrForbidden
	if _, err := s.Cancel(context.Background(), valid, id, "Reason"); !errors.Is(err, domain.ErrForbidden) || strings.Join(f.calls, ",") != "transaction,authority" {
		t.Fatal(err, f.calls)
	}
	f.calls = nil
	f.authorityErr = nil
	f.faculty = "other faculty"
	if _, err := s.Cancel(context.Background(), valid, id, "Reason"); !errors.Is(err, scheduling.ErrNotFound) {
		t.Fatal(err)
	}
	for _, input := range []struct{ id, reason string }{{"bad", "Reason"}, {id, " "}, {id, strings.Repeat("a", 2001)}, {id, string([]byte{0xff})}} {
		f.calls = nil
		if _, err := s.Cancel(context.Background(), valid, input.id, input.reason); !errors.Is(err, scheduling.ErrBookingInvalid) || len(f.calls) != 0 {
			t.Fatal(err, f.calls)
		}
	}
}
