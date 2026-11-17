// Package application owns Coordination's disciplinary operations.
package application

import (
	"context"
	"time"
)

type LateCancellation struct {
	UserID, SemesterID, SessionID, HomeFacultyID string
	OccurredAt                                   time.Time
}

// Discipline participates in the caller's transaction. The caller must hold
// the affected user's Identity write lock for semester incident counting.
type Discipline interface {
	RecordLateCancellation(context.Context, LateCancellation) error
}
