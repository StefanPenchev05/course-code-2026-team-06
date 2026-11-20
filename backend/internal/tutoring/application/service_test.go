package application

import (
	"context"
	"errors"
	"testing"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

const (
	studentID = "00000000-0000-0000-0000-000000000001"
	subjectID = "00000000-0000-0000-0000-000000000002"
	programID = "00000000-0000-0000-0000-000000000003"
	facultyID = "00000000-0000-0000-0000-000000000004"
	appID     = "00000000-0000-0000-0000-000000000005"
)

type academic struct{ subjects map[string]bool }

func (a academic) GetProgramFaculty(context.Context, string) (string, error) { return facultyID, nil }
func (a academic) SubjectBelongsToProgram(_ context.Context, program, subject string) (bool, error) {
	return program == programID && a.subjects[subject], nil
}

type txFake struct {
	Transaction
	principal       identity.Principal
	academic        academic
	active          bool
	created         Submission
	createErr       error
	review          ReviewRecord
	decision        domain.Status
	offeringChanges []string
}

func (f *txFake) Student(context.Context, string) (identity.Principal, error) {
	return f.principal, nil
}
func (f *txFake) Catalog() catalog.AcademicCatalog                             { return f.academic }
func (f *txFake) ActiveOffering(context.Context, string, string) (bool, error) { return f.active, nil }
func (f *txFake) CreateApplication(_ context.Context, user, faculty string, in Submission) (string, error) {
	if user != studentID || faculty != facultyID {
		return "", errors.New("incorrect application routing")
	}
	f.created = in
	return appID, f.createErr
}
func (f *txFake) CoordinatorFaculty(context.Context, string) (string, error) { return facultyID, nil }
func (f *txFake) ReviewRecord(context.Context, string, string, bool) (ReviewRecord, error) {
	return f.review, nil
}
func (f *txFake) Decide(_ context.Context, _ ReviewRecord, _ string, status domain.Status, _ string) error {
	f.review.Status, f.decision = status, status
	return nil
}
func (f *txFake) ActiveOfferings(context.Context, string) ([]domain.Offering, error) {
	return []domain.Offering{{ID: "eligible", SubjectID: subjectID}, {ID: "ineligible", SubjectID: appID}}, nil
}
func (f *txFake) Deactivate(_ context.Context, id, program, faculty string) error {
	if program != programID || faculty != facultyID {
		return errors.New("incorrect withdrawal audit scope")
	}
	f.offeringChanges = append(f.offeringChanges, id)
	return nil
}

type repoFake struct {
	Repository
	tx *txFake
}

func (r repoFake) InTransaction(_ context.Context, work func(Transaction) error) error {
	return work(r.tx)
}

func verified() identity.Principal {
	return identity.Principal{ID: studentID, Student: true, ProgramID: programID, Verification: identity.Approved}
}

func TestApplyValidationAndRouting(t *testing.T) {
	ctx := context.Background()
	f := &txFake{principal: verified(), academic: academic{map[string]bool{subjectID: true}}}
	s := New(repoFake{tx: f}, nil, nil)
	input := Submission{SubjectID: subjectID, Grade: 1750, Description: " algorithms "}
	unverified := verified()
	unverified.Verification = identity.Pending
	suspended := verified()
	suspended.Suspended = true
	for _, actor := range []identity.Principal{unverified, suspended} {
		if _, err := s.Apply(ctx, actor, input); !errors.Is(err, domain.ErrForbidden) {
			t.Fatal(err)
		}
	}
	wrong := input
	wrong.SubjectID = appID
	if _, err := s.Apply(ctx, verified(), wrong); !errors.Is(err, domain.ErrIneligibleSubject) {
		t.Fatal(err)
	}
	id, err := s.Apply(ctx, verified(), input)
	if err != nil || id != appID || f.created.Description != "algorithms" || f.created.Grade != 1750 {
		t.Fatalf("%s %v %+v", id, err, f.created)
	}
	f.active = true
	if _, err := s.Apply(ctx, verified(), input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	f.active = false
	f.createErr = domain.ErrConflict
	if _, err := s.Apply(ctx, verified(), input); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
}

func TestReviewAndRevalidation(t *testing.T) {
	ctx := context.Background()
	f := &txFake{principal: verified(), academic: academic{map[string]bool{subjectID: true}},
		review: ReviewRecord{StudentID: studentID, SubjectID: subjectID, Status: domain.Pending, Grade: 1750}}
	s := New(repoFake{tx: f}, nil, nil)
	reviewer := identity.Principal{ID: facultyID, CoordinatorFaculty: facultyID}
	if err := s.Review(ctx, reviewer, appID, domain.Rejected, " "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Review(ctx, reviewer, appID, domain.Approved, ""); err != nil || f.decision != domain.Approved {
		t.Fatal(err)
	}
	if err := s.Review(ctx, reviewer, appID, domain.Approved, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	f.review.Status = domain.Pending
	if err := s.Review(ctx, reviewer, appID, domain.Rejected, "Needs evidence"); err != nil || f.decision != domain.Rejected {
		t.Fatal(err)
	}
	if err := s.RevalidateTutorEligibility(ctx, studentID); err != nil || len(f.offeringChanges) != 1 || f.offeringChanges[0] != "ineligible" {
		t.Fatalf("revalidation: %v %v", f.offeringChanges, err)
	}
}
