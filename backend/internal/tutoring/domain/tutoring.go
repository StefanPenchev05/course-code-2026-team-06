package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid           = errors.New("invalid input")
	ErrForbidden         = errors.New("permission denied")
	ErrNotFound          = errors.New("resource not found")
	ErrConflict          = errors.New("conflicting operation")
	ErrIneligibleSubject = fmt.Errorf("%w: subject is not in the student's current program", ErrInvalid)
)

type Status string

const (
	Pending  Status = "PENDING"
	Approved Status = "APPROVED"
	Rejected Status = "REJECTED"
)

func (s Status) Valid() bool { return s == Pending || s == Approved || s == Rejected }

// Grade represents exact hundredths. Parse never rounds a submitted grade.
type Grade int

var gradeFormat = regexp.MustCompile(`^[0-9]{2}(\.[0-9]{1,2})?$`)

func ParseGrade(value string) (Grade, error) {
	invalid := fmt.Errorf("%w: grade must be 15.00 to 20.00 with at most two decimal places", ErrInvalid)
	if !gradeFormat.MatchString(value) {
		return 0, invalid
	}
	whole, fraction, _ := strings.Cut(value, ".")
	n, _ := strconv.Atoi(whole)
	cents, _ := strconv.Atoi((fraction + "00")[:2])
	grade := Grade(n*100 + cents)
	if !grade.Valid() {
		return 0, invalid
	}
	return grade, nil
}

func (g Grade) Valid() bool    { return g >= 1500 && g <= 2000 }
func (g Grade) String() string { return fmt.Sprintf("%d.%02d", g/100, g%100) }

type Application struct {
	ID              string     `json:"id"`
	StudentID       string     `json:"student_id"`
	SubjectID       string     `json:"subject_id"`
	SubjectName     string     `json:"subject_name"`
	Status          Status     `json:"status"`
	SubmittedAt     time.Time  `json:"submitted_at"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
}

type ReviewDetail struct {
	Application
	Grade       string `json:"grade"`
	Description string `json:"description"`
}

type Offering struct {
	ID          string `json:"id"`
	SubjectID   string `json:"subject_id"`
	SubjectName string `json:"subject_name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type Tutor struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type Profile struct {
	Tutor
	Offerings []Offering `json:"offerings"`
}

type Match struct {
	Tutor
	Offering Offering `json:"offering"`
}

type Page struct{ Limit, Offset int }
type Search struct {
	SubjectID, FacultyID, ProgramID, Level string
	Page
}
