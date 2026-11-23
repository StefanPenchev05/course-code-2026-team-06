package postgres

import (
	"context"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CalendarDependencies struct {
	Scheduling func(pgx.Tx) application.CalendarScheduling
	Discipline func(pgx.Tx) application.CalendarDiscipline
}
type CalendarRepository struct {
	pool *pgxpool.Pool
	deps CalendarDependencies
}

func NewCalendar(pool *pgxpool.Pool, deps CalendarDependencies) *CalendarRepository {
	return &CalendarRepository{pool: pool, deps: deps}
}

func (r *CalendarRepository) CalendarTransaction(ctx context.Context, readOnly bool, work func(application.CalendarTx) error) error {
	opts := pgx.TxOptions{IsoLevel: pgx.ReadCommitted}
	if readOnly {
		opts = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	}
	tx, err := r.pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx)) // also release locks after command cancellation
	if !readOnly {
		// Calendar entries are few and maintenance is rare. This serializes even
		// direct INSERT writers, while allowing Scheduling's FOR SHARE readers.
		// READ COMMITTED ensures List sees the preceding writer after this lock.
		if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "LOCK TABLE semesters IN SHARE ROW EXCLUSIVE MODE"); err != nil {
			return err
		}
	}
	if err = work(&calendarTx{tx: tx, deps: r.deps}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type calendarTx struct {
	tx   pgx.Tx
	deps CalendarDependencies
}

func (t *calendarTx) Scheduling() application.CalendarScheduling { return t.deps.Scheduling(t.tx) }
func (t *calendarTx) Discipline() application.CalendarDiscipline { return t.deps.Discipline(t.tx) }
func (t *calendarTx) List(ctx context.Context) ([]domain.Semester, error) {
	rows, err := t.tx.Query(ctx, "SELECT id::text,name,starts_at,ends_at FROM semesters ORDER BY starts_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Semester, 0)
	for rows.Next() {
		var s domain.Semester
		if err := rows.Scan(&s.ID, &s.Name, &s.StartsAt, &s.EndsAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
func (t *calendarTx) Insert(ctx context.Context, s domain.Semester) error {
	_, err := t.tx.Exec(ctx, "INSERT INTO semesters(id,name,starts_at,ends_at) VALUES($1,$2,$3,$4)", s.ID, s.Name, s.StartsAt, s.EndsAt)
	return err
}
