package application

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	catalogdomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type Submission struct {
	SubjectID   string
	Grade       domain.Grade
	Description string
}

type ReviewRecord struct {
	ID, StudentID, SubjectID, QualificationID, Description string
	Status                                                 domain.Status
	Grade                                                  domain.Grade
}

type Transaction interface {
	Student(context.Context, string) (identity.Principal, error)
	CoordinatorFaculty(context.Context, string) (string, error)
	Catalog() catalog.AcademicCatalog
	ActiveOffering(ctx context.Context, student, subject string) (bool, error)
	CreateApplication(ctx context.Context, student, faculty string, input Submission) (string, error)
	ReviewRecord(ctx context.Context, id, faculty string, lock bool) (ReviewRecord, error)
	Decide(ctx context.Context, record ReviewRecord, reviewer string, status domain.Status, reason string) error
	ActiveOfferings(ctx context.Context, student string) ([]domain.Offering, error)
	Deactivate(ctx context.Context, offeringID string) error
}

type Repository interface {
	InTransaction(context.Context, func(Transaction) error) error
	Applications(ctx context.Context, actor string, coordinator bool, status domain.Status, page domain.Page) ([]domain.Application, error)
	OwnApplication(ctx context.Context, actor, id string) (domain.Application, error)
	ReviewDetail(ctx context.Context, actor, id string) (domain.ReviewDetail, error)
	Profile(ctx context.Context, tutor string, own bool) (domain.Profile, error)
	Search(ctx context.Context, actor string, filters domain.Search) ([]domain.Match, error)
	ApprovedOffering(ctx context.Context, tutor, subject string) (domain.Offering, error)
}

// OfferingEligibility supplies Scheduling with the current approved entitlement.
type OfferingEligibility interface {
	GetApprovedOffering(ctx context.Context, tutorID, subjectID string) (domain.Offering, error)
}

type Service struct {
	repo    Repository
	catalog catalog.AcademicCatalog
}

func New(repo Repository, catalog catalog.AcademicCatalog) *Service {
	return &Service{repo: repo, catalog: catalog}
}

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func validPage(p domain.Page) bool {
	return p.Limit >= 1 && p.Limit <= 100 && p.Offset >= 0 && p.Offset <= 100000
}

func subjectEligible(ctx context.Context, reader catalog.AcademicCatalog, program, subject string) error {
	ok, err := reader.SubjectBelongsToProgram(ctx, program, subject)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrIneligibleSubject
	}
	return nil
}

func (s *Service) Apply(ctx context.Context, actor identity.Principal, input Submission) (string, error) {
	if !actor.VerifiedStudent() {
		return "", domain.ErrForbidden
	}
	input.Description = strings.TrimSpace(input.Description)
	if !uuid.MatchString(input.SubjectID) || !input.Grade.Valid() || !validText(input.Description, 4000) {
		return "", fmt.Errorf("%w: subject, grade and a description of 1–4000 bytes are required", domain.ErrInvalid)
	}
	var id string
	err := s.repo.InTransaction(ctx, func(tx Transaction) error {
		student, err := tx.Student(ctx, actor.ID)
		if err != nil {
			return err
		}
		if !student.VerifiedStudent() {
			return domain.ErrForbidden
		}
		reader := tx.Catalog()
		faculty, err := reader.GetProgramFaculty(ctx, student.ProgramID)
		if err != nil {
			return err
		}
		if err := subjectEligible(ctx, reader, student.ProgramID, input.SubjectID); err != nil {
			return err
		}
		active, err := tx.ActiveOffering(ctx, actor.ID, input.SubjectID)
		if err != nil {
			return err
		}
		if active {
			return domain.ErrConflict
		}
		id, err = tx.CreateApplication(ctx, actor.ID, faculty, input)
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) Review(ctx context.Context, actor identity.Principal, id string, status domain.Status, reason string) error {
	if !actor.Coordinator() {
		return domain.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if !uuid.MatchString(id) || (status != domain.Approved && status != domain.Rejected) {
		return domain.ErrInvalid
	}
	if status == domain.Approved {
		reason = ""
	}
	if status == domain.Rejected && !validText(reason, 2000) {
		return fmt.Errorf("%w: rejection reason of 1–2000 bytes is required", domain.ErrInvalid)
	}
	return s.repo.InTransaction(ctx, func(tx Transaction) error {
		faculty, err := tx.CoordinatorFaculty(ctx, actor.ID)
		if err != nil {
			return err
		}
		app, err := tx.ReviewRecord(ctx, id, faculty, false)
		if err != nil {
			return err
		}
		if app.Status != domain.Pending {
			return domain.ErrConflict
		}
		// Acquire student before application, in the same order as submissions.
		student, err := tx.Student(ctx, app.StudentID)
		if err != nil {
			return err
		}
		app, err = tx.ReviewRecord(ctx, id, faculty, true)
		if err != nil {
			return err
		}
		if app.Status != domain.Pending {
			return domain.ErrConflict
		}
		if status == domain.Approved {
			if !student.VerifiedStudent() || !app.Grade.Valid() {
				return domain.ErrConflict
			}
			reader := tx.Catalog()
			current, err := reader.GetProgramFaculty(ctx, student.ProgramID)
			if err != nil {
				return err
			}
			if current != faculty {
				return domain.ErrConflict
			}
			if err := subjectEligible(ctx, reader, student.ProgramID, app.SubjectID); err != nil {
				return err
			}
		}
		return tx.Decide(ctx, app, actor.ID, status, reason)
	})
}

func (s *Service) MyApplications(ctx context.Context, actor identity.Principal, p domain.Page) ([]domain.Application, error) {
	if !actor.Active() || !actor.Student {
		return nil, domain.ErrForbidden
	}
	if !validPage(p) {
		return nil, domain.ErrInvalid
	}
	p.Limit++
	return s.repo.Applications(ctx, actor.ID, false, "", p)
}

func (s *Service) CoordinatorApplications(ctx context.Context, actor identity.Principal, status domain.Status, p domain.Page) ([]domain.Application, error) {
	if !actor.Coordinator() {
		return nil, domain.ErrForbidden
	}
	if !status.Valid() || !validPage(p) {
		return nil, domain.ErrInvalid
	}
	p.Limit++
	return s.repo.Applications(ctx, actor.ID, true, status, p)
}

func (s *Service) Application(ctx context.Context, actor identity.Principal, id string) (domain.Application, error) {
	if !actor.Active() || (!actor.Student && !actor.Coordinator()) {
		return domain.Application{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Application{}, domain.ErrInvalid
	}
	// The repository includes authorized faculty access, without selecting grades.
	return s.repo.OwnApplication(ctx, actor.ID, id)
}

func (s *Service) ReviewDetail(ctx context.Context, actor identity.Principal, id string) (domain.ReviewDetail, error) {
	if !actor.Coordinator() {
		return domain.ReviewDetail{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.ReviewDetail{}, domain.ErrInvalid
	}
	return s.repo.ReviewDetail(ctx, actor.ID, id)
}

func (s *Service) Profile(ctx context.Context, actor identity.Principal, id string, own bool) (domain.Profile, error) {
	if !actor.VerifiedStudent() {
		return domain.Profile{}, domain.ErrForbidden
	}
	if own {
		id = actor.ID
	}
	if !uuid.MatchString(id) {
		return domain.Profile{}, domain.ErrInvalid
	}
	return s.repo.Profile(ctx, id, own)
}

func (s *Service) Search(ctx context.Context, actor identity.Principal, f domain.Search) ([]domain.Match, error) {
	if !actor.VerifiedStudent() {
		return nil, domain.ErrForbidden
	}
	if !uuid.MatchString(f.SubjectID) || (f.FacultyID != "" && !uuid.MatchString(f.FacultyID)) ||
		(f.ProgramID != "" && !uuid.MatchString(f.ProgramID)) || !validPage(f.Page) ||
		(f.Level != "" && !catalogdomain.DegreeLevel(f.Level).Valid()) {
		return nil, domain.ErrInvalid
	}
	if err := subjectEligible(ctx, s.catalog, actor.ProgramID, f.SubjectID); err != nil {
		return nil, err
	}
	f.Limit++
	return s.repo.Search(ctx, actor.ID, f)
}

func (s *Service) GetApprovedOffering(ctx context.Context, tutor, subject string) (domain.Offering, error) {
	if !uuid.MatchString(tutor) || !uuid.MatchString(subject) {
		return domain.Offering{}, domain.ErrInvalid
	}
	return s.repo.ApprovedOffering(ctx, tutor, subject)
}

// RevalidateTutorEligibility withdraws ineligible offerings without modifying existing sessions.
func (s *Service) RevalidateTutorEligibility(ctx context.Context, studentID string) error {
	if !uuid.MatchString(studentID) {
		return domain.ErrInvalid
	}
	return s.repo.InTransaction(ctx, func(tx Transaction) error {
		student, err := tx.Student(ctx, studentID)
		if err != nil {
			return err
		}
		offerings, err := tx.ActiveOfferings(ctx, studentID)
		if err != nil {
			return err
		}
		for _, offering := range offerings {
			ok, err := tx.Catalog().SubjectBelongsToProgram(ctx, student.ProgramID, offering.SubjectID)
			if err != nil {
				return err
			}
			if !ok {
				if err := tx.Deactivate(ctx, offering.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
