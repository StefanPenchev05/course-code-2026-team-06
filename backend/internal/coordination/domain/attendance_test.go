package domain

import (
	"errors"
	"testing"
	"time"

	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

func TestReportParticipantsAndWindow(t *testing.T) {
	end := time.Date(2027, 1, 12, 15, 0, 0, 0, time.UTC)
	session := scheduling.Session{StudentID: "student", TutorID: "tutor", EndsAt: end, Status: scheduling.Accepted}
	for _, tc := range []struct {
		actor, want string
		at          time.Time
		err         error
	}{
		{"student", "tutor", end, nil},
		{"tutor", "student", end.Add(168 * time.Hour), nil},
		{"student", "", end.Add(-time.Microsecond), ErrWindow},
		{"tutor", "", end.Add(168*time.Hour + time.Microsecond), ErrWindow},
		{"unrelated", "", end, ErrNotFound},
	} {
		absent, err := AbsentParticipant(session, tc.actor, tc.at)
		if absent != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s at %s: %q %v", tc.actor, tc.at, absent, err)
		}
	}
	session.Status = scheduling.Completed
	if _, err := AbsentParticipant(session, "student", end); err != nil {
		t.Fatal(err)
	}
	session.Status = scheduling.Cancelled
	if _, err := AbsentParticipant(session, "student", end); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	session.Status = scheduling.Accepted
	session.TutorID = session.StudentID
	if _, err := AbsentParticipant(session, "student", end); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPendingDecisionsAndConflictingOutcomes(t *testing.T) {
	r := Report{Status: Pending}
	for _, status := range []scheduling.Status{scheduling.Accepted, scheduling.Completed, scheduling.NoShow} {
		if err := r.ValidateDecision(Confirmed, scheduling.Session{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.ValidateDecision(Confirmed, scheduling.Session{Status: scheduling.Cancelled}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err := r.ValidateDecision(Rejected, scheduling.Session{Status: scheduling.Cancelled}); err != nil {
		t.Fatal(err)
	}
	for _, prior := range []Status{Confirmed, Rejected} {
		r.Status = prior
		for _, decision := range []Status{Confirmed, Rejected} {
			if err := r.ValidateDecision(decision, scheduling.Session{Status: scheduling.NoShow}); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
		}
	}
}
