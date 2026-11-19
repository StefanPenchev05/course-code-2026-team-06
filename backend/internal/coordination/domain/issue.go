package domain

import (
	"errors"
	"time"
)

var (
	ErrIssueNotFound = errors.New("issue resource not found")
	ErrIssueConflict = errors.New("issue report conflicts with current data")
)

type IssueStatus string

const IssueOpen IssueStatus = "OPEN"

// Issue uses the existing issue_reports schema. Resolution is a separate use case.
type Issue struct {
	ID          string      `json:"id"`
	ReporterID  string      `json:"reporter_user_id"`
	FacultyID   string      `json:"faculty_id"`
	SessionID   *string     `json:"session_id,omitempty"`
	Description string      `json:"description"`
	Status      IssueStatus `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
}

type IssueInput struct {
	Description string  `json:"description"`
	SessionID   *string `json:"session_id"`
}
