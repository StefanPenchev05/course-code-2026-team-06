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
	called bool
	at     time.Time
	err    error
}

func (r *semesterRepo) ListAvailableSemesters(_ context.Context, at time.Time) ([]domain.Semester, error) {
	r.called = true
	r.at = at
	return nil, r.err
}

func TestSemesterQueryPermissionsAndClock(t *testing.T) {
	for _, p := range []identity.Principal{
		{}, {ID: "student", Student: true, Verification: identity.Approved},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Pending},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Rejected},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved, Suspended: true},
		{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved, Closed: true},
		{ID: "coordinator", CoordinatorFaculty: "faculty"},
	} {
		s := NewSemesters(nil, nil)
		if _, err := s.List(context.Background(), p); !errors.Is(err, identity.ErrForbidden) {
			t.Fatal("unauthorized query reached persistence", err)
		}
	}
	now := time.Date(2027, 1, 1, 12, 0, 0, 0, time.FixedZone("local", 3600))
	r := &semesterRepo{}
	s := NewSemesters(r, func() time.Time { return now })
	actor := identity.Principal{ID: "tutor", Student: true, Tutor: true, Verification: identity.Approved}
	rows, err := s.List(context.Background(), actor)
	if err != nil || rows == nil || len(rows) != 0 || !r.called || !r.at.Equal(now) || r.at.Location() != time.UTC {
		t.Fatal("invalid clock or empty collection", err)
	}
	r.err = context.Canceled
	if _, err := s.List(context.Background(), actor); !errors.Is(err, context.Canceled) {
		t.Fatal("query error hidden")
	}
}
