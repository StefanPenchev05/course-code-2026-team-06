package application

import (
	"context"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// PolicySessions is Scheduling's transaction-bound cancellation capability.
type PolicySessions interface {
	LockFacultySession(context.Context, string, string) (scheduling.Session, error)
	CancelForPolicy(context.Context, scheduling.Session, string, string, time.Time) (scheduling.Session, error)
}
type PolicyCancellationTx interface {
	CoordinatorFaculty(context.Context, string) (string, error)
	Sessions() PolicySessions
}
type PolicyCancellationRepository interface {
	Transact(context.Context, func(PolicyCancellationTx) error) error
}
type PolicyCancellationService struct {
	repo PolicyCancellationRepository
	now  func() time.Time
}

func NewPolicyCancellation(repo PolicyCancellationRepository, now func() time.Time) *PolicyCancellationService {
	return &PolicyCancellationService{repo: repo, now: now}
}
func (s *PolicyCancellationService) Cancel(ctx context.Context, actor identity.Principal, id, input string) (scheduling.Session, error) {
	if !actor.Coordinator() || !attendanceID.MatchString(actor.ID) {
		return scheduling.Session{}, domain.ErrForbidden
	}
	reason, err := scheduling.Reason(input)
	if err != nil || !attendanceID.MatchString(id) {
		return scheduling.Session{}, scheduling.ErrBookingInvalid
	}
	var result scheduling.Session
	err = s.repo.Transact(ctx, func(tx PolicyCancellationTx) error {
		faculty, err := tx.CoordinatorFaculty(ctx, actor.ID)
		if err != nil {
			return err
		}
		session, err := tx.Sessions().LockFacultySession(ctx, strings.ToLower(id), faculty)
		if err != nil {
			return err
		}
		if session.FacultyID != faculty {
			return scheduling.ErrNotFound
		}
		result, err = tx.Sessions().CancelForPolicy(ctx, session, actor.ID, reason, s.now().UTC().Truncate(time.Microsecond))
		return err
	})
	if err != nil {
		return scheduling.Session{}, err
	}
	return result, nil
}
