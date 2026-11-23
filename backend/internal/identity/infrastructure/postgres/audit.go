package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type suspensionHistory struct{ tx pgx.Tx }

func SuspensionHistory(tx pgx.Tx) application.SuspensionHistory { return suspensionHistory{tx} }
func (r suspensionHistory) ListSuspensionHistory(ctx context.Context, user, faculty string, limit, offset int) ([]domain.SuspensionDecision, error) {
	rows, err := r.tx.Query(ctx, `SELECT id::text,user_id::text,faculty_id::text,changed_by_coordinator_id::text,
 was_suspended,is_suspended,reason,changed_at FROM user_suspension_history
 WHERE user_id=$1 AND faculty_id=$2 ORDER BY changed_at DESC,id DESC LIMIT $3 OFFSET $4`, user, faculty, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.SuspensionDecision, error) {
		var d domain.SuspensionDecision
		err := row.Scan(&d.ID, &d.UserID, &d.FacultyID, &d.ActorID, &d.WasSuspended, &d.IsSuspended, &d.Reason, &d.At)
		d.At = d.At.UTC()
		return d, err
	})
}
