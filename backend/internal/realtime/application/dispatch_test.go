package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeOutbox struct {
	claim         Claim
	claimed       bool
	sent, retries int
	delay         time.Duration
	ackErr        error
}

func (s *fakeOutbox) Claim(context.Context, time.Duration) (Claim, bool, error) {
	if s.claimed {
		return Claim{}, false, nil
	}
	s.claimed = true
	return s.claim, true, nil
}
func (s *fakeOutbox) Dispatched(_ context.Context, c Claim) error {
	if c.ID != s.claim.ID || c.Token != s.claim.Token {
		panic("claim identity changed")
	}
	s.sent++
	return s.ackErr
}
func (s *fakeOutbox) Retry(_ context.Context, c Claim, d time.Duration) error {
	if c.ID != s.claim.ID {
		panic("event ID changed on retry")
	}
	s.retries++
	s.delay = d
	return nil
}
func (s *fakeOutbox) Get(context.Context, string) (Event, error) { return s.claim.Event, nil }

type fakeBus struct {
	err error
	ids []string
}

func (b *fakeBus) Publish(_ context.Context, id string) error {
	b.ids = append(b.ids, id)
	return b.err
}

func TestDispatcherFailureRetryAndStableIdentity(t *testing.T) {
	e, _ := NewEvent(BookingChanged, "00000000-0000-0000-0000-000000000001", time.Now())
	s := &fakeOutbox{claim: Claim{Event: e, Token: "lease", Attempt: 3}}
	bus := &fakeBus{err: errors.New("Redis unavailable")}
	d := NewDispatcher(s, bus)
	if n, err := d.RunBatch(context.Background()); err == nil || n != 0 {
		t.Fatal("failed transport acknowledged")
	}
	if s.sent != 0 || s.retries != 1 || s.delay != 4*time.Second {
		t.Fatal("failed transport not retried")
	}
	s.claimed = false
	bus.err = nil
	if n, err := d.RunBatch(context.Background()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if len(bus.ids) != 2 || bus.ids[0] != bus.ids[1] {
		t.Fatal("retry changed event identity")
	}
	s.claimed = false
	s.ackErr = ErrClaimLost
	if _, err := d.RunBatch(context.Background()); !errors.Is(err, ErrClaimLost) {
		t.Fatal("lost claim hidden")
	}
	for _, attempt := range []int{0, 1, 2, 10, 100000} {
		d := RetryDelay(attempt)
		if d < time.Second || d > 5*time.Minute {
			t.Fatal("unbounded retry")
		}
	}
}
