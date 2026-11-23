package postgres

import (
	"context"
	"errors"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/domain"
	feedback "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application/monitoring"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MonitoringDependencies struct {
	Faculty    func(context.Context, pgx.Tx, string) (string, error)
	Tutors     func(pgx.Tx) tutoring.MonitoringQueries
	Activity   func(pgx.Tx) feedback.MonitoringQueries
	Scheduling func(pgx.Tx) scheduling.Queries
}
type MonitoringRepository struct {
	db   *pgxpool.Pool
	deps MonitoringDependencies
}

func NewMonitoring(db *pgxpool.Pool, deps MonitoringDependencies) *MonitoringRepository {
	return &MonitoringRepository{db, deps}
}

func (r *MonitoringRepository) Read(ctx context.Context, actor string, work func(application.MonitoringSnapshot) error) error {
	return pgx.BeginTxFunc(ctx, r.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		// Resolve permissions again in the same snapshot as the response. Neither
		// a stale middleware principal nor a client filter supplies faculty authority.
		faculty, err := r.deps.Faculty(ctx, tx, actor)
		if errors.Is(err, identity.ErrForbidden) {
			return domain.ErrForbidden
		}
		if err != nil {
			return err
		}
		return work(application.MonitoringSnapshot{Faculty: faculty,
			Tutors: r.deps.Tutors(tx), Activity: r.deps.Activity(tx), Scheduling: r.deps.Scheduling(tx)})
	})
}
