package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPolicyCancellationStateAndReason(t *testing.T) {
	now := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, status := range []Status{Requested, Accepted, Declined, Expired, Cancelled, Completed, NoShow} {
		for _, at := range []time.Time{now, now.Add(10 * time.Hour)} {
			s := Session{Status: status, RequestedAt: now, StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), ExpiresAt: now.Add(time.Minute), Late: true}
			got, err := s.CancelForPolicy("coordinator", "  Révision de la règle  ", at)
			if status != Requested && status != Accepted {
				if !errors.Is(err, ErrBookingState) {
					t.Fatal(status, err)
				}
				continue
			}
			if err != nil || got.Status != Cancelled || got.Late || got.CancellationReason != "Révision de la règle" || got.CancelledBy != "coordinator" || !got.CancelledAt.Equal(at) {
				t.Fatal(got, err)
			}
			if s.Status != status {
				t.Fatal("input mutated")
			}
		}
	}
	s := Session{Status: Requested, RequestedAt: now}
	for _, reason := range []string{"", " \t\n\u2003", string([]byte{0xff}), "a\x00b", strings.Repeat("é", 1001)} {
		if _, err := s.CancelForPolicy("coordinator", reason, now); !errors.Is(err, ErrBookingInvalid) {
			t.Fatal(reason, err)
		}
	}
	if _, err := s.CancelForPolicy("coordinator", strings.Repeat("é", 1000), now); err != nil {
		t.Fatal(err)
	}
}
