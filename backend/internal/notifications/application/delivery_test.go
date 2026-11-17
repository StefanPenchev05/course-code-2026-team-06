package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	pending   []Claim
	sent      int
	failure   FailureCode
	delay     time.Duration
	sentError error
}

func (f *fakeStore) Claim(ctx context.Context, _ time.Duration, _ int) (Claim, bool, error) {
	if len(f.pending) == 0 {
		return Claim{}, false, nil
	}
	c := f.pending[0]
	f.pending = f.pending[1:]
	return c, true, nil
}
func (f *fakeStore) Sent(context.Context, Claim) error { f.sent++; return f.sentError }
func (f *fakeStore) Failed(_ context.Context, _ Claim, code FailureCode, delay time.Duration) error {
	f.failure, f.delay = code, delay
	return nil
}
func (f *fakeStore) FailExhausted(context.Context, int, int) (int, error) { return 0, nil }

type sender func(context.Context, Message) error

func (s sender) Send(ctx context.Context, m Message) error { return s(ctx, m) }

func testConfig() Config {
	return Config{BatchSize: 3, MaxAttempts: 3, Lease: 20 * time.Second, SendTimeout: time.Second, RetryBase: time.Second, RetryMax: 3 * time.Second}
}

func TestDeliveryOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		attempt int
		sendErr error
		code    FailureCode
		delay   time.Duration
		stats   Stats
	}{
		{"accepted", 1, nil, "", 0, Stats{Sent: 1}},
		{"temporary", 2, TemporaryRejection, TemporaryRejection, 2 * time.Second, Stats{Retried: 1}},
		{"network", 1, errors.New("contains private details"), TransportFailure, time.Second, Stats{Retried: 1}},
		{"permanent", 1, PermanentRejection, PermanentRejection, 0, Stats{Failed: 1}},
		{"invalid message", 1, InvalidMessage, InvalidMessage, 0, Stats{Failed: 1}},
		{"exhausted", 3, context.DeadlineExceeded, TransportTimeout, 0, Stats{Failed: 1}},
		{"abandoned", 1, context.Canceled, "", 0, Stats{Lost: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{pending: []Claim{{Attempt: tc.attempt}}}
			service, err := New(store, sender(func(context.Context, Message) error { return tc.sendErr }), testConfig())
			if err != nil {
				t.Fatal(err)
			}
			got, err := service.RunBatch(context.Background())
			if err != nil || got != tc.stats || store.failure != tc.code || store.delay != tc.delay {
				t.Fatalf("stats %+v, failure %s, delay %s, error %v", got, store.failure, store.delay, err)
			}
		})
	}
}

func TestShutdownFinishesClaimAndStopsTakingWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeStore{pending: []Claim{{Attempt: 1}, {Attempt: 1}}}
	service, err := New(store, sender(func(inFlight context.Context, _ Message) error {
		cancel()
		if inFlight.Err() != nil {
			t.Fatal("already claimed SMTP work was interrupted")
		}
		return nil
	}), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.RunBatch(ctx)
	if err != nil || got.Sent != 1 || len(store.pending) != 1 {
		t.Fatalf("stats=%+v pending=%d err=%v", got, len(store.pending), err)
	}
}

func TestStaleClaimAndBoundedBackoff(t *testing.T) {
	store := &fakeStore{pending: []Claim{{Attempt: 1}}, sentError: ErrClaimLost}
	service, err := New(store, sender(func(context.Context, Message) error { return nil }), testConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.RunBatch(context.Background())
	if err != nil || got != (Stats{Lost: 1}) {
		t.Fatal(got, err)
	}
	if got := Backoff(99, time.Second, 3*time.Second); got != 3*time.Second {
		t.Fatal(got)
	}
}
