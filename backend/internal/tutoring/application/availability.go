package application

import (
	"context"
	"errors"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

// CanManageAvailability uses the approved tutor profile projected by Identity.
func (s *Service) CanManageAvailability(actor identity.Principal) bool {
	return actor.CanTutor()
}

// CanViewAvailability follows the same visibility rules as a tutor profile.
func (s *Service) CanViewAvailability(ctx context.Context, viewer identity.Principal, tutorID string) (bool, error) {
	if !viewer.VerifiedStudent() {
		return false, nil
	}
	_, err := s.Profile(ctx, viewer, tutorID, false)
	if errors.Is(err, domain.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
