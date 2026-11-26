package postgres

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListAvailableSemesters(ctx context.Context, now time.Time) ([]domain.Semester, error) {
	rows, err := r.db.Query(ctx, `SELECT id::text,name,starts_at,ends_at
		FROM semesters WHERE ends_at > $1 ORDER BY starts_at,id`, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Semester, error) {
		var s domain.Semester
		err := row.Scan(&s.ID, &s.Name, &s.StartsAt, &s.EndsAt)
		s.StartsAt, s.EndsAt = s.StartsAt.UTC(), s.EndsAt.UTC()
		return s, err
	})
}
