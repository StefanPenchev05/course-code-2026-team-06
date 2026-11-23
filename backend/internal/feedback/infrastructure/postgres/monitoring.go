package postgres

import (
	"context"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	"github.com/jackc/pgx/v5"
)

type monitoring struct{ tx pgx.Tx }

func Monitoring(tx pgx.Tx) application.MonitoringQueries { return &monitoring{tx} }

// Both faculty and tutor statistics use the same population and calculation.
// Faculty averages operate on individual ratings, never rounded tutor averages.
const activityColumns = `count(*),coalesce(sum(s.duration_minutes),0)::float8/60,
	round(avg(f.rating),2)::float8,count(f.rating)`
const activityFrom = ` FROM tutoring_sessions s LEFT JOIN feedback f ON f.session_id=s.id
	WHERE s.status='COMPLETED' `

func (m *monitoring) TutorStatistics(ctx context.Context, faculty string, tutors []string) (map[string]domain.Statistics, error) {
	return statistics(ctx, m.tx, tutors, &faculty)
}
func (m *monitoring) FacultyStatistics(ctx context.Context, faculty string) (domain.Statistics, error) {
	var v domain.Statistics
	err := m.tx.QueryRow(ctx, `SELECT `+activityColumns+activityFrom+`AND s.faculty_id=$1`, faculty).
		Scan(&v.CompletedSessions, &v.HoursTaught, &v.AverageRating, &v.RatingCount)
	return v, err
}
