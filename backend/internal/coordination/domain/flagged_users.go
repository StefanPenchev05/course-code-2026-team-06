package domain

import "time"

// FlaggedUser is a summary, not an attendance or account decision detail.
type FlaggedUser struct {
	FlagID      string    `json:"flag_id"`
	UserID      string    `json:"user_id"`
	SemesterID  string    `json:"semester_id"`
	StrikeCount int64     `json:"strike_count"`
	FlaggedAt   time.Time `json:"flagged_at"`
	IsSuspended bool      `json:"is_suspended"`
}
