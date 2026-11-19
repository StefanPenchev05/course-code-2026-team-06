package bootstrap

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	coordinationpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/application"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	tutoringpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/infrastructure/postgres"
)

func openDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid PostgreSQL connection configuration")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("could not connect to PostgreSQL")
	}
	return pool, nil
}

func newBookingService(pool *pgxpool.Pool) *scheduling.BookingService {
	repo := schedulingpg.NewBooking(pool, schedulingpg.BookingDependencies{
		LockStudent: identitypg.LockStudent,
		Catalog:     func(tx pgx.Tx) catalog.AcademicCatalog { return catalog.New(catalogpg.InTransaction(tx)) },
		Offerings:   tutoringpg.BookingOfferings,
		Discipline:  coordinationpg.Discipline,
	})
	return scheduling.NewBooking(repo, time.Now)
}
