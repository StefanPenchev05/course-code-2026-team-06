package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PolicyCancellationDependencies struct {
	LockCoordinatorFaculty func(context.Context, pgx.Tx, string) (string, error)
	Sessions               func(pgx.Tx) application.PolicySessions
}
type PolicyCancellationRepository struct {
	db   *pgxpool.Pool
	deps PolicyCancellationDependencies
}

func NewPolicyCancellation(db *pgxpool.Pool, deps PolicyCancellationDependencies) *PolicyCancellationRepository {
	return &PolicyCancellationRepository{db, deps}
}

type policyCancellationTx struct {
	tx   pgx.Tx
	deps PolicyCancellationDependencies
}

func (t policyCancellationTx) CoordinatorFaculty(ctx context.Context, id string) (string, error) {
	return t.deps.LockCoordinatorFaculty(ctx, t.tx, id)
}
func (t policyCancellationTx) Sessions() application.PolicySessions { return t.deps.Sessions(t.tx) }
func (r *PolicyCancellationRepository) Transact(ctx context.Context, work func(application.PolicyCancellationTx) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(policyCancellationTx{tx, r.deps}) })
	if errors.Is(err, identity.ErrForbidden) {
		return domain.ErrForbidden
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503" || pgErr.Code == "40001" || pgErr.Code == "40P01") {
		return scheduling.ErrConflict
	}
	return err
}
