package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type testRepo struct {
	Repository
	details domain.StudentDetails
	hash    string
	reviews int
}

func (r *testRepo) Register(_ context.Context, d domain.StudentDetails, hash string) (string, error) {
	r.details, r.hash = d, hash
	return "verification", nil
}

func (r *testRepo) Review(context.Context, string, string, domain.Status, string) error {
	r.reviews++
	return nil
}

type testPasswords struct{}

func (testPasswords) Hash(context.Context, string) (string, error)         { return "test-hash", nil }
func (testPasswords) Verify(context.Context, string, string) (bool, error) { return true, nil }

func TestValidationBeforePersistence(t *testing.T) {
	ctx := context.Background()
	repo := &testRepo{}
	s, err := New(ctx, repo, nil, testPasswords{}, Config{Domains: []string{"uni.example"},
		SessionTTL: time.Hour, ActivationTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-0000-0000-000000000001"
	d := domain.StudentDetails{Name: " Alice ", Email: "Alice@uni.example.evil", StudentNumber: " A1 ", ProgramID: id}
	if _, err := s.Register(ctx, d, "long password"); !errors.Is(err, domain.ErrInvalid) || repo.hash != "" {
		t.Fatal("lookalike university domain reached persistence")
	}
	d.Email = " Alice@UNI.Example "
	if _, err := s.Register(ctx, d, "long password"); err != nil ||
		repo.details.Email != "alice@uni.example" || repo.details.Name != "Alice" ||
		repo.details.StudentNumber != "A1" || repo.hash != "test-hash" {
		t.Fatalf("registration did not normalize and hash before writing: %v", err)
	}
	p := domain.Principal{ID: id, CoordinatorFaculty: id}
	if err := s.Review(ctx, p, id, domain.Rejected, "   "); !errors.Is(err, domain.ErrInvalid) || repo.reviews != 0 {
		t.Fatal("blank rejection reason reached persistence")
	}
	if err := s.Review(ctx, domain.Principal{ID: id, Student: true}, id, domain.Approved, ""); !errors.Is(err, domain.ErrForbidden) || repo.reviews != 0 {
		t.Fatal("student reached coordinator persistence")
	}
	if _, err := s.Resubmit(ctx, domain.Principal{ID: id, Student: true, Verification: domain.Approved}, d); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("approved student could resubmit")
	}
}
