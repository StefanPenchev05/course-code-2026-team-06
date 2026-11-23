package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityapp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type FlaggedUsersDependencies struct {
	Faculty  func(context.Context, pgx.Tx, string) (string, error)
	Accounts func(pgx.Tx) identityapp.AccountStates
}

type FlaggedUsersRepository struct {
	db   *pgxpool.Pool
	deps FlaggedUsersDependencies
}

func NewFlaggedUsers(db *pgxpool.Pool, deps FlaggedUsersDependencies) *FlaggedUsersRepository {
	return &FlaggedUsersRepository{db, deps}
}

func (r *FlaggedUsersRepository) ListFlaggedUsers(ctx context.Context, actor string, limit, offset int) ([]domain.FlaggedUser, error) {
	if limit < 1 || limit > 101 || offset < 0 || offset > 100000 {
		return nil, domain.ErrInvalid
	}
	result := []domain.FlaggedUser{}
	err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		faculty, err := r.deps.Faculty(ctx, tx, actor)
		if errors.Is(err, identity.ErrForbidden) {
			return domain.ErrForbidden
		}
		if err != nil {
			return err
		}
		// Page stored flag ownership first. Count only the selected user/semester
		// pairs using the existing violation index; do not duplicate rows by joining
		// every incident. Later incidents still count after a program move, while
		// individual evidence remains subject to its own historical faculty scope.
		rows, err := tx.Query(ctx, `
 WITH page AS (
   SELECT id,user_id,semester_id,flagged_at FROM user_semester_flags
   WHERE faculty_id=$1 ORDER BY flagged_at DESC,id DESC LIMIT $2 OFFSET $3
 )
 SELECT p.id::text,p.user_id::text,p.semester_id::text,p.flagged_at,
   (SELECT count(*) FROM user_violations v WHERE v.user_id=p.user_id AND v.semester_id=p.semester_id)
 FROM page p ORDER BY p.flagged_at DESC,p.id DESC`, faculty, limit, offset)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.FlaggedUser, error) {
			var f domain.FlaggedUser
			err := row.Scan(&f.FlagID, &f.UserID, &f.SemesterID, &f.FlaggedAt, &f.StrikeCount)
			f.FlaggedAt = f.FlaggedAt.UTC()
			return f, err
		})
		if err != nil || len(result) == 0 {
			return err
		}
		ids := make([]string, 0, len(result))
		for _, f := range result {
			ids = append(ids, f.UserID)
		}
		states, err := r.deps.Accounts(tx).SuspensionStates(ctx, ids)
		if err != nil {
			return err
		}
		for i := range result {
			state, found := states[result[i].UserID]
			if !found { // FK guarantees this; never silently invent an active state.
				return errors.New("flag account state unavailable")
			}
			result[i].IsSuspended = state
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
