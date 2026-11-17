package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type Repository struct { db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Repository { return &Repository{db: db} }

func mapError(err error) error {
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) && dbErr.Code == "23505" {
		return domain.ErrConflict
	}
	return err
}

func txValue[T any](ctx context.Context, db *pgxpool.Pool, work func(pgx.Tx) (T, error)) (T, error) {
	var result T
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var err error
		result, err = work(tx)
		return err
	})
	if err != nil {
		var zero T
		return zero, mapError(err)
	}
	return result, nil
}

func programFaculty(ctx context.Context, tx pgx.Tx, program string) (string, error) {
	var faculty string
	err := tx.QueryRow(ctx, "SELECT faculty_id::text FROM programs WHERE id = $1 FOR SHARE", program).Scan(&faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrInvalid
	}
	return faculty, err
}

func createVerification(ctx context.Context, tx pgx.Tx, user, faculty string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO student_verifications (student_id, faculty_id)
		VALUES ($1, $2) RETURNING id::text`, user, faculty).Scan(&id)
	return id, err
}

func (r *Repository) Register(ctx context.Context, d domain.StudentDetails, hash string) (string, error) {
	return txValue(ctx, r.db, func(tx pgx.Tx) (string, error) {
		faculty, err := programFaculty(ctx, tx, d.ProgramID)
		if err != nil {
			return "", err
		}
		var user string
		err = tx.QueryRow(ctx, `INSERT INTO users (name, email, password_hash)
			VALUES ($1, $2, $3) RETURNING id::text`, d.Name, d.Email, hash).Scan(&user)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO students (user_id, student_number, program_id)
			VALUES ($1, $2, $3)`, user, d.StudentNumber, d.ProgramID); err != nil {
			return "", err
		}
		return createVerification(ctx, tx, user, faculty)
	})
}

func (r *Repository) Credentials(ctx context.Context, email string) (id, hash string, err error) {
	err = r.db.QueryRow(ctx, `SELECT id::text, coalesce(password_hash, '')
		FROM users WHERE lower(email) = lower($1)`, email).Scan(&id, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return
}

func (r *Repository) Principal(ctx context.Context, id string) (domain.Principal, error) {
	var p domain.Principal
	err := r.db.QueryRow(ctx, `
		SELECT u.id::text, u.name, u.email, u.is_suspended, u.closed_at IS NOT NULL,
		       s.user_id IS NOT NULL, coalesce(t.is_active AND t.approval_ended_at IS NULL, false),
		       coalesce(c.faculty_id::text, ''), coalesce(v.status::text, ''), coalesce(v.rejection_reason, '')
		  FROM users u
		  LEFT JOIN students s ON s.user_id = u.id
		  LEFT JOIN tutor_profiles t ON t.student_id = s.user_id
		  LEFT JOIN coordinators c ON c.user_id = u.id
		  LEFT JOIN LATERAL (SELECT status, rejection_reason FROM student_verifications
		            WHERE student_id = s.user_id ORDER BY submitted_at DESC, id DESC LIMIT 1) v ON true
		 WHERE u.id = $1`, id).Scan(&p.ID, &p.Name, &p.Email, &p.Suspended, &p.Closed,
		&p.Student, &p.Tutor, &p.CoordinatorFaculty, &p.Verification, &p.RejectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return p, err
}

// Resolve the actor's actual faculty inside each query. A verification outside
// that faculty is invisible even when the caller knows its UUID.
const visibleVerifications = `
	SELECT v.id::text, v.student_id::text, v.faculty_id::text, u.name, u.email,
	       s.student_number, s.program_id::text, v.status::text, v.submitted_at,
	       v.reviewed_at, coalesce(v.reviewed_by_coordinator_id::text, ''),
	       coalesce(v.rejection_reason, '')
	  FROM student_verifications v
	  JOIN students s ON s.user_id = v.student_id
	  JOIN users u ON u.id = s.user_id
	  JOIN coordinators c ON c.faculty_id = v.faculty_id
	  JOIN users actor ON actor.id = c.user_id
	 WHERE c.user_id = $1 AND NOT actor.is_suspended AND actor.closed_at IS NULL`

func scanVerification(row pgx.Row) (domain.Verification, error) {
	var v domain.Verification
	err := row.Scan(&v.ID, &v.StudentID, &v.FacultyID, &v.Name, &v.Email, &v.StudentNumber,
		&v.ProgramID, &v.Status, &v.SubmittedAt, &v.ReviewedAt, &v.ReviewedBy, &v.RejectionReason)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return v, err
}

func (r *Repository) ListPending(ctx context.Context, coordinator string, limit, offset int) ([]domain.Verification, error) {
	rows, err := r.db.Query(ctx, visibleVerifications+
		" AND v.status = 'PENDING' ORDER BY v.submitted_at, v.id LIMIT $2 OFFSET $3", coordinator, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Verification, error) {
		return scanVerification(row)
	})
}

func (r *Repository) Verification(ctx context.Context, coordinator, id string) (domain.Verification, error) {
	return scanVerification(r.db.QueryRow(ctx, visibleVerifications+" AND v.id = $2", coordinator, id))
}

func (r *Repository) Review(ctx context.Context, coordinator, id string, decision domain.Status, reason string) error {
	_, err := txValue(ctx, r.db, func(tx pgx.Tx) (struct{}, error) {
		var faculty string
		err := tx.QueryRow(ctx, `
			SELECT c.faculty_id::text FROM coordinators c JOIN users u ON u.id = c.user_id
			WHERE c.user_id = $1 AND NOT u.is_suspended AND u.closed_at IS NULL
			FOR SHARE OF c, u`, coordinator).Scan(&faculty)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrForbidden
		}
		if err != nil {
			return struct{}{}, err
		}
		var status domain.Status
		err = tx.QueryRow(ctx, `SELECT status::text FROM student_verifications
			WHERE id = $1 AND faculty_id = $2 FOR UPDATE`, id, faculty).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrNotFound
		}
		if err != nil {
			return struct{}{}, err
		}
		if status != domain.Pending {
			return struct{}{}, domain.ErrConflict
		}
		_, err = tx.Exec(ctx, `UPDATE student_verifications
			SET status = $2, reviewed_by_coordinator_id = $3, reviewed_at = statement_timestamp(),
			    rejection_reason = CASE WHEN $2 = 'REJECTED' THEN $4::text END
			WHERE id = $1`, id, decision, coordinator, reason)
		return struct{}{}, err
	})
	return err
}

func (r *Repository) Resubmit(ctx context.Context, user string, d domain.StudentDetails) (string, error) {
	return txValue(ctx, r.db, func(tx pgx.Tx) (string, error) {
		var id string
		err := tx.QueryRow(ctx, `SELECT u.id::text FROM users u JOIN students s ON s.user_id = u.id
			WHERE u.id = $1 AND NOT u.is_suspended AND u.closed_at IS NULL FOR UPDATE OF u, s`, user).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrForbidden
		}
		if err != nil {
			return "", err
		}
		var eligible bool
		err = tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM student_verifications WHERE student_id = $1 AND status = 'REJECTED'
		) AND NOT EXISTS (
			SELECT 1 FROM student_verifications WHERE student_id = $1 AND status IN ('PENDING', 'APPROVED')
		)`, id).Scan(&eligible)
		if err != nil {
			return "", err
		}
		if !eligible {
			return "", domain.ErrConflict
		}
		faculty, err := programFaculty(ctx, tx, d.ProgramID)
		if err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, "UPDATE users SET name = $2, email = $3 WHERE id = $1", id, d.Name, d.Email); err != nil {
			return "", err
		}
		if _, err := tx.Exec(ctx, "UPDATE students SET student_number = $2, program_id = $3 WHERE user_id = $1",
			id, d.StudentNumber, d.ProgramID); err != nil {
			return "", err
		}
		return createVerification(ctx, tx, id, faculty)
	})
}

func (r *Repository) SaveActivation(ctx context.Context, coordinator string, digest []byte, ttl time.Duration) (string, error) {
	return txValue(ctx, r.db, func(tx pgx.Tx) (string, error) {
		var email string
		err := tx.QueryRow(ctx, `SELECT u.email FROM coordinators c JOIN users u ON u.id = c.user_id
			WHERE c.user_id = $1 AND u.password_hash IS NULL AND NOT u.is_suspended
			      AND u.closed_at IS NULL FOR UPDATE OF u`, coordinator).Scan(&email)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrNotFound
		}
		if err != nil {
			return "", err
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_activation_tokens
			(coordinator_id, token_hash, created_at, expires_at) VALUES
			($1, $2, statement_timestamp(), statement_timestamp() + $3::bigint * interval '1 microsecond')`,
			coordinator, digest, ttl.Microseconds())
		return email, err
	})
}

func (r *Repository) ConsumeActivation(ctx context.Context, digest []byte, hash string) error {
	_, err := txValue(ctx, r.db, func(tx pgx.Tx) (struct{}, error) {
		var user string
		err := tx.QueryRow(ctx, `SELECT u.id::text FROM account_activation_tokens t
			JOIN coordinators c ON c.user_id = t.coordinator_id JOIN users u ON u.id = c.user_id
			WHERE t.token_hash = $1 AND u.password_hash IS NULL AND NOT u.is_suspended
			      AND u.closed_at IS NULL FOR UPDATE OF u`, digest).Scan(&user)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrUnauthenticated
		}
		if err != nil {
			return struct{}{}, err
		}
		tag, err := tx.Exec(ctx, `UPDATE account_activation_tokens SET used_at = statement_timestamp()
			WHERE token_hash = $1 AND coordinator_id = $2 AND used_at IS NULL
			      AND expires_at > statement_timestamp()`, digest, user)
		if err != nil {
			return struct{}{}, err
		}
		if tag.RowsAffected() != 1 {
			return struct{}{}, domain.ErrUnauthenticated
		}
		_, err = tx.Exec(ctx, "UPDATE users SET password_hash = $2 WHERE id = $1", user, hash)
		return struct{}{}, err
	})
	return err
}
