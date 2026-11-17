package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrBookingInvalid = errors.New("invalid booking")
	ErrBookingOverlap = errors.New("a participant already has a reservation in this interval")
	ErrBookingState   = errors.New("session cannot make this transition")
	ErrRequestExpired = errors.New("the session request has expired")
)

type Status string

const (
	Requested Status = "REQUESTED"
	Accepted  Status = "ACCEPTED"
	Declined  Status = "DECLINED"
	Expired   Status = "EXPIRED"
	Cancelled Status = "CANCELLED"
	Completed Status = "COMPLETED"
	NoShow    Status = "NO_SHOW"
)

func (s Status) Valid() bool {
	switch s {
	case Requested, Accepted, Declined, Expired, Cancelled, Completed, NoShow:
		return true
	default:
		return false
	}
}

type Purpose string

const Introduction Purpose = "INTRODUCTION"

func (p Purpose) Allows(minutes int) bool {
	switch p {
	case Introduction:
		return minutes == 30
	case "HOMEWORK", "EXAM_PREPARATION", "COURSE_REVIEW", "OTHER":
		return minutes == 60 || minutes == 90 || minutes == 120
	default:
		return false
	}
}

type Request struct {
	OfferingID  string    `json:"offering_id"`
	SlotID      string    `json:"availability_slot_id"`
	StartsAt    time.Time `json:"starts_at"`
	Minutes     int       `json:"duration_minutes"`
	Mode        Mode      `json:"mode"`
	Purpose     Purpose   `json:"purpose"`
	Description string    `json:"description"`
}

func (r Request) Validate() error {
	if r.StartsAt.IsZero() || r.StartsAt.Nanosecond()%1000 != 0 || (r.Mode != Online && r.Mode != Onsite) ||
		!r.Purpose.Allows(r.Minutes) || !validBookingText(strings.TrimSpace(r.Description), 4000) {
		return fmt.Errorf("%w: provide a timestamp, allowed mode and purpose, valid duration, and non-empty description", ErrBookingInvalid)
	}
	return nil
}

func (r Request) End() time.Time { return r.StartsAt.Add(time.Duration(r.Minutes) * time.Minute) }

func (r Request) Fits(w Window) bool {
	return !r.StartsAt.Before(w.StartsAt) && !r.End().After(w.EndsAt) && (w.Mode == Both || w.Mode == r.Mode)
}

func Expiration(requestedAt, startsAt time.Time) (time.Time, error) {
	notice := startsAt.Sub(requestedAt)
	if notice < 3*time.Hour {
		return time.Time{}, fmt.Errorf("%w: booking requires at least three hours of notice", ErrBookingInvalid)
	}
	if notice <= 25*time.Hour {
		return earlier(requestedAt.Add(time.Hour), startsAt.Add(-2*time.Hour)), nil
	}
	return earlier(requestedAt.Add(48*time.Hour), startsAt.Add(-24*time.Hour)), nil
}

func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func LateCancellation(now, start time.Time) bool { return start.Sub(now) < 24*time.Hour }

type Session struct {
	ID                 string     `json:"id"`
	StudentID          string     `json:"student_id"`
	TutorID            string     `json:"tutor_id"`
	SubjectID          string     `json:"subject_id"`
	SubjectName        string     `json:"subject_name"`
	OfferingID         string     `json:"offering_id"`
	SlotID             string     `json:"availability_slot_id,omitempty"`
	SemesterID         string     `json:"semester_id"`
	StartsAt           time.Time  `json:"starts_at"`
	EndsAt             time.Time  `json:"ends_at"`
	Minutes            int        `json:"duration_minutes"`
	Mode               Mode       `json:"mode"`
	Purpose            Purpose    `json:"purpose"`
	Description        string     `json:"description"`
	Status             Status     `json:"status"`
	RequestedAt        time.Time  `json:"requested_at"`
	ExpiresAt          time.Time  `json:"expires_at"`
	RespondedAt        *time.Time `json:"responded_at,omitempty"`
	DeclineReason      string     `json:"decline_reason,omitempty"`
	MeetingLink        string     `json:"meeting_link,omitempty"`
	MeetingRoom        string     `json:"meeting_room,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
	CancelledBy        string     `json:"cancelled_by_user_id,omitempty"`
	CancellationReason string     `json:"cancellation_reason,omitempty"`
	Late               bool       `json:"is_late_cancellation"`
	ViewerRole         string     `json:"viewer_role,omitempty"`
}

func (s *Session) SetViewer(user string) {
	if s.StudentID == user {
		s.ViewerRole = "STUDENT"
	}
	if s.TutorID == user {
		s.ViewerRole = "TUTOR"
	}
}

func (s Session) Transition(to Status, now time.Time) error {
	switch to {
	case Accepted, Declined:
		if s.Status != Requested {
			return ErrBookingState
		}
		if !now.Before(s.ExpiresAt) {
			return ErrRequestExpired
		}
	case Expired:
		if s.Status != Requested || now.Before(s.ExpiresAt) {
			return ErrBookingState
		}
	case Cancelled:
		if s.Status != Requested && s.Status != Accepted {
			return ErrBookingState
		}
	default:
		return ErrBookingState
	}
	return nil
}

type Meeting struct {
	Link string `json:"meeting_link"`
	Room string `json:"meeting_room"`
}

func (m Meeting) Validate(mode Mode) error {
	switch mode {
	case Online:
		u, err := url.Parse(m.Link)
		if !validBookingText(m.Link, 2048) || strings.ContainsAny(m.Link, " \t\r\n") || err != nil ||
			(u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || m.Room != "" {
			return fmt.Errorf("%w: provide an HTTP or HTTPS meeting URL for an online session", ErrBookingInvalid)
		}
	case Onsite:
		if !validBookingText(m.Room, 500) || m.Link != "" {
			return fmt.Errorf("%w: provide a room or location for an onsite session", ErrBookingInvalid)
		}
	default:
		return ErrBookingInvalid
	}
	return nil
}

func Reason(input string) (string, error) {
	reason := strings.TrimSpace(input)
	if !validBookingText(reason, 2000) {
		return "", fmt.Errorf("%w: provide a reason of 1–2000 bytes", ErrBookingInvalid)
	}
	return reason, nil
}

func validBookingText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
