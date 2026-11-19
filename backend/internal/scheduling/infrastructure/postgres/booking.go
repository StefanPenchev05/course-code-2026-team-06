package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
)

type BookingDependencies struct {
	LockStudent StudentLocker
	Catalog     func(pgx.Tx) catalog.AcademicCatalog
	Offerings   func(pgx.Tx) tutoring.BookingOfferings
	Discipline  func(pgx.Tx) coordination.Discipline
}

type BookingRepository struct {
	db   *pgxpool.Pool
	deps BookingDependencies
}

func NewBooking(db *pgxpool.Pool, deps BookingDependencies) *BookingRepository {
	return &BookingRepository{db: db, deps: deps}
}

type bookingTx struct {
	tx   pgx.Tx
	deps BookingDependencies
}

func (r *BookingRepository) TransactBooking(ctx context.Context, work func(application.BookingTransaction) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(&bookingTx{tx, r.deps}) })
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23P01":
			return domain.ErrBookingOverlap
		case "23503", "23505", "40001", "40P01":
			return domain.ErrConflict
		}
	}
	return err
}

func (t *bookingTx) Student(ctx context.Context, id string) (identity.Principal, error) {
	p, err := t.deps.LockStudent(ctx, t.tx, id)
	if errors.Is(err, identity.ErrNotFound) {
		err = domain.ErrNotFound
	}
	return p, err
}

func (t *bookingTx) Catalog() catalog.AcademicCatalog     { return t.deps.Catalog(t.tx) }
func (t *bookingTx) Offerings() tutoring.BookingOfferings { return t.deps.Offerings(t.tx) }
func (t *bookingTx) Discipline() coordination.Discipline  { return t.deps.Discipline(t.tx) }

func (t *bookingTx) Source(ctx context.Context, id, tutor string) (domain.Window, domain.Semester, error) {
	var w domain.Window
	var semester domain.Semester
	var ruleID *string
	err := t.tx.QueryRow(ctx, `
		SELECT starts_at,ends_at,allowed_mode::text,rule_id::text
		FROM availability_slots WHERE id=$1 AND tutor_id=$2 FOR SHARE`, id, tutor).
		Scan(&w.StartsAt, &w.EndsAt, &w.Mode, &ruleID)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, semester, domain.ErrNotFound
	}
	if err != nil {
		return w, semester, err
	}
	if ruleID != nil {
		err = t.tx.QueryRow(ctx, `
			SELECT s.id::text,s.starts_at,s.ends_at
			FROM semesters s JOIN availability_rules r ON r.semester_id=s.id
			WHERE r.id=$1 FOR SHARE OF s`, *ruleID).Scan(&semester.ID, &semester.StartsAt, &semester.EndsAt)
	} else {
		err = t.tx.QueryRow(ctx, `
			SELECT id::text,starts_at,ends_at FROM semesters
			WHERE starts_at<=$1 AND ends_at>=$2 ORDER BY starts_at DESC,id LIMIT 1 FOR SHARE`,
			w.StartsAt, w.EndsAt).Scan(&semester.ID, &semester.StartsAt, &semester.EndsAt)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = fmt.Errorf("%w: the slot has no configured semester", domain.ErrBookingInvalid)
	}
	return w, semester, err
}

func (t *bookingTx) Policy(ctx context.Context) (string, error) {
	var id string
	const lookup = `SELECT id::text FROM session_policies ORDER BY id LIMIT 1 FOR SHARE`
	err := t.tx.QueryRow(ctx, lookup).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	// All current policy columns are fixed by checks. Serialize initialization
	// when the developer has not seeded the default row.
	if _, err := t.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(74208204)`); err != nil {
		return "", err
	}
	err = t.tx.QueryRow(ctx, lookup).Scan(&id)
	if !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	err = t.tx.QueryRow(ctx, `INSERT INTO session_policies DEFAULT VALUES RETURNING id::text`).Scan(&id)
	return id, err
}

const sessionColumns = `
	s.id::text,s.student_id::text,s.tutor_id::text,o.subject_id::text,subject.name,s.offering_id::text,
	coalesce(s.availability_slot_id::text,''),s.semester_id::text,s.starts_at,s.ends_at,
	s.duration_minutes,s.mode::text,s.purpose::text,s.description,s.status::text,s.requested_at,s.expires_at,
	s.responded_at,coalesce(s.decline_reason,''),coalesce(s.meeting_link,''),coalesce(s.meeting_room,''),
	s.cancelled_at,coalesce(s.cancelled_by_user_id::text,''),coalesce(s.cancellation_reason,''),s.is_late_cancellation, s.faculty_id::text, s.completed_at, coalesce(s.completed_by_tutor_id::text,'')`

const sessionView = `SELECT ` + sessionColumns + `
	FROM tutoring_sessions s JOIN tutor_offerings o ON o.id=s.offering_id
	JOIN subjects subject ON subject.id=o.subject_id`

func scanSession(row pgx.Row) (domain.Session, error) {
	var s domain.Session
	err := row.Scan(&s.ID, &s.StudentID, &s.TutorID, &s.SubjectID, &s.SubjectName, &s.OfferingID, &s.SlotID, &s.SemesterID,
		&s.StartsAt, &s.EndsAt, &s.Minutes, &s.Mode, &s.Purpose, &s.Description, &s.Status, &s.RequestedAt, &s.ExpiresAt,
		&s.RespondedAt, &s.DeclineReason, &s.MeetingLink, &s.MeetingRoom, &s.CancelledAt, &s.CancelledBy, &s.CancellationReason, &s.Late, &s.FacultyID, &s.CompletedAt, &s.CompletedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, domain.ErrNotFound
	}
	s.StartsAt, s.EndsAt, s.RequestedAt, s.ExpiresAt = s.StartsAt.UTC(), s.EndsAt.UTC(), s.RequestedAt.UTC(), s.ExpiresAt.UTC()
	for _, field := range []*time.Time{s.RespondedAt, s.CancelledAt, s.CompletedAt} {
		if field != nil {
			*field = field.UTC()
		}
	}
	return s, err
}

func (t *bookingTx) InsertSession(ctx context.Context, s domain.Session, policy, faculty string) (domain.Session, error) {
	return scanSession(t.tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO tutoring_sessions (student_id,tutor_id,offering_id,availability_slot_id,
				policy_id,semester_id,faculty_id,starts_at,ends_at,duration_minutes,mode,purpose,
				description,requested_at,expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *
		)
		SELECT `+sessionColumns+` FROM inserted s JOIN tutor_offerings o ON o.id=s.offering_id
		JOIN subjects subject ON subject.id=o.subject_id`, s.StudentID, s.TutorID, s.OfferingID, s.SlotID, policy, s.SemesterID,
		faculty, s.StartsAt, s.EndsAt, s.Minutes, s.Mode, s.Purpose, s.Description, s.RequestedAt, s.ExpiresAt))
}

const ownedSession = ` WHERE s.id=$1 AND (s.tutor_id=$2 OR (NOT $3 AND s.student_id=$2))`

func (t *bookingTx) ParticipantSession(ctx context.Context, id, actor string, tutorOnly, lock bool) (domain.Session, error) {
	query := sessionView + ownedSession
	if lock {
		query += ` FOR UPDATE OF s`
	}
	return scanSession(t.tx.QueryRow(ctx, query, id, actor, tutorOnly))
}

func (r *BookingRepository) ParticipantSession(ctx context.Context, id, actor string) (domain.Session, error) {
	return scanSession(r.db.QueryRow(ctx, sessionView+ownedSession, id, actor, false))
}

func collectSessions(rows pgx.Rows, err error) ([]domain.Session, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Session, error) { return scanSession(row) })
}

func (r *BookingRepository) ListSessions(ctx context.Context, actor string, status domain.Status, tutorOnly bool, p application.Page) ([]domain.Session, error) {
	return collectSessions(r.db.Query(ctx, sessionView+`
		WHERE (s.tutor_id=$1 OR (NOT $2 AND s.student_id=$1))
		  AND (NULLIF($3::text,'') IS NULL OR s.status=NULLIF($3::text,'')::session_status)
		ORDER BY s.starts_at,s.id LIMIT $4 OFFSET $5`, actor, tutorOnly, status, p.Limit, p.Offset))
}

func (t *bookingTx) DueRequests(ctx context.Context, now time.Time, limit int) ([]domain.Session, error) {
	return collectSessions(t.tx.Query(ctx, sessionView+`
		WHERE s.status='REQUESTED' AND s.expires_at<=$1
		ORDER BY s.expires_at,s.id LIMIT $2 FOR UPDATE OF s SKIP LOCKED`, now, limit))
}

func (t *bookingTx) UpdateSession(ctx context.Context, s domain.Session, previous domain.Status) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE tutoring_sessions SET status=$2,responded_at=$3,decline_reason=NULLIF($4,''),
			meeting_link=NULLIF($5,''),meeting_room=NULLIF($6,''),cancelled_at=$7,
			cancelled_by_user_id=NULLIF($8::text,'')::uuid,cancellation_reason=NULLIF($9,''),is_late_cancellation=$10,
            completed_at=$12,completed_by_tutor_id=NULLIF($13::text,'')::uuid
		WHERE id=$1 AND status=$11`, s.ID, s.Status, s.RespondedAt, s.DeclineReason, s.MeetingLink, s.MeetingRoom,
		s.CancelledAt, s.CancelledBy, s.CancellationReason, s.Late, previous, s.CompletedAt, s.CompletedBy)
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrBookingState
	}
	return err
}

func (t *bookingTx) RecordEvent(ctx context.Context, event application.Event) error {
	actor := "USER"
	if event.Actor == "" {
		actor = "SYSTEM"
	}
	_, err := t.tx.Exec(ctx, `
		INSERT INTO session_status_history (session_id,from_status,to_status,changed_at,actor_kind,changed_by_user_id,reason,cancellation_origin)
		VALUES ($1,NULLIF($2::text,'')::session_status,$3,$4,$5,NULLIF($6::text,'')::uuid,NULLIF($7,''),NULLIF($8,''))`,
		event.SessionID, event.From, event.To, event.At, actor, event.Actor, event.Reason, event.CancellationOrigin)
	if err != nil {
		return err
	}
	var kind, summary string
	switch event.To {
	case domain.Completed, domain.NoShow:
		return nil
	case domain.Requested:
		kind, summary = "NEW_REQUEST", "New tutoring request"
	case domain.Accepted:
		kind, summary = "REQUEST_ACCEPTED", "Tutoring request accepted"
	case domain.Declined:
		kind, summary = "REQUEST_DECLINED", "Tutoring request declined"
	case domain.Expired:
		kind, summary = "REQUEST_EXPIRED", "Tutoring request expired"
	case domain.Cancelled:
		kind, summary = "SESSION_CANCELLED", "Tutoring session cancelled"
	default:
		return domain.ErrBookingState
	}
	_, err = t.tx.Exec(ctx, `
		INSERT INTO notifications (recipient_user_id,type,subject,body,session_id,deduplication_key)
		SELECT recipient::uuid,$2::text::notification_type,$3,$4,$5::text::uuid,$6
		FROM unnest($1::text[]) AS emails(recipient)`, event.Recipients, kind, summary,
		summary+". Sign in to Tutti for details.", event.SessionID, "session:"+event.SessionID+":"+kind)
	return err
}

func (r *BookingRepository) UpcomingTutorSessions(ctx context.Context, tutor string, now time.Time, p application.Page) ([]domain.Session, error) {
	return collectSessions(r.db.Query(ctx, sessionView+`
		WHERE s.tutor_id=$1 AND s.status='ACCEPTED' AND s.starts_at >= $2
		ORDER BY s.starts_at,s.id LIMIT $3 OFFSET $4`, tutor, now, p.Limit, p.Offset))
}
