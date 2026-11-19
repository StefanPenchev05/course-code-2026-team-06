package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/application"
)

type Store struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) Check(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `SELECT attempt_count,next_attempt_at,lease_until,claim_token,last_error FROM notifications LIMIT 0`)
	return err
}

func (s *Store) Claim(ctx context.Context, lease time.Duration, maxAttempts int) (application.Claim, bool, error) {
	var c application.Claim
	err := s.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id FROM notifications
			WHERE email_status='PENDING' AND next_attempt_at<=statement_timestamp()
			  AND (lease_until IS NULL OR lease_until<=statement_timestamp()) AND attempt_count<$2
			ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE notifications n
			SET attempt_count=attempt_count+1,claim_token=gen_random_uuid(),
			    lease_until=statement_timestamp()+$1::bigint*interval '1 microsecond'
			FROM candidate c WHERE n.id=c.id
			RETURNING n.id,n.recipient_user_id,n.subject,n.body,n.claim_token,n.attempt_count,n.lease_until
		)
		SELECT c.id::text,u.email,c.subject,c.body,c.claim_token::text,c.attempt_count,c.lease_until
		FROM claimed c JOIN users u ON u.id=c.recipient_user_id`, lease.Microseconds(), maxAttempts).
		Scan(&c.ID, &c.Recipient, &c.Subject, &c.Body, &c.Token, &c.Attempt, &c.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

func (s *Store) Sent(ctx context.Context, c application.Claim) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE notifications SET email_status='SENT',email_sent_at=statement_timestamp(),email_failed_at=NULL,
		    lease_until=NULL,claim_token=NULL,last_error=NULL
		WHERE id=$1 AND claim_token=$2 AND email_status='PENDING'
		  AND lease_until>statement_timestamp()`, c.ID, c.Token)
	if err == nil && tag.RowsAffected() != 1 {
		return application.ErrClaimLost
	}
	return err
}

func (s *Store) Failed(ctx context.Context, c application.Claim, code application.FailureCode, retry time.Duration) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE notifications SET email_status=CASE WHEN $4::bigint>0 THEN 'PENDING'::email_status ELSE 'FAILED'::email_status END,
		    email_failed_at=CASE WHEN $4>0 THEN NULL ELSE statement_timestamp() END,
		    next_attempt_at=statement_timestamp()+$4*interval '1 microsecond',
		    lease_until=NULL,claim_token=NULL,last_error=$3
		WHERE id=$1 AND claim_token=$2 AND email_status='PENDING'
		  AND lease_until>statement_timestamp()`, c.ID, c.Token, code, retry.Microseconds())
	if err == nil && tag.RowsAffected() != 1 {
		return application.ErrClaimLost
	}
	return err
}

func (s *Store) FailExhausted(ctx context.Context, maxAttempts, limit int) (int, error) {
	tag, err := s.db.Exec(ctx, `
		WITH exhausted AS (
			SELECT id FROM notifications
			WHERE email_status='PENDING' AND attempt_count >= $1
			  AND (lease_until IS NULL OR lease_until<=statement_timestamp())
			ORDER BY next_attempt_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
		)
		UPDATE notifications n SET email_status='FAILED',email_failed_at=statement_timestamp(),
		    last_error=CASE WHEN lease_until IS NULL THEN coalesce(last_error,'attempt_limit') ELSE 'lease_expired' END,
		    lease_until=NULL,claim_token=NULL
		FROM exhausted e WHERE n.id=e.id`, maxAttempts, limit)
	return int(tag.RowsAffected()), err
}
