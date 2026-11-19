package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
	"github.com/jackc/pgx/v5"
)

type monitoring struct{ tx pgx.Tx }

func Monitoring(tx pgx.Tx) application.MonitoringQueries { return &monitoring{tx} }

const monitoringFrom = ` FROM tutor_profiles tp JOIN users u ON u.id=tp.student_id
	JOIN students st ON st.user_id=tp.student_id JOIN programs p ON p.id=st.program_id `

// Reuse discovery's eligibility predicates; EXISTS avoids multiplying profiles
// with multiple offerings. Inactive approved profiles remain visible for oversight.
const monitoringActive = `coalesce(` + activeTutor + ` AND EXISTS (
	SELECT 1 FROM tutor_offerings o JOIN tutor_qualifications q ON q.id=o.qualification_id
	JOIN tutor_applications a ON a.id=q.application_id AND a.status='APPROVED'
	WHERE o.tutor_id=tp.student_id AND ` + eligibleOffering + `),false)`
const monitoringColumns = `tp.student_id::text,u.name,p.id::text,p.name,p.faculty_id::text,tp.approved_at,` + monitoringActive

func scanMonitoredTutor(row pgx.Row) (application.MonitoredTutor, error) {
	var t application.MonitoredTutor
	err := row.Scan(&t.ID, &t.Name, &t.ProgramID, &t.ProgramName, &t.FacultyID, &t.ApprovedAt, &t.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	t.ApprovedAt = t.ApprovedAt.UTC()
	return t, err
}

func (m *monitoring) Tutors(ctx context.Context, faculty string, f application.MonitoringFilter) ([]application.MonitoredTutor, error) {
	rows, err := m.tx.Query(ctx, `SELECT `+monitoringColumns+monitoringFrom+`
		WHERE p.faculty_id=$1 AND ($2::uuid IS NULL OR p.id=$2)
		AND ($3::uuid IS NULL OR EXISTS (SELECT 1 FROM tutor_offerings o
			JOIN tutor_qualifications q ON q.id=o.qualification_id
			JOIN tutor_applications a ON a.id=q.application_id AND a.status='APPROVED'
			WHERE o.tutor_id=tp.student_id AND o.subject_id=$3))
		ORDER BY u.name,tp.student_id LIMIT $4 OFFSET $5`, faculty, optionalID(f.ProgramID), optionalID(f.SubjectID), f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (application.MonitoredTutor, error) { return scanMonitoredTutor(row) })
}

func optionalID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

func (m *monitoring) Tutor(ctx context.Context, faculty, id string) (application.MonitoredTutor, error) {
	return scanMonitoredTutor(m.tx.QueryRow(ctx, `SELECT `+monitoringColumns+monitoringFrom+`
		WHERE p.faculty_id=$1 AND tp.student_id=$2`, faculty, id))
}

func (m *monitoring) Counts(ctx context.Context, faculty string) (application.TutorCounts, error) {
	var v application.TutorCounts
	err := m.tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER (WHERE `+monitoringActive+`)`+monitoringFrom+`WHERE p.faculty_id=$1`, faculty).
		Scan(&v.ApprovedTutors, &v.ActiveTutors)
	return v, err
}

func (m *monitoring) Offerings(ctx context.Context, faculty, tutor string, p domain.Page) ([]domain.Offering, error) {
	rows, err := m.tx.Query(ctx, `SELECT o.id::text,s.id::text,s.name,o.description,`+eligibleOffering+tutorRows+`
		WHERE tp.student_id=$1 AND p.faculty_id=$2 ORDER BY s.name,o.id LIMIT $3 OFFSET $4`, tutor, faculty, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Offering, error) {
		var v domain.Offering
		err := row.Scan(&v.ID, &v.SubjectID, &v.SubjectName, &v.Description, &v.Active)
		return v, err
	})
}
