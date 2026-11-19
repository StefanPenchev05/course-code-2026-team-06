package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

var (
	ErrInvalid   = errors.New("provide a valid identifier and non-empty text of at most 2000 bytes")
	ErrForbidden = errors.New("permission denied")
	ErrNotFound  = errors.New("attendance resource not found")
	ErrConflict  = errors.New("attendance report already exists, was decided or conflicts with the session outcome")
	ErrWindow    = errors.New("reporting is open from scheduled end through seven elapsed days after the end")
)

type Status string

const (
	Pending   Status = "PENDING"
	Confirmed Status = "CONFIRMED"
	Rejected  Status = "REJECTED"
)

func (s Status) Valid() bool { return s == Pending || s == Confirmed || s == Rejected }

type Report struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	ReporterID  string     `json:"reporter_user_id"`
	AbsentID    string     `json:"absent_user_id"`
	Status      Status     `json:"status"`
	Description string     `json:"description"`
	ReportedAt  time.Time  `json:"reported_at"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	ReviewerID  string     `json:"reviewed_by_coordinator_id,omitempty"`
	Resolution  string     `json:"resolution_note,omitempty"`
}

func Text(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" || len(value) > 2000 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", ErrInvalid
	}
	return value, nil
}

func AbsentParticipant(s scheduling.Session, actor string, now time.Time) (string, error) {
	if s.StudentID == s.TutorID || (actor != s.StudentID && actor != s.TutorID) {
		return "", ErrNotFound
	}
	if s.Status != scheduling.Accepted && s.Status != scheduling.Completed {
		return "", ErrConflict
	}
	if now.Before(s.EndsAt) || now.After(s.EndsAt.Add(168*time.Hour)) {
		return "", ErrWindow
	}
	if actor == s.StudentID {
		return s.TutorID, nil
	}
	return s.StudentID, nil
}

func (r Report) ValidateDecision(status Status, session scheduling.Session) error {
	if status != Confirmed && status != Rejected {
		return ErrInvalid
	}
	if r.Status != Pending {
		return ErrConflict
	}
	if status == Confirmed && session.Status != scheduling.Accepted && session.Status != scheduling.Completed && session.Status != scheduling.NoShow {
		return ErrConflict
	}
	return nil
}
