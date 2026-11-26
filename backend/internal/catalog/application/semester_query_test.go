package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

type semesterRepo struct {
	CalendarTx
	called   bool
	readOnly bool
	rows     []domain.Semester
	err      error
}

func (r *semesterRepo) CalendarTransaction(ctx context.Context, readOnly bool, work func(CalendarTx) error) error {
	r.called = true
	r.readOnly = readOnly
	return work(r)
}
func (r *semesterRepo) List(context.Context) ([]domain.Semester, error) { return r.rows, r.err }

func TestSemesterQueryPermissionsAndClock(t *testing.T) {
	for _, p := range []identity.Principal{
		{}, {ID: "student", Student: true, Verification: identity.Approved},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Pending},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Rejected},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved, Suspended: true},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved, Closed: true},
		{ID: "coordinator", CoordinatorFaculty: "faculty"},
	} {
		s := NewCalendar(nil, nil)
		if _, err := s.ListForTutor(context.Background(), p); !errors.Is(err, identity.ErrForbidden) {
			t.Fatal("unauthorized query reached persistence", err)
		}
	}
	now := time.Date(2027, 1, 1, 12, 0, 0, 0, time.FixedZone("local", 3600))
	r := &semesterRepo{}
	s := NewCalendar(r, func() time.Time { return now })
	actor := identity.Principal{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved}
	rows, err := s.ListForTutor(context.Background(), actor)
	if err != nil || rows == nil || len(rows) != 0 || !r.called || !r.readOnly {
		t.Fatal("invalid clock or empty collection", err)
	}
	r.err = context.Canceled
	if _, err := s.ListForTutor(context.Background(), actor); !errors.Is(err, context.Canceled) {
		t.Fatal("query error hidden")
	}
}

func TestTutorCalendarReusesListingWithoutChangingCLI(t *testing.T) {
	now := time.Date(2027, 1, 1, 12, 0, 0, 0, time.FixedZone("local", 3600))
	repo := &semesterRepo{rows: []domain.Semester{
		{ID: "past", StartsAt: now.Add(-48 * time.Hour), EndsAt: now.Add(-24 * time.Hour)},
		{ID: "exact-end", StartsAt: now.Add(-24 * time.Hour), EndsAt: now},
		{ID: "current", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		{ID: "future", StartsAt: now.Add(time.Hour), EndsAt: now.Add(24 * time.Hour)},
	}}
	service := NewCalendar(repo, func() time.Time { return now })
	actor := identity.Principal{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved}
	rows, err := service.ListForTutor(context.Background(), actor)
	if err != nil || len(rows) != 2 || rows[0].ID != "current" || rows[1].ID != "future" {
		t.Fatal("incorrect filtered listing", rows, err)
	}
	if rows[0].StartsAt.Location() != time.UTC || rows[1].EndsAt.Location() != time.UTC {
		t.Fatal("timestamps not normalized")
	}
	receipt, err := service.List(context.Background())
	if err != nil || len(receipt.Semesters) != 4 || !receipt.ReadOnly || !repo.readOnly || receipt.Operation != "list" || receipt.Outcome != "inspected" {
		t.Fatal("CLI listing changed", err)
	}
	if receipt.Semesters[0].StartsAt.Location() == time.UTC {
		t.Fatal("HTTP filtering mutated shared calendar entries")
	}
}
