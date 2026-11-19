package application

import (
	"context"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
)

// MonitoringQueries derives activity in the caller's snapshot, using stored
// session faculty, including sessions of tutors who subsequently moved away.
type MonitoringQueries interface {
	TutorStatistics(context.Context, string, []string) (map[string]domain.Statistics, error)
	FacultyStatistics(context.Context, string) (domain.Statistics, error)
}
