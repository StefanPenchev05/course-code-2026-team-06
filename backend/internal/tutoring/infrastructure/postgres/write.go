package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type Dependencies struct {
	Catalog                func(pgx.Tx) catalog.AcademicCatalog
	LockStudent            func(context.Context, pgx.Tx, string) (identity.Principal, error)
	LockCoordinatorFaculty func(context.Context, pgx.Tx, string) (string, error)
}

type Repository struct {
	db   *pgxpool.Pool
	deps Dependencies
}

func New(db *pgxpool.Pool, deps Dependencies) *Repository {
	return &Repository{db: db, deps: deps}
}

func (r *Repository) InTransaction(ctx context.Context, work func(application.Transaction) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		return work(&transaction{tx: tx, reader: r.deps.Catalog(tx), deps: r.deps})
	})
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) {
		switch dbErr.Code {
		case "23505", "23503", "40001", "40P01":
			return domain.ErrConflict
		}
	}
	return err
}

// EligibilityInTransaction binds the existing reconciliation adapter to a caller's
// transaction. Dependencies must also be transaction-bound; bootstrap supplies
// Identity's locker and Catalog's transactional adapter. No pool query is used.
func EligibilityInTransaction(tx pgx.Tx, deps Dependencies) application.EligibilityTransaction {
	return &transaction{tx: tx, reader: deps.Catalog(tx), deps: deps}
}

type transaction struct {
	tx     pgx.Tx
	reader catalog.AcademicCatalog
	deps   Dependencies
}

func (t *transaction) Catalog() catalog.AcademicCatalog { return t.reader }

func (t *transaction) Student(ctx context.Context, id string) (identity.Principal, error) {
	p, err := t.deps.LockStudent(ctx, t.tx, id)
	if errors.Is(err, identity.ErrNotFound) {
		err = domain.ErrNotFound
	}
	return p, err
}

func (t *transaction) CoordinatorFaculty(ctx context.Context, id string) (string, error) {
	faculty, err := t.deps.LockCoordinatorFaculty(ctx, t.tx, id)
	if errors.Is(err, identity.ErrForbidden) {
		err = domain.ErrForbidden
	}
	return faculty, err
}

func (t *transaction) ActiveOffering(ctx context.Context, student, subject string) (bool, error) {
	var active bool
	err := t.tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM tutor_offerings WHERE tutor_id = $1 AND subject_id = $2 AND is_active)`,
		student, subject).Scan(&active)
	return active, err
}

func (t *transaction) CreateApplication(ctx context.Context, student, faculty string, input application.Submission) (string, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		INSERT INTO tutor_applications (student_id, faculty_id, description)
		VALUES ($1,$2,$3) RETURNING id::text`, student, faculty, input.Description).Scan(&id)
	if err != nil {
		return "", err
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO tutor_qualifications (application_id, student_id, subject_id, grade)
		VALUES ($1,$2,$3,$4::text::numeric)`, id, student, input.SubjectID, input.Grade.String())
	if err != nil {
		return "", err
	}
	return id, t.history(ctx, id, student, nil, domain.Pending, "")
}

func (t *transaction) ReviewRecord(ctx context.Context, id, faculty string, lock bool) (application.ReviewRecord, error) {
	var app application.ReviewRecord
	var grade string
	query := `
		SELECT a.id::text, a.student_id::text, q.subject_id::text, q.id::text,
		       a.description, a.status::text, q.grade::text
		FROM tutor_applications a JOIN tutor_qualifications q ON q.application_id = a.id
		WHERE a.id = $1 AND a.faculty_id = $2`
	if lock {
		query += " FOR UPDATE OF a, q"
	}
	err := t.tx.QueryRow(ctx, query, id, faculty).Scan(&app.ID, &app.StudentID, &app.SubjectID,
		&app.QualificationID, &app.Description, &app.Status, &grade)
	if errors.Is(err, pgx.ErrNoRows) {
		return app, domain.ErrNotFound
	}
	if err != nil {
		return app, err
	}
	app.Grade, err = domain.ParseGrade(grade)
	return app, err
}

func (t *transaction) history(ctx context.Context, id, actor string, from any, to domain.Status, reason string) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO application_status_history
			(application_id, changed_by_user_id, actor_kind, from_status, to_status, reason)
		VALUES ($1,$2,'USER',$3,$4,NULLIF($5,''))`, id, actor, from, to, reason)
	return err
}

func (t *transaction) Decide(ctx context.Context, app application.ReviewRecord, actor string, status domain.Status, reason string) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE tutor_applications SET status = $2, reviewed_by_coordinator_id = $3,
		       reviewed_at = statement_timestamp(), rejection_reason = NULLIF($4,'')
		WHERE id = $1 AND status = 'PENDING'`, app.ID, status, actor, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}

	if status == domain.Approved {
		if err := t.approveOffering(ctx, app); err != nil {
			return err
		}
	}
	if err := t.history(ctx, app.ID, actor, domain.Pending, status, reason); err != nil {
		return err
	}
	kind, summary := "APPLICATION_REJECTED", "Tutor application rejected"
	if status == domain.Approved {
		kind, summary = "APPLICATION_APPROVED", "Tutor application approved"
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO notifications (recipient_user_id, type, subject, body, application_id, deduplication_key)
		VALUES ($1,$2,$3,$4,$5,$6)`, app.StudentID, kind, summary,
		summary+". Sign in to Tutti to view the decision.", app.ID, "tutor-application-decision:"+app.ID)
	return err
}

func (t *transaction) approveOffering(ctx context.Context, app application.ReviewRecord) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO tutor_profiles (student_id, description, approved_at)
		VALUES ($1,$2,statement_timestamp()) ON CONFLICT (student_id) DO NOTHING`, app.StudentID, app.Description)
	if err != nil {
		return err
	}
	var enabled bool
	err = t.tx.QueryRow(ctx, `
		SELECT is_active AND approval_ended_at IS NULL FROM tutor_profiles
		WHERE student_id = $1 FOR UPDATE`, app.StudentID).Scan(&enabled)
	if err != nil {
		return err
	}
	if !enabled {
		return domain.ErrConflict
	}
	// Only an explicit new approval may replace the current evidence pointer.
	// The old qualification and its reviewed application remain intact.
	tag, err := t.tx.Exec(ctx, `
		INSERT INTO tutor_offerings (tutor_id, subject_id, qualification_id, description)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (tutor_id, subject_id) DO UPDATE SET
			qualification_id = EXCLUDED.qualification_id, description = EXCLUDED.description,
			is_active = true, deactivated_at = NULL, deactivation_reason = NULL
		WHERE NOT tutor_offerings.is_active`, app.StudentID, app.SubjectID, app.QualificationID, app.Description)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return nil
}

func (t *transaction) ActiveOfferings(ctx context.Context, student string) ([]domain.Offering, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id::text, subject_id::text FROM tutor_offerings
		WHERE tutor_id = $1 AND is_active ORDER BY subject_id FOR UPDATE`, student)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Offering, error) {
		var o domain.Offering
		err := row.Scan(&o.ID, &o.SubjectID)
		return o, err
	})
}

func (t *transaction) Deactivate(ctx context.Context, id string) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE tutor_offerings SET is_active = false, deactivated_at = statement_timestamp(),
			deactivation_reason = 'Subject is outside the tutor''s current academic program'
		WHERE id = $1 AND is_active`, id)
	return err
}
