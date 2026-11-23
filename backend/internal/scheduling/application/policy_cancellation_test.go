package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type policyWriter struct {
	session               domain.Session
	events                []Event
	failUpdate, failEvent error
}

func (w *policyWriter) UpdateSession(_ context.Context, s domain.Session, _ domain.Status) error {
	w.session = s
	return w.failUpdate
}
func (w *policyWriter) RecordEvent(_ context.Context, e Event) error {
	w.events = append(w.events, e)
	return w.failEvent
}
func TestPolicyCancellationWritesOneExplicitEvent(t *testing.T) {
	now := time.Date(2028, 1, 1, 1, 2, 3, 123456789, time.FixedZone("test", 3600))
	input := domain.Session{ID: "session", StudentID: "student", TutorID: "tutor", Status: domain.Accepted}
	w := &policyWriter{}
	out, err := CancelForPolicy(context.Background(), w, input, "coordinator", "  Reason  ", now)
	if err != nil || out.Status != domain.Cancelled || out.Late || len(w.events) != 1 {
		t.Fatal(out, err, w.events)
	}
	e := w.events[0]
	if e.CancellationOrigin != domain.CoordinatorPolicyCancellation || e.From != domain.Accepted || e.To != domain.Cancelled || e.Actor != "coordinator" || e.Reason != "Reason" || len(e.Recipients) != 2 || e.Recipients[0] != "student" || e.Recipients[1] != "tutor" || !e.At.Equal(*out.CancelledAt) || e.At.Location() != time.UTC || e.At.Nanosecond()%1000 != 0 {
		t.Fatal(e)
	}
	failure := errors.New("fixture")
	for _, stage := range []string{"update", "event"} {
		w = &policyWriter{}
		if stage == "update" {
			w.failUpdate = failure
		} else {
			w.failEvent = failure
		}
		out, err = CancelForPolicy(context.Background(), w, input, "coordinator", "Reason", now)
		if !errors.Is(err, failure) || out.ID != "" {
			t.Fatal(out, err)
		}
		if stage == "update" && len(w.events) != 0 {
			t.Fatal("event after failed update")
		}
	}
}
