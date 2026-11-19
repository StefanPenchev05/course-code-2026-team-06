package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type AttendanceDependencies struct {
	LockStudent            func(context.Context, pgx.Tx, string) (identity.Principal, error)
	LockCoordinatorFaculty func(context.Context, pgx.Tx, string) (string, error)
	HomeFaculty            func(context.Context, pgx.Tx, string) (string, error)
	Sessions               func(pgx.Tx) application.AttendanceSessions
}

type AttendanceRepository struct {
	db   *pgxpool.Pool
	deps AttendanceDependencies
}

func NewAttendance(db *pgxpool.Pool, deps AttendanceDependencies) *AttendanceRepository {
	return &AttendanceRepository{db: db, deps: deps}
}

type attendanceTx struct {
	tx   pgx.Tx
	deps AttendanceDependencies
}

func (r *AttendanceRepository) Transact(ctx context.Context, work func(application.AttendanceTx) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(attendanceTx{tx, r.deps}) })
	switch {
	case errors.Is(err, identity.ErrForbidden):
		return domain.ErrForbidden
	case errors.Is(err, identity.ErrNotFound), errors.Is(err, scheduling.ErrNotFound):
		return domain.ErrNotFound
	case errors.Is(err, scheduling.ErrBookingState):
		return domain.ErrConflict
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "40P01" || pgErr.Code == "40001") {
		return domain.ErrConflict
	}
	return err
}

func (t attendanceTx) Student(ctx context.Context, id string) (identity.Principal, error) {
	return t.deps.LockStudent(ctx, t.tx, id)
}
func (t attendanceTx) CoordinatorFaculty(ctx context.Context, id string) (string, error) {
	return t.deps.LockCoordinatorFaculty(ctx, t.tx, id)
}
func (t attendanceTx) HomeFaculty(ctx context.Context, program string) (string, error) {
	return t.deps.HomeFaculty(ctx, t.tx, program)
}
func (t attendanceTx) Sessions() application.AttendanceSessions { return t.deps.Sessions(t.tx) }

const reportColumns = `a.id::text,a.session_id::text,a.reporter_user_id::text,a.absent_user_id::text,
	a.status::text,coalesce(a.details,''),a.reported_at,a.reviewed_at,
	coalesce(a.reviewed_by_coordinator_id::text,''),coalesce(a.resolution_note,'')`

const coordinatorReports = `SELECT ` + reportColumns + `
	FROM attendance_reports a JOIN tutoring_sessions s ON s.id=a.session_id
	JOIN coordinators c ON c.faculty_id=s.faculty_id
	JOIN users actor ON actor.id=c.user_id AND NOT actor.is_suspended AND actor.closed_at IS NULL
	WHERE c.user_id=$1`

func scanReport(row pgx.Row) (domain.Report, error) {
	var r domain.Report
	err := row.Scan(&r.ID, &r.SessionID, &r.ReporterID, &r.AbsentID, &r.Status, &r.Description, &r.ReportedAt, &r.ReviewedAt, &r.ReviewerID, &r.Resolution)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	r.ReportedAt = r.ReportedAt.UTC()
	if r.ReviewedAt != nil {
		*r.ReviewedAt = r.ReviewedAt.UTC()
	}
	return r, err
}

func (r *AttendanceRepository) Get(ctx context.Context, coordinator, id string) (domain.Report, error) {
	return scanReport(r.db.QueryRow(ctx, coordinatorReports+" AND a.id=$2", coordinator, id))
}

func (r *AttendanceRepository) List(ctx context.Context, coordinator string, status domain.Status, limit, offset int) ([]domain.Report, error) {
	rows, err := r.db.Query(ctx, coordinatorReports+`
		AND a.status=$2 ORDER BY a.reported_at,a.id LIMIT $3 OFFSET $4`, coordinator, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Report, error) { return scanReport(row) })
}

func (t attendanceTx) Report(ctx context.Context, coordinator, id string, lock bool) (domain.Report, error) {
	query := coordinatorReports + " AND a.id=$2"
	if lock {
		query += " FOR UPDATE OF a"
	}
	return scanReport(t.tx.QueryRow(ctx, query, coordinator, id))
}

func (t attendanceTx) InsertReport(ctx context.Context, r domain.Report) (domain.Report, error) {
	return scanReport(t.tx.QueryRow(ctx, `
		INSERT INTO attendance_reports AS a (session_id,reporter_user_id,absent_user_id,details,reported_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING `+reportColumns, r.SessionID, r.ReporterID, r.AbsentID, r.Description, r.ReportedAt))
}

func (t attendanceTx) Decide(ctx context.Context, r domain.Report) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE attendance_reports SET status=$2,reviewed_at=$3,reviewed_by_coordinator_id=$4,resolution_note=$5
		WHERE id=$1 AND status='PENDING'`, r.ID, r.Status, r.ReviewedAt, r.ReviewerID, r.Resolution)
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrConflict
	}
	return err
}

func (t attendanceTx) RecordNoShow(ctx context.Context, r domain.Report, s scheduling.Session, home string) error {
	return (discipline{tx: t.tx}).recordNoShow(ctx, r.AbsentID, s.SemesterID, s.ID, home, r.ID, r.ReviewerID, s.EndsAt, *r.ReviewedAt)
}

func (t attendanceTx) NotifyReport(ctx context.Context, r domain.Report, faculty string) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO notifications (recipient_user_id,type,subject,body,session_id,deduplication_key)
		SELECT user_id,'ATTENDANCE_REPORTED','Attendance report submitted',
		       'Sign in to Tutti to review the report.',$2,$3
		FROM coordinators WHERE faculty_id=$1`, faculty, r.SessionID, "attendance-report:"+r.ID)
	return err
}

func (t attendanceTx) NotifyDecision(ctx context.Context, r domain.Report) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO notifications (recipient_user_id,type,subject,body,session_id,deduplication_key)
		SELECT recipient::uuid,'ATTENDANCE_DECIDED','Attendance report reviewed',$2,$3,$4
		FROM unnest($1::text[]) AS recipients(recipient)`, []string{r.ReporterID, r.AbsentID},
		"The attendance report was "+string(r.Status)+". Sign in to Tutti to view your session.",
		r.SessionID, "attendance-decision:"+r.ID)
	return err
}
