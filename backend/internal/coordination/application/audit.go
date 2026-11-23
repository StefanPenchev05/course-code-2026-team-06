package application

import (
	"context"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type AuditRepository interface {
	ListAudit(ctx context.Context, actor, user, category string, limit, offset int) ([]domain.AuditEntry, error)
}
type AuditService struct{ repo AuditRepository }

func NewAudit(repo AuditRepository) *AuditService { return &AuditService{repo} }
func (s *AuditService) List(ctx context.Context, actor identity.Principal, user, category string, limit, offset int) ([]domain.AuditEntry, error) {
	if !actor.Coordinator() {
		return nil, domain.ErrForbidden
	}
	if !attendanceID.MatchString(user) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 ||
		(category != "SUSPENSION" && category != "VIOLATION" && category != "FLAG") {
		return nil, domain.ErrInvalid
	}
	return s.repo.ListAudit(ctx, actor.ID, user, category, limit+1, offset)
}
