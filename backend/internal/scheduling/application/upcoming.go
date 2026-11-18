package application

import (
	"context"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// UpcomingTutorSessions shares Scheduling's accepted sessions and trusted clock.
func (s *BookingService) UpcomingTutorSessions(ctx context.Context, actor identity.Principal, limit, offset int) ([]domain.Session, error) {
	if !actor.CanTutor() {
		return nil, domain.ErrForbidden
	}
	if limit < 1 || limit > 101 || offset < 0 || offset > 100000 {
		return nil, domain.ErrBookingInvalid
	}
	return s.repo.UpcomingTutorSessions(ctx, actor.ID, s.clock(), Page{Limit: limit, Offset: offset})
}
