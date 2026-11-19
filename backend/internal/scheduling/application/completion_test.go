package application

import (
	"context"
	"errors"
	"testing"
	"time"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

func TestCompleteUsesTutorOwnershipAndTrustedClock(t *testing.T) {
	end := time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC)
	now := end.Add(-time.Microsecond)
	actor := testActor()
	other := actor
	other.ID = testSemester
	tx := &fakeBookingTx{
		people:  map[string]identity.Principal{actor.ID: actor, other.ID: other},
		session: domain.Session{ID: testRule, TutorID: actor.ID, StudentID: other.ID, Status: domain.Accepted, EndsAt: end},
	}
	s := NewBooking(fakeBookings{tx: tx}, func() time.Time { return now })
	ctx := context.Background()
	if _, err := s.Complete(ctx, other, testRule); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Complete(ctx, actor, testRule); !errors.Is(err, domain.ErrBookingState) {
		t.Fatal(err)
	}
	now = end
	suspended := actor
	suspended.Suspended = true
	tx.people[actor.ID] = suspended
	if _, err := s.Complete(ctx, actor, testRule); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal(err)
	}
	tx.people[actor.ID] = actor
	tx.session.Status = domain.Cancelled
	if _, err := s.Complete(ctx, actor, testRule); !errors.Is(err, domain.ErrBookingState) {
		t.Fatal(err)
	}
	tx.session.Status = domain.Accepted
	got, err := s.Complete(ctx, actor, testRule)
	if err != nil || got.Status != domain.Completed || got.CompletedAt == nil || !got.CompletedAt.Equal(end) || got.CompletedBy != actor.ID {
		t.Fatal(got, err)
	}
	if _, err := s.Complete(ctx, actor, testRule); !errors.Is(err, domain.ErrBookingState) {
		t.Fatal(err)
	}
	if len(tx.events) != 1 || tx.events[0].From != domain.Accepted || tx.events[0].To != domain.Completed || tx.events[0].Actor != actor.ID {
		t.Fatal(tx.events)
	}
}
