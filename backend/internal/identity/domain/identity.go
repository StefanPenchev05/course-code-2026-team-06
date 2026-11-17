// Package domain defines the identity module's permissions and verification state.
package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalid = errors.New("invalid input")
	ErrUnauthenticated = errors.New("invalid credentials or session")
	ErrForbidden = errors.New("permission denied")
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("conflicting operation")
)

type Status string

const (
	Pending Status = "PENDING"
	Approved Status = "APPROVED"
	Rejected Status = "REJECTED"
)

type Principal struct {
	ID string
	Name string
	Email string
	Student bool
	Tutor bool
	CoordinatorFaculty string
	Verification Status
	Suspended bool
	Closed bool
	RejectionReason string
}

func (p Principal) Active() bool { return p.ID != "" && !p.Suspended && !p.Closed }
func (p Principal) VerifiedStudent() bool { return p.Active() && p.Student && p.Verification == Approved }
func (p Principal) CanTutor() bool { return p.VerifiedStudent() && p.Tutor }
func (p Principal) Coordinator() bool { return p.Active() && p.CoordinatorFaculty != "" }
func (p Principal) CanAccessFaculty(faculty string) bool {
	return p.Coordinator() && p.CoordinatorFaculty == faculty
}

type StudentDetails struct {
	Name string
	Email string
	StudentNumber string
	ProgramID string
}

type Verification struct {
	ID string `json:"id"`
	StudentID string `json:"student_id"`
	FacultyID string `json:"faculty_id"`
	Name string `json:"name"`
	Email string `json:"email"`
	StudentNumber string `json:"student_number"`
	ProgramID string `json:"program_id"`
	Status Status `json:"status"`
	SubmittedAt time.Time `json:"submitted_at"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy string `json:"reviewed_by,omitempty"`
	RejectionReason string `json:"rejection_reason,omitempty"`
}
