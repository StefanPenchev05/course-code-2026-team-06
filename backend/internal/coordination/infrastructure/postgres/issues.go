package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// Bootstrap binds all cross-module capabilities to the same pgx transaction.
type IssueDependencies struct {
	LockStudent            func(context.Context, pgx.Tx, string) (identity.Principal, error)
	LockCoordinatorFaculty func(context.Context, pgx.Tx, string) (string, error)
	HomeFaculty            func(context.Context, pgx.Tx, string) (string, error)
	LockParticipantSession func(context.Context, pgx.Tx, string, string) (scheduling.Session, error)
}

type IssueRepository struct {
	db   *pgxpool.Pool
	deps IssueDependencies
}

func NewIssues(db *pgxpool.Pool, deps IssueDependencies) *IssueRepository {
	return &IssueRepository{db: db, deps: deps}
}

func (r *IssueRepository) Transact(ctx context.Context, work func(application.IssueTx) error) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error { return work(issueTx{tx: tx, deps: r.deps}) })
	switch {
	case errors.Is(err, identity.ErrForbidden), errors.Is(err, identity.ErrNotFound),
		errors.Is(err, catalog.ErrProgramNotFound), errors.Is(err, catalog.ErrInvalidID):
		return domain.ErrForbidden
	case errors.Is(err, scheduling.ErrNotFound):
		return domain.ErrIssueNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503", "23505", "40001", "40P01":
			return domain.ErrIssueConflict
		}
	}
	return err
}

type issueTx struct {
	tx   pgx.Tx
	deps IssueDependencies
}

func (t issueTx) Student(ctx context.Context, actor string) (identity.Principal, error) {
	return t.deps.LockStudent(ctx, t.tx, actor)
}
func (t issueTx) HomeFaculty(ctx context.Context, program string) (string, error) {
	return t.deps.HomeFaculty(ctx, t.tx, program)
}
func (t issueTx) ParticipantSession(ctx context.Context, id, actor string) (scheduling.Session, error) {
	return t.deps.LockParticipantSession(ctx, t.tx, id, actor)
}

func (t issueTx) CoordinatorFaculty(ctx context.Context, actor string) (string, error) {
	return t.deps.LockCoordinatorFaculty(ctx, t.tx, actor)
}

const issueColumns = `i.id::text,i.reporter_user_id::text,i.faculty_id::text,i.session_id::text,
	i.description,i.status::text,i.created_at,i.resolved_at,coalesce(i.handled_by_coordinator_id::text,''),coalesce(i.resolution_note,'')`

const coordinatorIssues = `SELECT ` + issueColumns + ` FROM issue_reports i
	JOIN coordinators c ON c.faculty_id=i.faculty_id
	JOIN users actor ON actor.id=c.user_id AND NOT actor.is_suspended AND actor.closed_at IS NULL
	WHERE c.user_id=$1`

func scanIssue(row pgx.Row) (domain.Issue, error) {
	var r domain.Issue
	err := row.Scan(&r.ID, &r.ReporterID, &r.FacultyID, &r.SessionID, &r.Description, &r.Status, &r.CreatedAt, &r.ResolvedAt, &r.ResolverID, &r.Resolution)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrIssueNotFound
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if r.ResolvedAt != nil {
		*r.ResolvedAt = r.ResolvedAt.UTC()
	}
	return r, err
}

func (r *IssueRepository) Get(ctx context.Context, coordinator, id string) (domain.Issue, error) {
	return scanIssue(r.db.QueryRow(ctx, coordinatorIssues+` AND i.id=$2`, coordinator, id))
}

func (r *IssueRepository) List(ctx context.Context, coordinator string, status domain.IssueStatus, limit, offset int) ([]domain.Issue, error) {
	rows, err := r.db.Query(ctx, coordinatorIssues+` AND i.status=$2 ORDER BY i.created_at,i.id LIMIT $3 OFFSET $4`, coordinator, status, limit, offset)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Issue, error) { return scanIssue(row) })
}

func (t issueTx) LockIssue(ctx context.Context, id, faculty string) (domain.Issue, error) {
	return scanIssue(t.tx.QueryRow(ctx, `SELECT `+issueColumns+` FROM issue_reports i WHERE i.id=$1 AND i.faculty_id=$2 FOR UPDATE OF i`, id, faculty))
}

func (t issueTx) ResolveIssue(ctx context.Context, report domain.Issue, coordinator, note string) (domain.Issue, error) {
	r, err := scanIssue(t.tx.QueryRow(ctx, `UPDATE issue_reports AS i
		SET status='RESOLVED',handled_by_coordinator_id=$3,resolution_note=$4,resolved_at=statement_timestamp()
		WHERE i.id=$1 AND i.faculty_id=$2 AND i.status='OPEN' RETURNING `+issueColumns, report.ID, report.FacultyID, coordinator, note))
	if errors.Is(err, domain.ErrIssueNotFound) {
		err = domain.ErrIssueConflict
	}
	return r, err
}

func (t issueTx) RecordIssueResolution(ctx context.Context, r domain.Issue) error {
	tag, err := t.tx.Exec(ctx, `INSERT INTO issue_status_history
		(issue_report_id,from_status,to_status,changed_at,actor_kind,changed_by_user_id)
		SELECT id,'OPEN',status,resolved_at,'USER',handled_by_coordinator_id FROM issue_reports
		WHERE id=$1 AND status='RESOLVED'`, r.ID)
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrIssueConflict
	}
	return err
}

func (t issueTx) InsertIssue(ctx context.Context, r domain.Issue) (domain.Issue, error) {
	var result domain.Issue
	err := t.tx.QueryRow(ctx, `INSERT INTO issue_reports
		(reporter_user_id,faculty_id,session_id,description,status,created_at)
		VALUES ($1,$2,$3,$4,'OPEN',statement_timestamp())
		RETURNING id::text,reporter_user_id::text,faculty_id::text,session_id::text,description,status::text,created_at`,
		r.ReporterID, r.FacultyID, r.SessionID, r.Description).
		Scan(&result.ID, &result.ReporterID, &result.FacultyID, &result.SessionID, &result.Description, &result.Status, &result.CreatedAt)
	result.CreatedAt = result.CreatedAt.UTC()
	return result, err
}

func (t issueTx) RecordIssueCreation(ctx context.Context, r domain.Issue) error {
	tag, err := t.tx.Exec(ctx, `INSERT INTO issue_status_history
		(issue_report_id,from_status,to_status,changed_at,actor_kind,changed_by_user_id)
		SELECT id,NULL,status,created_at,'USER',reporter_user_id FROM issue_reports
		WHERE id=$1 AND status='OPEN'`, r.ID)
	if err == nil && tag.RowsAffected() != 1 {
		return domain.ErrIssueConflict
	}
	return err
}
