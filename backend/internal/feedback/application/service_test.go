package application

import (
	"context"
	"errors"
	"testing"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

const learner = "00000000-0000-0000-0000-000000000001"
const tutor = "00000000-0000-0000-0000-000000000002"
const session = "00000000-0000-0000-0000-000000000003"

type fakeWrite struct {
	session   scheduling.Session
	current   identity.Principal
	insertErr error
	writes    int
}

func (f *fakeWrite) Student(context.Context, string) (identity.Principal, error) {
	return f.current, nil
}
func (f *fakeWrite) LockStudentSession(context.Context, string, string) (scheduling.Session, error) {
	return f.session, nil
}
func (f *fakeWrite) Insert(_ context.Context, _ scheduling.Session, i domain.Input) (domain.Entry, error) {
	f.writes++
	return domain.Entry{Rating: i.Rating, Comment: i.Comment}, f.insertErr
}

type fakeRepo struct {
	Repository
	tx *fakeWrite
}

func (f fakeRepo) Transact(_ context.Context, work func(WriteTx) error) error { return work(f.tx) }
func (f fakeRepo) Statistics(context.Context, []string) (map[string]domain.Statistics, error) {
	return nil, nil
}

func TestWriteAuthorizationAndOutcome(t *testing.T) {
	ctx := context.Background()
	actor := identity.Principal{ID: learner, Student: true, Verification: identity.Approved}
	tx := &fakeWrite{session: scheduling.Session{ID: session, StudentID: learner, TutorID: tutor, Status: scheduling.Completed}, current: actor}
	s := New(fakeRepo{tx: tx}, nil)
	in := domain.Input{Rating: 4}
	for _, status := range []scheduling.Status{scheduling.Accepted, scheduling.Cancelled, scheduling.NoShow} {
		tx.session.Status = status
		if _, err := s.Submit(ctx, actor, session, in); !errors.Is(err, domain.ErrIneligible) {
			t.Fatal(err)
		}
	}
	tx.session.Status = scheduling.Completed
	for _, id := range []string{tutor, session} {
		wrong := actor
		wrong.ID = id
		if _, err := s.Submit(ctx, wrong, session, in); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal(err)
		}
	}
	tx.current.Suspended = true
	if _, err := s.Submit(ctx, actor, session, in); !errors.Is(err, domain.ErrForbidden) || tx.writes != 0 {
		t.Fatal(err)
	}
	tx.current = actor
	if got, err := s.Submit(ctx, actor, session, in); err != nil || got.Rating != 4 {
		t.Fatal(got, err)
	}
	tx.insertErr = domain.ErrConflict
	if _, err := s.Submit(ctx, actor, session, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
}

func TestEmptySummaryAndCoordinatorAuthorization(t *testing.T) {
	ctx := context.Background()
	s := New(fakeRepo{}, nil)
	got, err := s.RatingSummaries(ctx, []string{tutor})
	if err != nil || got[tutor].RatingCount != 0 || got[tutor].AverageRating != nil {
		t.Fatal(got, err)
	}
	actor := identity.Principal{ID: learner, Student: true, Verification: identity.Approved}
	if _, err := s.CoordinatorFeedback(ctx, actor, tutor, Page{Limit: 25}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
}
