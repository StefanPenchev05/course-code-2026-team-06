package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// LockStudent serializes student mutations with program changes and suspension.
// The caller owns the transaction and decides whether an active account is required.
func LockStudent(ctx context.Context, tx pgx.Tx, id string) (domain.Principal, error) {
	var locked string
	err := tx.QueryRow(ctx, `SELECT u.id::text FROM users u WHERE u.id = $1 FOR NO KEY UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Principal{}, err
	}
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM students WHERE user_id = $1 FOR SHARE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Principal{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Principal{}, err
	}
	return readPrincipal(ctx, tx, id)
}

// LockCoordinatorFaculty resolves and locks the current coordinator assignment.
func LockCoordinatorFaculty(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var faculty string
	err := tx.QueryRow(ctx, `
		SELECT c.faculty_id::text FROM coordinators c JOIN users u ON u.id = c.user_id
		WHERE c.user_id = $1 AND NOT u.is_suspended AND u.closed_at IS NULL
		FOR SHARE OF c, u`, id).Scan(&faculty)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrForbidden
	}
	return faculty, err
}
