// Package monitoring publishes Scheduling's read contract without depending on
// its booking orchestration (which itself consumes Coordination's discipline port).
package monitoring

import (
	"context"
	"time"
)

// ScheduleEntry distinguishes published windows from booked sessions. It never
// includes student identity, descriptions, cancellation evidence or meeting secrets.
type ScheduleEntry struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Mode        string    `json:"mode"`
	Status      string    `json:"status,omitempty"`
	SubjectID   string    `json:"subject_id,omitempty"`
	SubjectName string    `json:"subject_name,omitempty"`
	RuleID      string    `json:"rule_id,omitempty"`
	TimeZone    string    `json:"time_zone,omitempty"`
	SemesterID  string    `json:"semester_id,omitempty"`
}

type Filter struct {
	From, To      time.Time
	Limit, Offset int
}

type Queries interface {
	Schedule(context.Context, string, string, Filter) ([]ScheduleEntry, error)
}
