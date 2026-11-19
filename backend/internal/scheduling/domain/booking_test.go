package domain

import (
	"errors"
	"testing"
	"time"
)

func TestExpirationBoundaries(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		notice, delay time.Duration
		valid         bool
	}{
		{2*time.Hour + 59*time.Minute, 0, false},
		{3 * time.Hour, time.Hour, true},
		{24 * time.Hour, time.Hour, true},
		{25 * time.Hour, time.Hour, true},
		{25*time.Hour + time.Second, time.Hour + time.Second, true},
		{48 * time.Hour, 24 * time.Hour, true},
		{72 * time.Hour, 48 * time.Hour, true},
		{96 * time.Hour, 48 * time.Hour, true},
	} {
		got, err := Expiration(now, now.Add(tc.notice))
		if tc.valid && (err != nil || !got.Equal(now.Add(tc.delay))) ||
			!tc.valid && !errors.Is(err, ErrBookingInvalid) {
			t.Errorf("notice %s: %v %v", tc.notice, got, err)
		}
	}
}

func TestBookingValidation(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	r := Request{StartsAt: now, Minutes: 60, Mode: Online, Purpose: "HOMEWORK", Description: "Practice"}
	w := Window{StartsAt: now, EndsAt: now.Add(time.Hour), Mode: Both}
	if r.Validate() != nil || !r.Fits(w) {
		t.Fatal("valid booking rejected")
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.Purpose = Introduction },
		func(r *Request) { r.Minutes = 30 },
		func(r *Request) { r.Purpose = "INVALID" },
		func(r *Request) { r.Mode = Both },
		func(r *Request) { r.Description = " " },
	} {
		bad := r
		change(&bad)
		if !errors.Is(bad.Validate(), ErrBookingInvalid) {
			t.Fatal(bad)
		}
	}
	r.Purpose, r.Minutes = Introduction, 30
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	w.Mode = Onsite
	if r.Fits(w) {
		t.Fatal("wrong slot mode accepted")
	}
	if (Meeting{}).Validate(Online) == nil || (Meeting{Link: "javascript:alert(1)"}).Validate(Online) == nil ||
		(Meeting{Link: "https://example.edu/meet"}).Validate(Online) != nil ||
		(Meeting{Room: "A 204"}).Validate(Onsite) != nil || (Meeting{}).Validate(Onsite) == nil {
		t.Fatal("meeting details do not enforce session mode")
	}
	if _, err := Reason("   "); !errors.Is(err, ErrBookingInvalid) {
		t.Fatal(err)
	}
	if _, err := Reason("Schedule changed"); err != nil {
		t.Fatal(err)
	}
}

func TestBookingTransitionsAndLateCancellation(t *testing.T) {
	now := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	s := Session{Status: Requested, ExpiresAt: now.Add(time.Hour)}
	for _, to := range []Status{Accepted, Declined, Cancelled} {
		if err := s.Transition(to, now); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(s.Transition(Accepted, s.ExpiresAt), ErrRequestExpired) ||
		!errors.Is(s.Transition(Declined, s.ExpiresAt), ErrRequestExpired) ||
		s.Transition(Expired, s.ExpiresAt) != nil || s.Transition(Expired, now) == nil {
		t.Fatal("invalid deadline handling")
	}
	s.Status = Accepted
	if s.Transition(Cancelled, now) != nil || s.Transition(Expired, s.ExpiresAt) == nil || s.Transition(Completed, now) != nil {
		t.Fatal("invalid accepted-session transitions")
	}
	for _, status := range []Status{Declined, Expired, Cancelled, Completed, NoShow} {
		s.Status = status
		for _, to := range []Status{Requested, Accepted, Declined, Expired, Cancelled} {
			if s.Transition(to, s.ExpiresAt) == nil {
				t.Fatalf("%s -> %s", status, to)
			}
		}
	}
	if LateCancellation(now, now.Add(24*time.Hour)) || !LateCancellation(now, now.Add(24*time.Hour-time.Second)) {
		t.Fatal("24 hour boundary should be strict")
	}
}
