package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RealtimeQueries struct{ db *pgxpool.Pool }

func NewRealtime(db *pgxpool.Pool) *RealtimeQueries { return &RealtimeQueries{db} }

var _ application.RealtimeQueries = (*RealtimeQueries)(nil)

func (r *RealtimeQueries) CaseAudience(ctx context.Context, kind, id string) (coordinator, faculty string, err error) {
	// Only fixed queries: resource_kind is private metadata, never SQL or client authority.
	var query string
	switch kind {
	case "issue":
		query = `SELECT faculty_id FROM issue_reports WHERE id=$1`
	case "attendance":
		query = `SELECT s.faculty_id FROM attendance_reports a JOIN tutoring_sessions s ON s.id=a.session_id WHERE a.id=$1`
	case "violation":
		query = `SELECT home_faculty_id AS faculty_id FROM user_violations WHERE id=$1`
	case "flag":
		query = `SELECT faculty_id FROM user_semester_flags WHERE id=$1`
	case "policy":
		query = `SELECT s.faculty_id FROM session_status_history h JOIN tutoring_sessions s ON s.id=h.session_id WHERE h.id=$1 AND h.cancellation_origin='COORDINATOR_POLICY'`
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
