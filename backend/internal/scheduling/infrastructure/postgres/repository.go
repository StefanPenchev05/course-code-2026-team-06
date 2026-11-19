package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type StudentLocker func(context.Context, pgx.Tx, string) (identity.Principal, error)

type Repository struct {
	db          *pgxpool.Pool
	lockStudent StudentLocker
}

func New(db *pgxpool.Pool, lockStudent StudentLocker) *Repository {
	return &Repository{db: db, lockStudent: lockStudent}
}

func (r *Repository) InTransaction(ctx context.Context, tutor string, work func(application.Transaction, identity.Principal) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		p, err := r.lockStudent(ctx, tx, tutor)
		if errors.Is(err, identity.ErrNotFound) {
			return domain.ErrForbidden
		}
		if err != nil {
			return err
		}
		return work(&transaction{tx}, p)
	})
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) {
		switch dbErr.Code {
		case "23P01":
			return domain.ErrOverlap
		case "23505", "23503", "40P01", "40001":
			return domain.ErrConflict
		}
	}
	return err
}

type transaction struct{ tx pgx.Tx }

func (t *transaction) Semester(ctx context.Context, id string) (domain.Semester, error) {
	var s domain.Semester
	err := t.tx.QueryRow(ctx, `SELECT id::text,starts_at,ends_at FROM semesters WHERE id=$1 FOR SHARE`, id).
		Scan(&s.ID, &s.StartsAt, &s.EndsAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = fmt.Errorf("%w: unknown semester", domain.ErrInvalid)
	}
	return s, err
}

func (t *transaction) RequireSemesterFor(ctx context.Context, w domain.Window) error {
	var id string
	err := t.tx.QueryRow(ctx, `
		SELECT id::text FROM semesters WHERE starts_at <= $1 AND ends_at >= $2
		ORDER BY starts_at DESC,id LIMIT 1 FOR SHARE`, w.StartsAt, w.EndsAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: window must fit inside a configured semester", domain.ErrInvalid)
	}
	return err
}

const ruleColumns = `id::text,semester_id::text,iso_weekday,local_start_time::text,local_end_time::text,time_zone,allowed_mode::text`

func scanRule(row pgx.Row) (domain.Rule, error) {
	var v domain.Rule
	err := row.Scan(&v.ID, &v.SemesterID, &v.Weekday, &v.Start, &v.End, &v.Zone, &v.Mode)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return v, err
}

func (r *Repository) ListRules(ctx context.Context, tutor string, p application.Page) ([]domain.Rule, error) {
	rows, err := r.db.Query(ctx, `SELECT `+ruleColumns+`
		FROM availability_rules WHERE tutor_id=$1
		ORDER BY semester_id,iso_weekday,local_start_time,id LIMIT $2 OFFSET $3`, tutor, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Rule, error) { return scanRule(row) })
}

func (t *transaction) CreateRule(ctx context.Context, tutor string, v domain.Rule) (string, error) {
	var id string
	err := t.tx.QueryRow(ctx, `
		INSERT INTO availability_rules (tutor_id,semester_id,iso_weekday,local_start_time,local_end_time,time_zone,allowed_mode)
		VALUES ($1,$2,$3,$4::text::time,$5::text::time,$6,$7) RETURNING id::text`,
		tutor, v.SemesterID, v.Weekday, v.Start, v.End, v.Zone, v.Mode).Scan(&id)
	return id, err
}

func (t *transaction) Rule(ctx context.Context, tutor, id string) (domain.Rule, error) {
	return scanRule(t.tx.QueryRow(ctx, `SELECT `+ruleColumns+`
		FROM availability_rules WHERE tutor_id=$1 AND id=$2 FOR UPDATE`, tutor, id))
}

func (t *transaction) UpdateRule(ctx context.Context, v domain.Rule) error {
	_, err := t.tx.Exec(ctx, `
		UPDATE availability_rules SET iso_weekday=$2,local_start_time=$3::text::time,
			local_end_time=$4::text::time,time_zone=$5,allowed_mode=$6 WHERE id=$1`,
		v.ID, v.Weekday, v.Start, v.End, v.Zone, v.Mode)
	return err
}

func (t *transaction) DeleteRule(ctx context.Context, id string) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM availability_rules WHERE id=$1`, id)
	return err
}

func (t *transaction) ExceptionDates(ctx context.Context, id string) (map[string]bool, error) {
	rows, err := t.tx.Query(ctx, `SELECT to_char(local_date,'YYYY-MM-DD') FROM availability_exceptions WHERE rule_id=$1`, id)
	if err != nil {
		return nil, err
	}
	dates, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(dates))
	for _, date := range dates {
		set[date] = true
	}
	return set, nil
}

func (r *Repository) ListExceptions(ctx context.Context, tutor, id string, p application.Page) ([]domain.Exception, error) {
	rows, err := r.db.Query(ctx, `
		SELECT to_char(e.local_date,'YYYY-MM-DD'),coalesce(e.reason,'')
		FROM availability_rules r
		LEFT JOIN LATERAL (
			SELECT local_date,reason FROM availability_exceptions WHERE rule_id=r.id
			ORDER BY local_date LIMIT $3 OFFSET $4
		) e ON true
		WHERE r.tutor_id=$1 AND r.id=$2 ORDER BY e.local_date`, tutor, id, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found bool
	items := []domain.Exception{}
	for rows.Next() {
		found = true
		var date *string
		var reason string
		if err := rows.Scan(&date, &reason); err != nil {
			return nil, err
		}
		if date != nil {
			items = append(items, domain.Exception{Date: *date, Reason: reason})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, domain.ErrNotFound
	}
	return items, nil
}

func (t *transaction) AddException(ctx context.Context, id, date string) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO availability_exceptions (rule_id,local_date) VALUES ($1,$2::text::date)
		ON CONFLICT (rule_id,local_date) DO NOTHING`, id, date)
	return err
}

func (t *transaction) RemoveException(ctx context.Context, id, date string) error {
	tag, err := t.tx.Exec(ctx, `DELETE FROM availability_exceptions WHERE rule_id=$1 AND local_date=$2::text::date`, id, date)
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return err
}

const slotColumns = `id::text,tutor_id::text,starts_at,ends_at,allowed_mode::text,coalesce(rule_id::text,'')`

func scanSlot(row pgx.Row) (domain.Slot, error) {
	var v domain.Slot
	err := row.Scan(&v.ID, &v.TutorID, &v.StartsAt, &v.EndsAt, &v.Mode, &v.RuleID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	v.StartsAt, v.EndsAt = v.StartsAt.UTC(), v.EndsAt.UTC()
	v.Recurring = v.RuleID != ""
	return v, err
}

func collectSlots(rows pgx.Rows, err error) ([]domain.Slot, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Slot, error) { return scanSlot(row) })
}

func (t *transaction) FutureRuleSlots(ctx context.Context, id string, now time.Time) ([]domain.Slot, error) {
	return collectSlots(t.tx.Query(ctx, `SELECT `+slotColumns+`
		FROM availability_slots WHERE rule_id=$1 AND starts_at>$2 ORDER BY starts_at,id`, id, now))
}

func (t *transaction) Slot(ctx context.Context, tutor, id string) (domain.Slot, error) {
	return scanSlot(t.tx.QueryRow(ctx, `SELECT `+slotColumns+`
		FROM availability_slots WHERE tutor_id=$1 AND id=$2`, tutor, id))
}

func (t *transaction) CreateSlot(ctx context.Context, tutor string, w domain.Window) (domain.Slot, error) {
	return scanSlot(t.tx.QueryRow(ctx, `
		INSERT INTO availability_slots (tutor_id,starts_at,ends_at,allowed_mode) VALUES ($1,$2,$3,$4)
		RETURNING `+slotColumns, tutor, w.StartsAt, w.EndsAt, w.Mode))
}

func (t *transaction) CreateRuleSlots(ctx context.Context, tutor, id string, windows []domain.Window) error {
	if len(windows) == 0 {
		return nil
	}
	starts, ends := make([]time.Time, len(windows)), make([]time.Time, len(windows))
	for i, w := range windows {
		starts[i], ends[i] = w.StartsAt, w.EndsAt
	}
	_, err := t.tx.Exec(ctx, `
		INSERT INTO availability_slots (tutor_id,rule_id,starts_at,ends_at,allowed_mode)
		SELECT $1,$2,w.starts_at,w.ends_at,$3
		FROM unnest($4::timestamptz[],$5::timestamptz[]) AS w(starts_at,ends_at)
		ORDER BY w.starts_at`, tutor, id, windows[0].Mode, starts, ends)
	return err
}

func (t *transaction) DeleteSlots(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := t.tx.Exec(ctx, `DELETE FROM availability_slots WHERE id=ANY($1::text[]::uuid[])`, ids)
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) && dbErr.Code == "23514" && dbErr.TableName == "tutoring_sessions" {
		// SET NULL is legal for historical records. The existing session check
		// rejects it for REQUESTED and ACCEPTED, including concurrent inserts.
		return domain.ErrReserved
	}
	return err
}

func (r *Repository) ListSlots(ctx context.Context, tutor string, freeOnly bool, f application.SlotFilter) ([]domain.Slot, error) {
	var to *time.Time
	if !f.To.IsZero() {
		to = &f.To
	}
	if !freeOnly {
		return collectSlots(r.db.Query(ctx, `SELECT `+slotColumns+`
			FROM availability_slots WHERE tutor_id=$1 AND ends_at>$2 AND ($3::timestamptz IS NULL OR starts_at<$3)
			ORDER BY starts_at,id LIMIT $4 OFFSET $5`, tutor, f.From, to, f.Limit, f.Offset))
	}
	// The participant table has both roles and only active reservations.
	// A single statement clips the windows and subtracts every overlapping block.
	return collectSlots(r.db.Query(ctx, `
		WITH windows AS (
			SELECT id,tutor_id,rule_id,allowed_mode,
				tstzrange(greatest(starts_at,$2),least(ends_at,$3::timestamptz),'[)') AS span
			FROM availability_slots
			WHERE tutor_id=$1 AND ends_at>$2 AND ($3::timestamptz IS NULL OR starts_at<$3)
		)
		SELECT w.id::text,w.tutor_id::text,lower(free.span),upper(free.span),w.allowed_mode::text,coalesce(w.rule_id::text,'')
		FROM windows w
		CROSS JOIN LATERAL unnest(tstzmultirange(w.span) - coalesce((
			SELECT range_agg(tstzrange(b.starts_at,b.ends_at,'[)')) FROM session_participant_blocks b
			WHERE b.user_id=w.tutor_id AND tstzrange(b.starts_at,b.ends_at,'[)') && w.span
		),'{}'::tstzmultirange)) AS free(span)
		ORDER BY lower(free.span),w.id LIMIT $4 OFFSET $5`, tutor, f.From, to, f.Limit, f.Offset))
}
