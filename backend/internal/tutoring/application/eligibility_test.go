package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type eligibilityFake struct {
	Transaction
	active                            map[string]string
	eligible                          map[string]bool
	studentErr, programErr, listErr   error
	catalogFailure, deactivateFailure string
	changed                           []string
	student                           string
}

func (f *eligibilityFake) Student(_ context.Context, id string) (identity.Principal, error) {
	f.student = id
	return verified(), f.studentErr
}
func (f *eligibilityFake) Catalog() catalog.AcademicCatalog { return f }
func (f *eligibilityFake) GetProgramFaculty(context.Context, string) (string, error) {
	return facultyID, f.programErr
}
func (f *eligibilityFake) SubjectBelongsToProgram(_ context.Context, program, subject string) (bool, error) {
	if program != programID {
		return false, errors.New("wrong current program")
	}
	if subject == f.catalogFailure {
		return false, errEligibilityFixture
	}
	return f.eligible[subject], nil
}
func (f *eligibilityFake) ActiveOfferings(_ context.Context, id string) ([]domain.Offering, error) {
	if id != studentID {
		return nil, errors.New("wrong offering owner")
	}
	var out []domain.Offering
	// Stable order makes the later failure exercise partial work.
	for _, id := range []string{"one", "two"} {
		if subject, ok := f.active[id]; ok {
			out = append(out, domain.Offering{ID: id, SubjectID: subject})
		}
	}
	return out, f.listErr
}
func (f *eligibilityFake) Deactivate(_ context.Context, id string) error {
	if id == f.deactivateFailure {
		return errEligibilityFixture
	}
	delete(f.active, id)
	f.changed = append(f.changed, id)
	return nil
}

var errEligibilityFixture = errors.New("eligibility fixture failure")

type eligibilityRepo struct {
	Repository
	tx     *eligibilityFake
	called bool
}

func (r *eligibilityRepo) InTransaction(_ context.Context, work func(Transaction) error) error {
	r.called = true
	before := map[string]string{}
	for k, v := range r.tx.active {
		before[k] = v
	}
	err := work(r.tx)
	if err != nil {
		r.tx.active = before
	}
	return err
}
func TestEligibilityReconciliation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		active   map[string]string
		eligible map[string]bool
		want     int
	}{
		{"no profile or active offerings", nil, nil, 0},
		{"all eligible", map[string]string{"one": "a", "two": "b"}, map[string]bool{"a": true, "b": true}, 0},
		{"shared subject stays eligible", map[string]string{"one": "a", "two": "b"}, map[string]bool{"a": true}, 1},
		{"all ineligible", map[string]string{"one": "a", "two": "b"}, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &eligibilityFake{active: tc.active, eligible: tc.eligible}
			r := &eligibilityRepo{tx: f}
			s := New(r, nil, nil)
			if err := s.RevalidateTutorEligibility(context.Background(), studentID); err != nil {
				t.Fatal(err)
			}
			if len(f.changed) != tc.want || f.student != studentID {
				t.Fatalf("changes %v, student %s", f.changed, f.student)
			}
			if err := s.RevalidateTutorEligibility(context.Background(), studentID); err != nil || len(f.changed) != tc.want {
				t.Fatalf("not idempotent: %v %v", f.changed, err)
			}
			f.eligible = map[string]bool{"a": true, "b": true}
			if err := s.RevalidateTutorEligibility(context.Background(), studentID); err != nil || len(f.changed) != tc.want {
				t.Fatalf("unexpected reactivation: %v", err)
			}
		})
	}
}
func TestEligibilityFailures(t *testing.T) {
	for _, name := range []string{"student", "program", "list", "catalog", "deactivation"} {
		t.Run(name, func(t *testing.T) {
			f := &eligibilityFake{active: map[string]string{"one": "a", "two": "b"}}
			switch name {
			case "student":
				f.studentErr = domain.ErrNotFound
			case "program":
				f.programErr = errEligibilityFixture
			case "list":
				f.listErr = errEligibilityFixture
			case "catalog":
				f.catalogFailure = "b"
			case "deactivation":
				f.deactivateFailure = "two"
			}
			before := map[string]string{"one": "a", "two": "b"}
			r := &eligibilityRepo{tx: f}
			s := New(r, nil, nil)
			err := s.RevalidateTutorEligibility(context.Background(), studentID)
			want := errEligibilityFixture
			if name == "student" {
				want = domain.ErrNotFound
			}
			if !errors.Is(err, want) || !reflect.DeepEqual(before, f.active) {
				t.Fatalf("rollback/error: %v %+v", err, f.active)
			}
		})
	}
	f := &eligibilityFake{}
	r := &eligibilityRepo{tx: f}
	s := New(r, nil, nil)
	if err := s.RevalidateTutorEligibility(context.Background(), "bad"); !errors.Is(err, domain.ErrInvalid) || r.called {
		t.Fatal(err, r.called)
	}
	if err := RevalidateTutorEligibilityInTransaction(context.Background(), f, "bad"); !errors.Is(err, domain.ErrInvalid) || f.student != "" {
		t.Fatal(err)
	}
}
