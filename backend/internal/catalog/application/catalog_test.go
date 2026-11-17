package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
)

type queryRepo struct {
	Repository
	query         string
	limit, offset int
}

func (r *queryRepo) SearchSubjects(_ context.Context, q string, limit, offset int) ([]domain.Subject, error) {
	r.query, r.limit, r.offset = q, limit, offset
	return []domain.Subject{{ID: "one"}, {ID: "two"}}, nil
}

func TestSubjectPage(t *testing.T) {
	ctx := context.Background()
	repo := &queryRepo{}
	s := New(repo)
	for _, tc := range []struct {
		filter SubjectFilter
		err    error
	}{
		{SubjectFilter{Limit: 101}, domain.ErrInvalidPagination},
		{SubjectFilter{Limit: 1, Offset: -1}, domain.ErrInvalidPagination},
		{SubjectFilter{Limit: 1, Offset: 100001}, domain.ErrInvalidPagination},
		{SubjectFilter{Limit: 1, Query: strings.Repeat("é", 101)}, domain.ErrInvalidSearch},
		{SubjectFilter{Limit: 1, Query: "a\x00"}, domain.ErrInvalidSearch},
	} {
		if _, err := s.SearchSubjects(ctx, tc.filter); !errors.Is(err, tc.err) {
			t.Errorf("SearchSubjects returned %v, want %v", err, tc.err)
		}
	}
	if repo.limit != 0 {
		t.Fatal("invalid search reached the repository")
	}
	p, err := s.SearchSubjects(ctx, SubjectFilter{Query: " Algo ", Limit: 1, Offset: 3})
	if err != nil || !p.HasMore || len(p.Subjects) != 1 || p.Subjects[0].ID != "one" {
		t.Fatalf("incorrect page: %+v, %v", p, err)
	}
	if repo.query != "Algo" || repo.limit != 2 || repo.offset != 3 {
		t.Fatalf("incorrect query: %q, %d, %d", repo.query, repo.limit, repo.offset)
	}
}

func TestCatalogValidation(t *testing.T) {
	ctx := context.Background()
	s := New(nil) // Invalid arguments must stop before reaching the repository.
	const id = "00000000-0000-0000-0000-000000000001"
	if _, err := s.ListPrograms(ctx, id, "PHD"); !errors.Is(err, domain.ErrInvalidDegreeLevel) {
		t.Fatalf("invalid level: %v", err)
	}
	if _, err := s.SubjectBelongsToProgram(ctx, id, "wrong"); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("invalid subject ID: %v", err)
	}
	if _, err := s.GetProgramFaculty(ctx, "wrong"); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("invalid program ID: %v", err)
	}
}
