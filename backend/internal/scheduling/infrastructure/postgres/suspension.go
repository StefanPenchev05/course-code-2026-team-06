package postgres

import (
	"context"
	"time"

	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	"github.com/jackc/pgx/v5"
)

type suspensionSessions struct{ bookingTx }

func SuspensionSessions(tx pgx.Tx) coordination.SuspensionSessions {
	return suspensionSessions{bookingTx{tx: tx}}
}

func (s suspensionSessions) LockFutureSessions(ctx context.Context, target string, at time.Time, after string, limit int) ([]domain.Session, error) {
	return collectSessions(s.tx.Query(ctx, sessionView+`
		WHERE (s.student_id=$1 OR s.tutor_id=$1) AND s.starts_at >= $2
		AND s.status IN ('REQUESTED','ACCEPTED') AND ($3='' OR s.id > NULLIF($3,'')::uuid)
		ORDER BY s.id LIMIT $4 FOR UPDATE OF s`, target, at, after, limit))
}

func (s suspensionSessions) CancelFuture(ctx context.Context, target, actor, decision string, at time.Time) (int, error) {
	return application.CancelForSuspension(ctx, &s, target, actor, decision, at)
}
