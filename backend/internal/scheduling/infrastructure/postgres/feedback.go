package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// LockStudentSession gives the Feedback workflow the same session row lock
// used by outcome corrections, restricted to the learner.
func LockStudentSession(ctx context.Context, tx pgx.Tx, id, student string) (domain.Session, error) {
	return scanSession(tx.QueryRow(ctx, sessionView+" WHERE s.id=$1 AND s.student_id=$2 FOR UPDATE OF s", id, student))
}
