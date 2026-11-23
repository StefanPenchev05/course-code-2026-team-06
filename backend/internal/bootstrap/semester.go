package bootstrap

import (
	"context"
	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/interfaces/cli"
	coordpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/infrastructure/postgres"
	schedulingpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/infrastructure/postgres"
	"io"
	"time"
)

func RunSemesterMaintenance(ctx context.Context, args []string, out, errOut io.Writer) int {
	return cli.Run(ctx, args, out, errOut, func(ctx context.Context) (*catalog.CalendarService, func(), error) {
		pool, err := openDatabase(ctx)
		if err != nil {
			return nil, nil, err
		}
		repo := catalogpg.NewCalendar(pool, catalogpg.CalendarDependencies{Scheduling: schedulingpg.Calendar, Discipline: coordpg.Calendar})
		return catalog.NewCalendar(repo, time.Now), pool.Close, nil
	})
}
