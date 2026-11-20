package application

import (
	"context"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type FlaggedUsersRepository interface {
	ListFlaggedUsers(ctx context.Context, actor string, limit, offset int) ([]domain.FlaggedUser, error)
}

type FlaggedUsersService struct{ repo FlaggedUsersRepository }

func NewFlaggedUsers(repo FlaggedUsersRepository) *FlaggedUsersService {
	return &FlaggedUsersService{repo}
}

func (s *FlaggedUsersService) List(ctx context.Context, actor identity.Principal, limit, offset int) ([]domain.FlaggedUser, error) {
	if !actor.Coordinator() {
		return nil, domain.ErrForbidden
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, domain.ErrInvalid
	}
	return s.repo.ListFlaggedUsers(ctx, actor.ID, limit+1, offset)
}
