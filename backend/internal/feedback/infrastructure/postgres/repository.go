package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type Dependencies struct {
	Student func(context.Context, pgx.Tx, string) (identity.Principal, error)
	Session func(context.Context, pgx.Tx, string, string) (scheduling.Session, error)
}

type Repository struct {
	db   *pgxpool.Pool
	deps Dependencies
}

func New(db *pgxpool.Pool, deps Dependencies) *Repository { return &Repository{db: db, deps: deps} }

type writeTx struct {
	tx   pgx.Tx
	deps Dependencies
}

func (r *Repository) Transact(ctx context.Context, work func(application.WriteTx) error) error {
	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(writeTx{tx, r.deps}) })
}

func (t writeTx) Student(ctx context.Context, id string) (identity.Principal, error) {
	p, err := t.deps.Student(ctx, t.tx, id)
	if errors.Is(err, identity.ErrNotFound) {
		err = domain.ErrForbidden
	}
	return p, err
}
func (t writeTx) LockStudentSession(ctx context.Context, id, actor string) (scheduling.Session, error) {
	s, err := t.deps.Session(ctx, t.tx, id, actor)
	if errors.Is(err, scheduling.ErrNotFound) {
		err = domain.ErrNotFound
	}
	return s, err
}
func (t writeTx) Insert(ctx context.Context, s scheduling.Session, i domain.Input) (domain.Entry, error) {
	e := domain.Entry{SessionID: s.ID, SessionStatus: string(s.Status), SubjectID: s.SubjectID, SubjectName: s.SubjectName,
		StartsAt: s.StartsAt.UTC(), Rating: i.Rating, Comment: i.Comment}
	err := t.tx.QueryRow(ctx, `INSERT INTO feedback (session_id,rating,comment) VALUES ($1,$2,NULLIF($3,''))
		RETURNING created_at`, s.ID, i.Rating, i.Comment).Scan(&e.CreatedAt)
	var dbErr *pgconn.PgError
	if errors.As(err, &dbErr) && dbErr.Code == "23505" && dbErr.ConstraintName == "feedback_pkey" {
		return domain.Entry{}, domain.ErrConflict
	}
	e.CreatedAt = e.CreatedAt.UTC()
	return e, err
}

type reader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

const feedbackView = `
	SELECT f.session_id::text,s.status::text,sub.id::text,sub.name,s.starts_at,f.rating,coalesce(f.comment,''),f.created_at
	FROM feedback f JOIN tutoring_sessions s ON s.id=f.session_id
	JOIN tutor_offerings o ON o.id=s.offering_id JOIN subjects sub ON sub.id=o.subject_id`

func scanEntry(row pgx.Row) (domain.Entry, error) {
	var e domain.Entry
	err := row.Scan(&e.SessionID, &e.SessionStatus, &e.SubjectID, &e.SubjectName, &e.StartsAt, &e.Rating, &e.Comment, &e.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	e.StartsAt, e.CreatedAt = e.StartsAt.UTC(), e.CreatedAt.UTC()
	return e, err
}

func (r *Repository) Get(ctx context.Context, id, actor string) (domain.Entry, error) {
	return scanEntry(r.db.QueryRow(ctx, feedbackView+`
		JOIN users actor ON actor.id=$2 AND NOT actor.is_suspended AND actor.closed_at IS NULL
		WHERE f.session_id=$1 AND (
			(s.status='COMPLETED' AND $2 IN (s.student_id,s.tutor_id))
			OR EXISTS (SELECT 1 FROM coordinators c WHERE c.user_id=$2 AND c.faculty_id=s.faculty_id)
		)`, id, actor))
}

func (r *Repository) List(ctx context.Context, tutor string, p application.Page) ([]domain.Entry, error) {
	return list(ctx, r.db, tutor, nil, p)
}
func list(ctx context.Context, db reader, tutor string, faculty *string, p application.Page) ([]domain.Entry, error) {
	rows, err := db.Query(ctx, feedbackView+`
		WHERE s.tutor_id=$1 AND s.status='COMPLETED' AND ($2::uuid IS NULL OR s.faculty_id=$2)
		ORDER BY f.created_at DESC,f.session_id DESC LIMIT $3 OFFSET $4`, tutor, faculty, p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Entry, error) { return scanEntry(row) })
}

func (r *Repository) Statistics(ctx context.Context, tutors []string) (map[string]domain.Statistics, error) {
	return statistics(ctx, r.db, tutors, nil)
}
func statistics(ctx context.Context, db reader, tutors []string, faculty *string) (map[string]domain.Statistics, error) {
	out := make(map[string]domain.Statistics, len(tutors))
	if len(tutors) == 0 {
		return out, nil
	}
	rows, err := db.Query(ctx, `
		SELECT s.tutor_id::text,count(*),sum(s.duration_minutes)::float8/60,
		       round(avg(f.rating),2)::float8,count(f.rating)
		FROM tutoring_sessions s LEFT JOIN feedback f ON f.session_id=s.id
		WHERE s.status='COMPLETED' AND s.tutor_id=ANY($1::text[]::uuid[])
		  AND ($2::uuid IS NULL OR s.faculty_id=$2)
		GROUP BY s.tutor_id`, tutors, faculty)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var v domain.Statistics
		if err := rows.Scan(&id, &v.CompletedSessions, &v.HoursTaught, &v.AverageRating, &v.RatingCount); err != nil {
			return nil, err
		}
		out[id] = v
	}
	return out, rows.Err()
}

func (r *Repository) CoordinatorFeedback(ctx context.Context, actor, tutor string, p application.Page) (application.Review, error) {
	var result application.Review
	err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var faculty string
		err := tx.QueryRow(ctx, `
			SELECT c.faculty_id::text FROM coordinators c
			JOIN users actor ON actor.id=c.user_id AND NOT actor.is_suspended AND actor.closed_at IS NULL
			JOIN programs p ON p.faculty_id=c.faculty_id
			JOIN students st ON st.program_id=p.id JOIN tutor_profiles tp ON tp.student_id=st.user_id
			WHERE c.user_id=$1 AND tp.student_id=$2`, actor, tutor).Scan(&faculty)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		entries, err := list(ctx, tx, tutor, &faculty, p)
		if err != nil {
			return err
		}
		stats, err := statistics(ctx, tx, []string{tutor}, &faculty)
		if err != nil {
			return err
		}
		result = application.Review{TutorRatingSummary: stats[tutor].TutorRatingSummary, Entries: entries}
		return nil
	})
	return result, err
}
