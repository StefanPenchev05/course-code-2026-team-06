package application

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

// MonitoredTutor deliberately excludes account contacts and qualification evidence.
// Active means currently eligible with at least one eligible approved offering.
type MonitoredTutor struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProgramID   string    `json:"program_id"`
	ProgramName string    `json:"program_name"`
	FacultyID   string    `json:"faculty_id"`
	ApprovedAt  time.Time `json:"approved_at"`
	Active      bool      `json:"active"`
}

type MonitoringFilter struct {
	ProgramID, SubjectID string
	domain.Page
}

type TutorCounts struct {
	ApprovedTutors int64 `json:"approved_tutors"`
	ActiveTutors   int64 `json:"active_tutors"`
}

// MonitoringQueries is bound to the caller's read snapshot and trusted faculty.
type MonitoringQueries interface {
	Tutors(context.Context, string, MonitoringFilter) ([]MonitoredTutor, error)
	Tutor(context.Context, string, string) (MonitoredTutor, error)
	Offerings(context.Context, string, string, domain.Page) ([]domain.Offering, error)
	Counts(context.Context, string) (TutorCounts, error)
}
