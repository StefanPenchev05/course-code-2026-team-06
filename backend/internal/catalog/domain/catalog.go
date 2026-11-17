package domain

import "errors"

var (
	ErrFacultyNotFound    = errors.New("faculty not found")
	ErrProgramNotFound    = errors.New("program not found")
	ErrSubjectNotFound    = errors.New("subject not found")
	ErrInvalidID          = errors.New("invalid UUID")
	ErrInvalidDegreeLevel = errors.New("level must be BACHELOR or MASTER")
	ErrInvalidPagination  = errors.New("limit must be 1–100 and offset must be 0–100000")
	ErrInvalidSearch      = errors.New("q must be valid text of at most 100 characters")
)

type DegreeLevel string

const (
	Bachelor DegreeLevel = "BACHELOR"
	Master   DegreeLevel = "MASTER"
)

func (d DegreeLevel) Valid() bool { return d == Bachelor || d == Master }

type Faculty struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Program struct {
	ID        string      `json:"id"`
	FacultyID string      `json:"faculty_id"`
	Name      string      `json:"name"`
	Level     DegreeLevel `json:"level"`
}

type ProgramDetails struct {
	Program
	Faculty Faculty `json:"faculty"`
}

type Subject struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}
