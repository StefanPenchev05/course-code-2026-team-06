package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type attendanceSessions struct{ bookingTx }

func AttendanceSessions(tx pgx.Tx) coordination.AttendanceSessions {
	return attendanceSessions{bookingTx{tx: tx}}
}

func (a attendanceSessions) LockParticipantSession(ctx context.Context, id, actor string) (domain.Session, error) {
	return a.ParticipantSession(ctx, id, actor, false, true)
}

func (a attendanceSessions) LockFacultySession(ctx context.Context, id, faculty string) (domain.Session, error) {
	return scanSession(a.tx.QueryRow(ctx, sessionView+` WHERE s.id=$1 AND s.faculty_id=$2 FOR UPDATE OF s`, id, faculty))
}

func (a attendanceSessions) CorrectNoShow(ctx context.Context, s domain.Session, actor, note string, now time.Time) error {
	return application.CorrectNoShow(ctx, &a.bookingTx, s, actor, note, now)
}
