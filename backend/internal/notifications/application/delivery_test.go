package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeStore struct {
	pending     []Claim
	sent        int
	failure     FailureCode
	delay       time.Duration
	sentError   error
	failedError error
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
	return f.failedError
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
		{"auth bounded retry", 1, AuthenticationFailure, AuthenticationFailure, time.Second, Stats{Retried: 1}},
		{"auth exhausted", 3, AuthenticationFailure, AuthenticationFailure, 0, Stats{Failed: 1}},
		{"TLS bounded retry", 1, TLSFailure, TLSFailure, time.Second, Stats{Retried: 1}},
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

func TestBackoffProgressionAndBounds(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		want    time.Duration
	}{
		{1, 30 * time.Second}, {2, time.Minute}, {3, 2 * time.Minute}, {6, 16 * time.Minute}, {7, 30 * time.Minute}, {20, 30 * time.Minute},
	} {
		if got := Backoff(tc.attempt, 30*time.Second, 30*time.Minute); got != tc.want {
			t.Fatalf("attempt %d: got %s, want %s", tc.attempt, got, tc.want)
		}
	}
	if got := Backoff(1000, time.Duration(1<<62), time.Duration(1<<63-1)); got != time.Duration(1<<63-1) {
		t.Fatal("overflow", got)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.BatchSize = 0 }, func(c *Config) { c.BatchSize = 101 },
		func(c *Config) { c.MaxAttempts = 0 }, func(c *Config) { c.MaxAttempts = 21 },
		func(c *Config) { c.RetryBase = 0 }, func(c *Config) { c.RetryMax = c.RetryBase - time.Nanosecond },
		func(c *Config) { c.RetryMax = 25 * time.Hour }, func(c *Config) { c.SendTimeout = 61 * time.Second },
		func(c *Config) { c.Lease = c.SendTimeout + 2*DBTimeout }, func(c *Config) { c.Lease = time.Hour + time.Second },
	} {
		c := testConfig()
		change(&c)
		if err := c.Validate(); err == nil {
			t.Fatal("invalid delivery configuration accepted")
		}
	}
}

func TestDeliveryObserverAndPersistenceFailures(t *testing.T) {
	for _, tc := range []struct {
		sendErr, storeErr error
		state             string
		wantErr           bool
	}{
		{nil, nil, "sent", false}, {TemporaryRejection, nil, "retry", false},
		{PermanentRejection, nil, "failed", false}, {context.Canceled, nil, "abandoned", false},
		{nil, ErrClaimLost, "claim_lost", false}, {TemporaryRejection, ErrClaimLost, "claim_lost", false},
		{nil, errors.New("database unavailable"), "finalize_error", true},
		{TemporaryRejection, errors.New("database unavailable"), "finalize_error", true},
	} {
		store := &fakeStore{pending: []Claim{{Message: Message{ID: "notification-id", Recipient: "private@example", Body: "private body"}, Attempt: 1}}, sentError: tc.storeErr, failedError: tc.storeErr}
		service, err := New(store, sender(func(ctx context.Context, _ Message) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("send has no deadline")
			}
			return tc.sendErr
		}), testConfig())
		if err != nil {
			t.Fatal(err)
		}
		var events []Event
		service = service.WithObserver(func(e Event) { events = append(events, e) })
		_, err = service.RunBatch(context.Background())
		if (err != nil) != tc.wantErr || len(events) != 1 || events[0].State != tc.state || events[0].ID != "notification-id" || events[0].Attempt != 1 {
			t.Fatalf("events=%+v error=%v", events, err)
		}
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
