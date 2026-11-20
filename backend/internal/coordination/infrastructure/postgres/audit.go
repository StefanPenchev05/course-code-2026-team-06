package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	identityapp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type AuditDependencies struct {
	Faculty  func(context.Context, pgx.Tx, string) (string, error)
	Accounts func(pgx.Tx) identityapp.SuspensionHistory
}
type AuditRepository struct {
	db   *pgxpool.Pool
	deps AuditDependencies
}

func NewAudit(db *pgxpool.Pool, deps AuditDependencies) *AuditRepository {
	return &AuditRepository{db, deps}
}
func (r *AuditRepository) ListAudit(ctx context.Context, actor, user, category string, limit, offset int) ([]domain.AuditEntry, error) {
	result := []domain.AuditEntry{}
	err := pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		faculty, err := r.deps.Faculty(ctx, tx, actor)
		if errors.Is(err, identity.ErrForbidden) {
			return domain.ErrForbidden
		}
		if err != nil {
			return err
		}
		if category == "SUSPENSION" {
			decisions, err := r.deps.Accounts(tx).ListSuspensionHistory(ctx, user, faculty, limit, offset)
			if err != nil {
				return err
			}
			for _, d := range decisions {
				kind := "USER"
				result = append(result, domain.AuditEntry{ID: d.ID, UserID: d.UserID, FacultyID: d.FacultyID, Category: category, At: d.At,
					ActorKind: &kind, ActorID: &d.ActorID, WasSuspended: &d.WasSuspended, IsSuspended: &d.IsSuspended, Reason: d.Reason})
			}
			return nil
		}
		var rows pgx.Rows
		switch category {
		case "VIOLATION":
			rows, err = tx.Query(ctx, `SELECT id::text,recorded_at,actor_kind::text,recorded_by_user_id::text,
    kind::text,semester_id::text,session_id::text,occurred_at,attendance_report_id::text,cancellation_history_id::text
    FROM user_violations WHERE user_id=$1 AND home_faculty_id=$2
    ORDER BY recorded_at DESC,id DESC LIMIT $3 OFFSET $4`, user, faculty, limit, offset)
		case "FLAG":
			rows, err = tx.Query(ctx, `SELECT id::text,flagged_at,actor_kind::text,semester_id::text,first_violation_id::text,second_violation_id::text
    FROM user_semester_flags WHERE user_id=$1 AND faculty_id=$2
    ORDER BY flagged_at DESC,id DESC LIMIT $3 OFFSET $4`, user, faculty, limit, offset)
		default:
			return domain.ErrInvalid
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := domain.AuditEntry{UserID: user, FacultyID: faculty, Category: category}
			if category == "VIOLATION" {
				err = rows.Scan(&e.ID, &e.At, &e.ActorKind, &e.ActorID, &e.Kind, &e.SemesterID, &e.SessionID, &e.OccurredAt, &e.AttendanceReportID, &e.CancellationHistoryID)
			} else {
				err = rows.Scan(&e.ID, &e.At, &e.ActorKind, &e.SemesterID, &e.FirstViolationID, &e.SecondViolationID)
			}
			if err != nil {
				return err
			}
			e.At = e.At.UTC()
			if e.OccurredAt != nil {
				*e.OccurredAt = e.OccurredAt.UTC()
			}
			result = append(result, e)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
