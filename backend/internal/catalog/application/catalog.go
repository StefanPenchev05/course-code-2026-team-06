// Package application exposes Catalog queries to HTTP and other business modules.
package application

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
)

const DefaultLimit = 25

// AcademicCatalog is the read port for other modules' academic eligibility checks.
// IDs use the same UUID string representation as Identity.
type AcademicCatalog interface {
	SubjectBelongsToProgram(ctx context.Context, programID, subjectID string) (bool, error)
	GetProgramFaculty(ctx context.Context, programID string) (string, error)
}

type Repository interface {
	ListFaculties(context.Context) ([]domain.Faculty, error)
	ListPrograms(context.Context, string, domain.DegreeLevel) ([]domain.Program, error)
	GetProgram(context.Context, string) (domain.ProgramDetails, error)
	ListProgramSubjects(ctx context.Context, programID, query string, limit, offset int) ([]domain.Subject, error)
	SearchSubjects(ctx context.Context, query string, limit, offset int) ([]domain.Subject, error)
	GetSubject(context.Context, string) (domain.Subject, error)
	AcademicCatalog
}

type Service struct{ repo Repository }

var _ AcademicCatalog = (*Service)(nil)

func New(repo Repository) *Service { return &Service{repo: repo} }

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Service) ListFaculties(ctx context.Context) ([]domain.Faculty, error) {
	return s.repo.ListFaculties(ctx)
}

func (s *Service) ListPrograms(ctx context.Context, facultyID string, level domain.DegreeLevel) ([]domain.Program, error) {
	if !uuid.MatchString(facultyID) {
		return nil, domain.ErrInvalidID
	}
	if level != "" && !level.Valid() {
		return nil, domain.ErrInvalidDegreeLevel
	}
	return s.repo.ListPrograms(ctx, facultyID, level)
}

func (s *Service) GetProgram(ctx context.Context, id string) (domain.ProgramDetails, error) {
	if !uuid.MatchString(id) {
		return domain.ProgramDetails{}, domain.ErrInvalidID
	}
	return s.repo.GetProgram(ctx, id)
}

func (s *Service) GetSubject(ctx context.Context, id string) (domain.Subject, error) {
	if !uuid.MatchString(id) {
		return domain.Subject{}, domain.ErrInvalidID
	}
	return s.repo.GetSubject(ctx, id)
}

type SubjectFilter struct {
	Query  string
	Limit  int
	Offset int
}

type SubjectPage struct {
	Subjects []domain.Subject
	HasMore  bool
}

func (f SubjectFilter) validate() error {
	if !utf8.ValidString(f.Query) || utf8.RuneCountInString(f.Query) > 100 || strings.ContainsRune(f.Query, 0) {
		return domain.ErrInvalidSearch
	}
	if f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 100000 {
		return domain.ErrInvalidPagination
	}
	return nil
}

func subjectPage(subjects []domain.Subject, limit int) SubjectPage {
	if len(subjects) > limit {
		return SubjectPage{Subjects: subjects[:limit], HasMore: true}
	}
	return SubjectPage{Subjects: subjects}
}

func (s *Service) ListProgramSubjects(ctx context.Context, programID string, f SubjectFilter) (SubjectPage, error) {
	if !uuid.MatchString(programID) {
		return SubjectPage{}, domain.ErrInvalidID
	}
	if err := f.validate(); err != nil {
		return SubjectPage{}, err
	}
	subjects, err := s.repo.ListProgramSubjects(ctx, programID, strings.TrimSpace(f.Query), f.Limit+1, f.Offset)
	if err != nil {
		return SubjectPage{}, err
	}
	return subjectPage(subjects, f.Limit), nil
}

func (s *Service) SearchSubjects(ctx context.Context, f SubjectFilter) (SubjectPage, error) {
	if err := f.validate(); err != nil {
		return SubjectPage{}, err
	}
	subjects, err := s.repo.SearchSubjects(ctx, strings.TrimSpace(f.Query), f.Limit+1, f.Offset)
	if err != nil {
		return SubjectPage{}, err
	}
	return subjectPage(subjects, f.Limit), nil
}

func (s *Service) SubjectBelongsToProgram(ctx context.Context, programID, subjectID string) (bool, error) {
	if !uuid.MatchString(programID) || !uuid.MatchString(subjectID) {
		return false, domain.ErrInvalidID
	}
	return s.repo.SubjectBelongsToProgram(ctx, programID, subjectID)
}

func (s *Service) GetProgramFaculty(ctx context.Context, programID string) (string, error) {
	if !uuid.MatchString(programID) {
		return "", domain.ErrInvalidID
	}
	return s.repo.GetProgramFaculty(ctx, programID)
}
