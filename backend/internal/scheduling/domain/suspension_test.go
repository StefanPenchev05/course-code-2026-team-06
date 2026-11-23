package domain

import (
	"testing"
	"time"
)

func TestSuspensionSessionBoundary(t *testing.T) {
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []Status{Requested, Accepted, Cancelled, Completed, Expired, Declined, NoShow} {
		for _, delta := range []time.Duration{-time.Microsecond, 0, time.Hour} {
			for _, target := range []string{"student", "tutor", "outsider"} {
				s := Session{ID: "s", StudentID: "student", TutorID: "tutor", Status: status, StartsAt: at.Add(delta), RequestedAt: at.Add(-5 * time.Hour), Late: true}
				got, err := s.CancelForSuspension(target, "coordinator", at)
				valid := (status == Requested || status == Accepted) && delta >= 0 && target != "outsider"
				if valid != (err == nil) {
					t.Fatal(status, delta, target, err)
				}
				if valid && (got.Late || got.CancellationReason != SuspensionSessionReason || got.CancelledBy != "coordinator" || !got.CancelledAt.Equal(at)) {
					t.Fatal(got)
				}
			}
		}
	}
}
