package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// SuspensionDecision is an authentic account transition, not a role approval.
type SuspensionDecision struct {
	ID, UserID, FacultyID, ActorID string
	WasSuspended, IsSuspended      bool
	Reason                         string
	At                             time.Time
}

func SuspensionReason(input string) (string, error) {
	text := strings.TrimSpace(input)
	if text == "" || len(text) > 2000 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", ErrInvalid
	}
	return text, nil
}

func (p Principal) ChangeSuspension(suspend bool) error {
	if p.ID == "" || !p.Student {
		return ErrNotFound
	}
	if p.Closed || p.Suspended == suspend {
		return ErrConflict
	}
	return nil
}
