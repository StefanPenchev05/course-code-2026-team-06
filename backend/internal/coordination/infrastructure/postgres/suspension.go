package postgres

import (
	"context"
	"errors"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	identityapp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SuspensionDependencies struct {
	Accounts func(pgx.Tx) identityapp.SuspensionAccounts
	Catalog  func(pgx.Tx) catalog.AcademicCatalog
	Sessions func(pgx.Tx) application.SuspensionSessions
}
type SuspensionRepository struct {
	db   *pgxpool.Pool
	deps SuspensionDependencies
}

func NewSuspension(db *pgxpool.Pool, deps SuspensionDependencies) *SuspensionRepository {
	return &SuspensionRepository{db, deps}
}

type suspensionTx struct {
	tx   pgx.Tx
	deps SuspensionDependencies
}

func (t suspensionTx) Accounts() identityapp.SuspensionAccounts { return t.deps.Accounts(t.tx) }
func (t suspensionTx) Catalog() catalog.AcademicCatalog         { return t.deps.Catalog(t.tx) }
func (t suspensionTx) Sessions() application.SuspensionSessions { return t.deps.Sessions(t.tx) }

func (r *SuspensionRepository) Transact(ctx context.Context, work func(application.SuspensionTx) error) error {
	for attempt := 0; ; attempt++ {
		err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(suspensionTx{tx, r.deps}) })
		var dbErr *pgconn.PgError
		if !errors.As(err, &dbErr) {
			return err
		}
		if dbErr.Code == "55P03" && attempt < 9 {
			// No locks or writes survive the aborted attempt. Bounded retries
			// tolerate brief contention without a cross-workflow lock inversion.
			timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		switch dbErr.Code {
		case "55P03", "40P01", "40001", "23503", "23505":
			return identity.ErrConflict
		}
		return err
	}
}
