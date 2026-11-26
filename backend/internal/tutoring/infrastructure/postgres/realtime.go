package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/jackc/pgx/v5"
)

var _ application.RealtimeQueries = (*Repository)(nil)

func (r *Repository) ApplicationAudience(ctx context.Context, id string) (owner, coordinator, faculty string, err error) {
	err = r.db.QueryRow(ctx, `SELECT a.student_id::text,coalesce(c.user_id::text,''),a.faculty_id::text
		FROM tutor_applications a LEFT JOIN coordinators c ON c.faculty_id=a.faculty_id WHERE a.id=$1`, id).
		Scan(&owner, &coordinator, &faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

// Monitoring follows the tutor's current home faculty, unlike application review.
func (r *Repository) ActivityAudience(ctx context.Context, id string) (coordinator, faculty string, err error) {
	err = r.db.QueryRow(ctx, `SELECT coalesce(c.user_id::text,''),p.faculty_id::text
		FROM tutor_profiles t JOIN students s ON s.user_id=t.student_id JOIN programs p ON p.id=s.program_id
		LEFT JOIN coordinators c ON c.faculty_id=p.faculty_id WHERE t.student_id=$1`, id).Scan(&coordinator, &faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}
