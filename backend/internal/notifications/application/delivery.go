// Package application delivers queued notifications through an injected mail transport.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrClaimLost = errors.New("notification claim is no longer owned")

type Message struct {
	ID, Recipient, Subject, Body string
}

type Claim struct {
	Message
	Token      string
	Attempt    int
	LeaseUntil time.Time
}

type Store interface {
	Claim(ctx context.Context, lease time.Duration, maxAttempts int) (Claim, bool, error)
	Sent(ctx context.Context, claim Claim) error
	Failed(ctx context.Context, claim Claim, code FailureCode, retryAfter time.Duration) error
	FailExhausted(ctx context.Context, maxAttempts, limit int) (int, error)
}

type Transport interface {
	Send(context.Context, Message) error
}

// FailureCode contains operational categories. Provider replies can contain
// personal data, so the transport returns only these categories to the worker.
type FailureCode string

const (
	InvalidMessage        FailureCode = "invalid_message"
	PermanentRejection    FailureCode = "smtp_permanent"
	TemporaryRejection    FailureCode = "smtp_temporary"
	TransportFailure      FailureCode = "transport"
	TransportTimeout      FailureCode = "timeout"
	AuthenticationFailure FailureCode = "smtp_auth"
	TLSFailure            FailureCode = "smtp_tls"
)

func (f FailureCode) Error() string { return string(f) }

func failureCode(err error) FailureCode {
	var code FailureCode
	if errors.As(err, &code) {
		switch code {
		case InvalidMessage, PermanentRejection, TemporaryRejection, TransportFailure, TransportTimeout, AuthenticationFailure, TLSFailure:
			return code
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return TransportTimeout
	}
	return TransportFailure
}

type Config struct {
	BatchSize, MaxAttempts                  int
	Lease, SendTimeout, RetryBase, RetryMax time.Duration
}

const DBTimeout = 5 * time.Second

func (c Config) Validate() error {
	if c.BatchSize < 1 || c.BatchSize > 100 || c.MaxAttempts < 1 || c.MaxAttempts > 20 ||
		c.SendTimeout <= 0 || c.SendTimeout > time.Minute ||
		c.Lease <= c.SendTimeout+2*DBTimeout || c.Lease > time.Hour ||
		c.RetryBase <= 0 || c.RetryMax < c.RetryBase || c.RetryMax > 24*time.Hour {
		return fmt.Errorf("invalid email delivery configuration")
	}
	return nil
}

type Delivery struct {
	store     Store
	transport Transport
	config    Config
	observe   func(Event)
}

// Event deliberately excludes recipient addresses, message content, provider
// replies and claim tokens. Observers must not block delivery.
type Event struct {
	ID         string
	Attempt    int
	State      string
	Failure    FailureCode
	RetryAfter time.Duration
	LeaseUntil time.Time
	Duration   time.Duration
}

// WithObserver returns a copy; configure it before starting the delivery loop.
func (d *Delivery) WithObserver(observe func(Event)) *Delivery {
	copy := *d
	copy.observe = observe
	return &copy
}

func New(store Store, transport Transport, config Config) (*Delivery, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Delivery{store: store, transport: transport, config: config}, nil
}

type Stats struct{ Sent, Retried, Failed, Lost int }

func Backoff(attempt int, base, max time.Duration) time.Duration {
	delay := base
	for i := 1; i < attempt && delay < max; i++ {
		if delay > max/2 {
			return max
		}
		delay *= 2
	}
	return min(delay, max)
}

func (d *Delivery) RunBatch(ctx context.Context) (Stats, error) {
	var stats Stats
	if ctx.Err() != nil {
		return stats, nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, DBTimeout)
	n, err := d.store.FailExhausted(dbCtx, d.config.MaxAttempts, d.config.BatchSize)
	cancel()
	if err != nil {
		return stats, fmt.Errorf("mark exhausted email claims: %w", err)
	}
	stats.Failed = n

	for i := 0; i < d.config.BatchSize && ctx.Err() == nil; i++ {
		// Claim just before sending. Earlier messages cannot use up the lease
		// of a later message in a slow batch.
		dbCtx, cancel := context.WithTimeout(ctx, DBTimeout)
		claim, ok, err := d.store.Claim(dbCtx, d.config.Lease, d.config.MaxAttempts)
		cancel()
		if err != nil {
			return stats, fmt.Errorf("claim email: %w", err)
		}
		if !ok {
			break
		}
		if err := d.deliver(ctx, claim, &stats); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func (d *Delivery) deliver(ctx context.Context, claim Claim, stats *Stats) error {
	started := time.Now()
	event := Event{ID: claim.ID, Attempt: claim.Attempt, LeaseUntil: claim.LeaseUntil}
	defer func() {
		if d.observe != nil {
			event.Duration = time.Since(started)
			d.observe(event)
		}
	}()
	// On SIGTERM finish only the already claimed message, within these timeouts.
	inFlight := context.WithoutCancel(ctx)
	sendCtx, cancel := context.WithTimeout(inFlight, d.config.SendTimeout)
	sendErr := d.transport.Send(sendCtx, claim.Message)
	cancel()
	dbCtx, cancel := context.WithTimeout(inFlight, DBTimeout)
	defer cancel()

	var err error
	switch {
	case sendErr == nil:
		event.State = "sent"
		err = d.store.Sent(dbCtx, claim)
		if err == nil {
			stats.Sent++
		}
	case errors.Is(sendErr, context.Canceled):
		event.State = "abandoned"
		// An adapter that aborts without a definite provider result leaves the
		// claim for lease recovery. Do not mark the message permanently failed.
		stats.Lost++
	default:
		code := failureCode(sendErr)
		event.Failure = code
		var delay time.Duration
		if code != InvalidMessage && code != PermanentRejection && claim.Attempt < d.config.MaxAttempts {
			delay = Backoff(claim.Attempt, d.config.RetryBase, d.config.RetryMax)
		}
		err = d.store.Failed(dbCtx, claim, code, delay)
		event.State, event.RetryAfter = "failed", delay
		if err == nil && delay > 0 {
			event.State = "retry"
			stats.Retried++
		}
		if err == nil && delay == 0 {
			stats.Failed++
		}
	}
	if errors.Is(err, ErrClaimLost) {
		event.State = "claim_lost"
		stats.Lost++
		return nil
	}
	if err != nil {
		event.State = "finalize_error"
		return fmt.Errorf("finalize email claim: %w", err)
	}
	return nil
}
