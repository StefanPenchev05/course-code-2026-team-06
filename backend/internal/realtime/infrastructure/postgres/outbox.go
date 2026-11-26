package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Outbox struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) *Outbox { return &Outbox{db} }
func (s *Outbox) Check(ctx context.Context) error {
	_, err := s.db.Exec(ctx, `SELECT id,event_type,entity_id,resource_kind,producer_xid,claim_token,lease_until,dispatched_at FROM realtime_events LIMIT 0`)
	return err
}

// Only the partial pending index is scanned; dispatched retention is measured
// by relation size rather than a full historical row count on every sample.
func (s *Outbox) Health(ctx context.Context) (observability.Outbox, error) {
	var h observability.Outbox
	err := s.db.QueryRow(ctx, `SELECT count(resource_kind),coalesce(greatest(extract(epoch FROM statement_timestamp()-min(occurred_at)),0),0)::float8,
		coalesce(sum(greatest(attempt_count-1,0)),0)::bigint,count(*) FILTER(WHERE lease_until<=statement_timestamp()),
		count(*) FILTER(WHERE last_error='fanout_failed'),pg_total_relation_size('realtime_events')
		FROM realtime_events WHERE dispatched_at IS NULL`).Scan(&h.Pending, &h.OldestSeconds, &h.Retries, &h.ExpiredLeases, &h.FanoutFailed, &h.Bytes)
	return h, err
}

func (s *Outbox) Claim(ctx context.Context, lease time.Duration) (app.Claim, bool, error) {
	var c app.Claim
	var at time.Time
	if lease < time.Second || lease > time.Minute {
		return c, false, errors.New("invalid realtime lease")
	}
	err := s.db.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM realtime_events WHERE dispatched_at IS NULL AND next_attempt_at<=statement_timestamp()
		AND(lease_until IS NULL OR lease_until<=statement_timestamp())
		ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
	) UPDATE realtime_events e SET attempt_count=least(e.attempt_count::bigint+1,2147483647)::integer,
		claim_token=gen_random_uuid(),lease_until=statement_timestamp()+$1::bigint*interval '1 microsecond'
		FROM candidate c WHERE e.id=c.id
		RETURNING e.id::text,e.event_type,e.entity_id::text,e.occurred_at,e.claim_token::text,e.attempt_count`, lease.Microseconds()).
		Scan(&c.ID, &c.Type, &c.EntityID, &at, &c.Token, &c.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	c.Version = 1
	if c.Type == app.AccountAccessChanged {
		c.EntityID = ""
	}
	c.OccurredAt = at.UTC().Format(time.RFC3339Nano)
	return c, err == nil, err
}
func (s *Outbox) Get(ctx context.Context, id string) (app.Event, error) {
	var e app.Event
	var at time.Time
	err := s.db.QueryRow(ctx, `SELECT id::text,event_type,entity_id::text,occurred_at FROM realtime_events WHERE id=$1`, id).Scan(&e.ID, &e.Type, &e.EntityID, &at)
	e.Version = 1
	if e.Type == app.AccountAccessChanged {
		e.EntityID = ""
	}
	e.OccurredAt = at.UTC().Format(time.RFC3339Nano)
	return e, err
}

// Route is a private lookup, separate from the validated public Event projection.
func (s *Outbox) Route(ctx context.Context, id string) (target, kind string, err error) {
	err = s.db.QueryRow(ctx, `SELECT entity_id::text,resource_kind FROM realtime_events WHERE id=$1`, id).Scan(&target, &kind)
	return
}
func (s *Outbox) Dispatched(ctx context.Context, c app.Claim) error {
	tag, err := s.db.Exec(ctx, `UPDATE realtime_events SET dispatched_at=statement_timestamp(),claim_token=NULL,lease_until=NULL,last_error=NULL
		WHERE id=$1 AND claim_token=$2 AND dispatched_at IS NULL AND lease_until>statement_timestamp()`, c.ID, c.Token)
	if err == nil && tag.RowsAffected() != 1 {
		return app.ErrClaimLost
	}
	return err
}
func (s *Outbox) Retry(ctx context.Context, c app.Claim, delay time.Duration) error {
	if delay < 0 || delay > 5*time.Minute {
		return errors.New("invalid realtime retry delay")
	}
	tag, err := s.db.Exec(ctx, `UPDATE realtime_events SET next_attempt_at=statement_timestamp()+$3::bigint*interval '1 microsecond',
		claim_token=NULL,lease_until=NULL,last_error='fanout_failed'
		WHERE id=$1 AND claim_token=$2 AND dispatched_at IS NULL AND lease_until>statement_timestamp()`, c.ID, c.Token, delay.Microseconds())
	if err == nil && tag.RowsAffected() != 1 {
		return app.ErrClaimLost
	}
	return err
}
