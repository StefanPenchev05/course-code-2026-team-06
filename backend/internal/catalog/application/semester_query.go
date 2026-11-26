package application

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type SemesterRepository interface {
	ListAvailableSemesters(context.Context, time.Time) ([]domain.Semester, error)
}

// SemesterService exposes only the calendar read needed to create availability.
// Trusted calendar creation and maintenance remain operator-only.
type SemesterService struct {
	repo SemesterRepository
	now  func() time.Time
}

func NewSemesters(repo SemesterRepository, now func() time.Time) *SemesterService {
	return &SemesterService{repo: repo, now: now}
}

func (s *SemesterService) List(ctx context.Context, actor identity.Principal) ([]domain.Semester, error) {
	if !actor.CanTutor() {
		return nil, identity.ErrForbidden
	}
	rows, err := s.repo.ListAvailableSemesters(ctx, s.now().UTC())
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.Semester{}
	}
	return rows, nil
}
