package application

import "context"

// BookingAudience exposes routing identifiers only, never descriptions, grades,
// meeting credentials or disciplinary evidence. Current authority is rechecked
// at delivery rather than persisted as a historical recipient entitlement.
type BookingAudience struct{ StudentID, TutorID, FacultyID, CoordinatorID string }
type RealtimeQueries interface {
	BookingAudience(context.Context, string) (BookingAudience, error)
}
