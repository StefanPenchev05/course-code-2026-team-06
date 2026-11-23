package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type accountStates struct{ tx pgx.Tx }

func AccountStates(tx pgx.Tx) application.AccountStates { return accountStates{tx} }

func (r accountStates) SuspensionStates(ctx context.Context, users []string) (map[string]bool, error) {
	if len(users) > 101 { // One bounded page plus the has_more probe.
		return nil, domain.ErrInvalid
	}
	result := make(map[string]bool, len(users))
	if len(users) == 0 {
		return result, nil
	}
	rows, err := r.tx.Query(ctx, `SELECT id::text,is_suspended FROM users WHERE id=ANY($1::uuid[])`, users)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var suspended bool
		if err := rows.Scan(&id, &suspended); err != nil {
			return nil, err
		}
		result[id] = suspended
	}
	return result, rows.Err()
}
