package postgres

import (
	"context"
	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/jackc/pgx/v5"
)

type calendarQueries struct{ tx pgx.Tx }

func Calendar(tx pgx.Tx) catalog.CalendarDiscipline { return &calendarQueries{tx: tx} }
func (q *calendarQueries) Inspect(ctx context.Context, id string) (map[string]int64, error) {
	var violations, flags, attendance int64
	err := q.tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM user_violations WHERE semester_id=$1),
 (SELECT count(*) FROM user_semester_flags WHERE semester_id=$1),
 (SELECT count(*) FROM attendance_reports a JOIN tutoring_sessions s ON s.id=a.session_id WHERE s.semester_id=$1)`, id).Scan(&violations, &flags, &attendance)
	return map[string]int64{"violations_retained": violations, "flags_retained": flags, "attendance_reports_retained": attendance}, err
}
