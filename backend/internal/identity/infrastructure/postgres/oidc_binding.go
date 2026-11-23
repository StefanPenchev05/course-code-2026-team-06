package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/jackc/pgx/v5"
)

var _ application.OIDCIdentityReader = (*Repository)(nil)
var _ application.OIDCIdentityWriter = (*Repository)(nil)

func (r *Repository) LookupOIDCIdentity(ctx context.Context, identity domain.VerifiedIdentity) (domain.Principal, error) {
	var principal domain.Principal
	err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var userID string
		err := tx.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject=$2`, identity.Issuer, identity.Subject).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrOIDCIdentityUnlinked
		}
		if err != nil {
			return err
		}
		principal, err = readPrincipal(ctx, tx, userID)
		return err
	})
	if err != nil {
		return domain.Principal{}, err
	}
	return principal, nil
}

func (r *Repository) BindOIDCIdentity(ctx context.Context, userID string, identity domain.VerifiedIdentity) error {
	_, err := txValue(ctx, r.db, func(tx pgx.Tx) (struct{}, error) {
		var locked string
		err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 FOR NO KEY UPDATE`, userID).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrNotFound
		}
		if err != nil {
			return struct{}{}, err
		}
		principal, err := readPrincipal(ctx, tx, userID)
		if err != nil {
			return struct{}{}, err
		}
		if !principal.Active() || (!principal.Student && !principal.Coordinator()) {
			return struct{}{}, domain.ErrForbidden
		}
		tag, err := tx.Exec(ctx, `INSERT INTO oidc_identities(user_id,issuer,subject) VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, userID, identity.Issuer, identity.Subject)
		if err != nil {
			return struct{}{}, err
		}
		if tag.RowsAffected() == 1 {
			return struct{}{}, nil
		}
		// ON CONFLICT never rewrites or reassigns an existing binding. An exact
		// duplicate succeeds; either unique key conflicting otherwise is rejected.
		var existing string
		err = tx.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject=$2`, identity.Issuer, identity.Subject).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			return struct{}{}, domain.ErrConflict
		}
		if err != nil {
			return struct{}{}, err
		}
		if existing != locked {
			return struct{}{}, domain.ErrConflict
		}
		return struct{}{}, nil
	})
	return err
}
