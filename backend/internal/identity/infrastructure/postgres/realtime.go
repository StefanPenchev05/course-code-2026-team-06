package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/jackc/pgx/v5"
)

var _ application.RealtimeQueries = (*Repository)(nil)

func (r *Repository) AccountReviewAudience(ctx context.Context, kind, id string) (coordinator, faculty string, err error) {
	var query string
	switch kind {
	case "verification":
		query = `SELECT faculty_id FROM student_verifications WHERE id=$1`
	case "suspension":
		// Administration uses current home faculty. A historical decision ID must
		// also remain readable under its stored audit authority; do not transfer it
		// to a new home faculty after an academic move.
		query = `SELECT h.faculty_id FROM user_suspension_history h JOIN students s ON s.user_id=h.user_id
			JOIN programs p ON p.id=s.program_id AND p.faculty_id=h.faculty_id WHERE h.id=$1`
	default:
		return "", "", nil
	}
	err = r.db.QueryRow(ctx, `SELECT coalesce(c.user_id::text,''),a.faculty_id::text FROM (`+query+`) a
		LEFT JOIN coordinators c ON c.faculty_id=a.faculty_id`, id).Scan(&coordinator, &faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
