package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

const applicationRows = `
	SELECT a.id::text, a.student_id::text, s.id::text, s.name, a.status::text,
	       a.submitted_at, a.reviewed_at, coalesce(a.rejection_reason, '')`

const applicationsJoin = `
	FROM tutor_applications a
	JOIN tutor_qualifications q ON q.application_id = a.id
	JOIN subjects s ON s.id = q.subject_id
	JOIN users actor ON actor.id = $1 AND NOT actor.is_suspended AND actor.closed_at IS NULL
	LEFT JOIN coordinators c ON c.user_id = actor.id AND c.faculty_id = a.faculty_id`

func scanApp(row pgx.Row) (domain.Application, error) {
	var a domain.Application
	err := row.Scan(&a.ID, &a.StudentID, &a.SubjectID, &a.SubjectName, &a.Status, &a.SubmittedAt, &a.ReviewedAt, &a.RejectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return a, err
}

func (r *Repository) Applications(ctx context.Context, actor string, coordinator bool, status domain.Status, page domain.Page) ([]domain.Application, error) {
	rows, err := r.db.Query(ctx, applicationRows+applicationsJoin+`
		WHERE ($2 AND c.user_id IS NOT NULL AND a.status::text = $3)
		   OR (NOT $2 AND a.student_id = actor.id)
		ORDER BY a.submitted_at DESC, a.id DESC LIMIT $4 OFFSET $5`,
		actor, coordinator, status, page.Limit, page.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Application, error) { return scanApp(row) })
}

func (r *Repository) OwnApplication(ctx context.Context, actor, id string) (domain.Application, error) {
	return scanApp(r.db.QueryRow(ctx, applicationRows+applicationsJoin+`
		WHERE a.id = $2 AND (a.student_id = actor.id OR c.user_id IS NOT NULL)`, actor, id))
}

func (r *Repository) ReviewDetail(ctx context.Context, actor, id string) (domain.ReviewDetail, error) {
	var d domain.ReviewDetail
	err := r.db.QueryRow(ctx, applicationRows+", q.grade::text, a.description "+applicationsJoin+`
		WHERE a.id = $2 AND c.user_id IS NOT NULL`, actor, id).Scan(&d.ID, &d.StudentID, &d.SubjectID,
		&d.SubjectName, &d.Status, &d.SubmittedAt, &d.ReviewedAt, &d.RejectionReason, &d.Grade, &d.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return d, err
}

// These set-based read projections enforce both participants' live eligibility in
// a single snapshot. Write workflows use the transaction-bound module ports.
const tutorRows = `
	FROM tutor_profiles tp JOIN users u ON u.id = tp.student_id
	JOIN students st ON st.user_id = tp.student_id
	JOIN programs p ON p.id = st.program_id
	JOIN tutor_offerings o ON o.tutor_id = tp.student_id
	JOIN subjects s ON s.id = o.subject_id
	JOIN tutor_qualifications q ON q.id = o.qualification_id
	JOIN tutor_applications a ON a.id = q.application_id AND a.status = 'APPROVED'`

const activeTutor = `
	(tp.is_active AND tp.approval_ended_at IS NULL AND NOT u.is_suspended AND u.closed_at IS NULL
	 AND (SELECT v.status FROM student_verifications v WHERE v.student_id = st.user_id
	      ORDER BY v.submitted_at DESC, v.id DESC LIMIT 1) = 'APPROVED')`

const eligibleOffering = `
	(o.is_active AND EXISTS (SELECT 1 FROM program_subjects ps
	                         WHERE ps.program_id = st.program_id AND ps.subject_id = o.subject_id))`

const offeringColumns = `o.id::text, s.id::text, s.name, o.description`

func (r *Repository) Search(ctx context.Context, actor string, f domain.Search) ([]domain.Match, error) {
	rows, err := r.db.Query(ctx, `
		SELECT tp.student_id::text, u.name, tp.description, `+offeringColumns+tutorRows+`
		WHERE `+activeTutor+" AND "+eligibleOffering+`
		  AND o.subject_id = $1
		  AND (NULLIF($2::text,'') IS NULL OR p.faculty_id = NULLIF($2::text,'')::uuid)
		  AND (NULLIF($3::text,'') IS NULL OR p.id = NULLIF($3::text,'')::uuid)
		  AND (NULLIF($4::text,'') IS NULL OR p.level = NULLIF($4::text,'')::degree_level)
		  AND EXISTS (SELECT 1 FROM students requester
		              JOIN program_subjects ps ON ps.program_id = requester.program_id
		              JOIN users ru ON ru.id = requester.user_id
		              WHERE requester.user_id = $5 AND ps.subject_id = o.subject_id
		                AND NOT ru.is_suspended AND ru.closed_at IS NULL
		                AND (SELECT v.status FROM student_verifications v WHERE v.student_id = requester.user_id
		                     ORDER BY v.submitted_at DESC, v.id DESC LIMIT 1) = 'APPROVED')
		ORDER BY u.name, tp.student_id LIMIT $6 OFFSET $7`,
		f.SubjectID, f.FacultyID, f.ProgramID, f.Level, actor, f.Limit, f.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Match, error) {
		m := domain.Match{Tutor: domain.Tutor{Active: true}, Offering: domain.Offering{Active: true}}
		err := row.Scan(&m.ID, &m.Name, &m.Description,
			&m.Offering.ID, &m.Offering.SubjectID, &m.Offering.SubjectName, &m.Offering.Description)
		return m, err
	})
}

func (r *Repository) Profile(ctx context.Context, tutor string, own bool) (domain.Profile, error) {
	rows, err := r.db.Query(ctx, `
		SELECT tp.student_id::text, u.name, tp.description, coalesce(`+activeTutor+`, false),
		       `+offeringColumns+", coalesce("+activeTutor+" AND "+eligibleOffering+", false) "+tutorRows+`
		WHERE tp.student_id = $1 AND ($2 OR (`+activeTutor+" AND "+eligibleOffering+`))
		ORDER BY s.name, s.id`, tutor, own)
	if err != nil {
		return domain.Profile{}, err
	}
	defer rows.Close()
	profile := domain.Profile{Offerings: []domain.Offering{}}
	for rows.Next() {
		var o domain.Offering
		err := rows.Scan(&profile.ID, &profile.Name, &profile.Description, &profile.Active,
			&o.ID, &o.SubjectID, &o.SubjectName, &o.Description, &o.Active)
		if err != nil {
			return domain.Profile{}, err
		}
		profile.Offerings = append(profile.Offerings, o)
	}
	if err := rows.Err(); err != nil {
		return domain.Profile{}, err
	}
	if profile.ID == "" {
		return domain.Profile{}, domain.ErrNotFound
	}
	return profile, nil
}

func (r *Repository) ApprovedOffering(ctx context.Context, tutor, subject string) (domain.Offering, error) {
	o := domain.Offering{Active: true}
	err := r.db.QueryRow(ctx, "SELECT "+offeringColumns+tutorRows+`
		WHERE tp.student_id = $1 AND o.subject_id = $2 AND `+activeTutor+" AND "+eligibleOffering,
		tutor, subject).Scan(&o.ID, &o.SubjectID, &o.SubjectName, &o.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return o, err
}
