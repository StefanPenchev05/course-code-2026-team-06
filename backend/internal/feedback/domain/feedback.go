package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid    = errors.New("rating must be an integer from 1 to 5 and comment at most 2000 characters")
	ErrQuery      = errors.New("invalid identifier or pagination")
	ErrForbidden  = errors.New("permission denied")
	ErrNotFound   = errors.New("feedback resource not found")
	ErrConflict   = errors.New("session already has feedback")
	ErrIneligible = errors.New("only currently completed sessions can receive feedback")
)

type Input struct {
	Rating  int    `json:"rating"`
	Comment string `json:"comment"`
}

func (i Input) Normalize() (Input, error) {
	if i.Rating < 1 || i.Rating > 5 || !utf8.ValidString(i.Comment) ||
		utf8.RuneCountInString(i.Comment) > 2000 || strings.ContainsRune(i.Comment, 0) {
		return Input{}, ErrInvalid
	}
	i.Comment = strings.TrimSpace(i.Comment)
	return i, nil
}

type Entry struct {
	SessionID     string    `json:"session_id"`
	SessionStatus string    `json:"session_status"`
	SubjectID     string    `json:"subject_id"`
	SubjectName   string    `json:"subject_name"`
	StartsAt      time.Time `json:"starts_at"`
	Rating        int       `json:"rating"`
	Comment       string    `json:"comment,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type TutorRatingSummary struct {
	AverageRating *float64 `json:"average_rating"`
	RatingCount   int64    `json:"rating_count"`
}

type Statistics struct {
	TutorRatingSummary
	CompletedSessions int64   `json:"completed_sessions"`
	HoursTaught       float64 `json:"hours_taught"`
}
