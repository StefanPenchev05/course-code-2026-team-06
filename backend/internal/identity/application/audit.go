package application

import (
	"context"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// SuspensionHistory reads only events owned by the authorized historical faculty.
// The caller supplies a faculty resolved by Identity in the same read snapshot.
type SuspensionHistory interface {
	ListSuspensionHistory(ctx context.Context, user, faculty string, limit, offset int) ([]domain.SuspensionDecision, error)
}
