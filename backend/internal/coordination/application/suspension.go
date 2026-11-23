package application

import (
	"context"
	"strings"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identityapp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type SuspensionSessions interface {
	CancelFuture(ctx context.Context, target, actor, decision string, at time.Time) (int, error)
}

type SuspensionTx interface {
	Accounts() identityapp.SuspensionAccounts
	Catalog() catalog.AcademicCatalog
	Sessions() SuspensionSessions
}

type SuspensionRepository interface {
	Transact(context.Context, func(SuspensionTx) error) error
}

type SuspensionResult struct {
	Decision          identity.SuspensionDecision
	CancelledSessions int
}

type SuspensionService struct {
	repo SuspensionRepository
	now  func() time.Time
}

func NewSuspension(repo SuspensionRepository, now func() time.Time) *SuspensionService {
	return &SuspensionService{repo, now}
}

func (s *SuspensionService) Change(ctx context.Context, actor identity.Principal, id, input string, suspend bool) (SuspensionResult, error) {
	if !actor.Coordinator() || !attendanceID.MatchString(actor.ID) {
		return SuspensionResult{}, identity.ErrForbidden
	}
	reason, err := identity.SuspensionReason(input)
	if err != nil || !attendanceID.MatchString(id) {
		return SuspensionResult{}, identity.ErrInvalid
	}
	var result SuspensionResult
	err = s.repo.Transact(ctx, func(tx SuspensionTx) error {
		// A retry must never retain output from an aborted transaction.
		result = SuspensionResult{}
		target, faculty, err := tx.Accounts().LockAdministration(ctx, strings.ToLower(actor.ID), strings.ToLower(id))
		if err != nil {
			return err
		}
		home, err := tx.Catalog().GetProgramFaculty(ctx, target.ProgramID)
		if err != nil {
			return err
		}
		if home != faculty {
			return identity.ErrNotFound
		}
		if err := target.ChangeSuspension(suspend); err != nil {
			return err
		}
		at := s.now().UTC().Truncate(time.Microsecond)
		if at.IsZero() {
			return identity.ErrInvalid
		}
		result.Decision, err = tx.Accounts().Change(ctx, target, home, strings.ToLower(actor.ID), reason, suspend, at)
		if err != nil {
			return err
		}
		if suspend {
			result.CancelledSessions, err = tx.Sessions().CancelFuture(ctx, target.ID, strings.ToLower(actor.ID), result.Decision.ID, at)
		}
		return err
	})
	if err != nil {
		return SuspensionResult{}, err
	}
	return result, nil
}
