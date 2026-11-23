package domain

import "time"

// AuditEntry is a minimal coordinator projection, never a public account profile.
// A nil actor kind or evidence ID means legacy metadata is unknown.
type AuditEntry struct {
	ID                    string     `json:"id"`
	UserID                string     `json:"user_id"`
	FacultyID             string     `json:"faculty_id"`
	Category              string     `json:"category"`
	At                    time.Time  `json:"recorded_at"`
	ActorKind             *string    `json:"actor_kind"`
	ActorID               *string    `json:"actor_user_id"`
	WasSuspended          *bool      `json:"was_suspended,omitempty"`
	IsSuspended           *bool      `json:"is_suspended,omitempty"`
	Reason                string     `json:"reason,omitempty"`
	Kind                  string     `json:"violation_kind,omitempty"`
	SemesterID            string     `json:"semester_id,omitempty"`
	SessionID             string     `json:"session_id,omitempty"`
	OccurredAt            *time.Time `json:"occurred_at,omitempty"`
	AttendanceReportID    *string    `json:"attendance_report_id,omitempty"`
	CancellationHistoryID *string    `json:"cancellation_history_id,omitempty"`
	FirstViolationID      *string    `json:"first_violation_id,omitempty"`
	SecondViolationID     *string    `json:"second_violation_id,omitempty"`
}
