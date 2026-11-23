package postgres

import (
	"context"
	"time"

	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	"github.com/jackc/pgx/v5"
)

type policySessions struct{ attendanceSessions }

func PolicySessions(tx pgx.Tx) coordination.PolicySessions {
	return policySessions{attendanceSessions{bookingTx{tx: tx}}}
}
func (s policySessions) CancelForPolicy(ctx context.Context, session domain.Session, actor, reason string, now time.Time) (domain.Session, error) {
	return application.CancelForPolicy(ctx, &s.bookingTx, session, actor, reason, now)
}
