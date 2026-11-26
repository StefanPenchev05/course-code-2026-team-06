package application

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
)

var ErrClaimLost = errors.New("realtime claim no longer owned")

type Claim struct {
	Event
	Token   string
	Attempt int
}
type Outbox interface {
	Claim(context.Context, time.Duration) (Claim, bool, error)
	Dispatched(context.Context, Claim) error
	Retry(context.Context, Claim, time.Duration) error
	Get(context.Context, string) (Event, error)
}
type Fanout interface {
	Publish(context.Context, string) error
}

type Dispatcher struct {
	store   Outbox
	bus     Fanout
	metrics *observability.Metrics
}

func NewDispatcher(store Outbox, bus Fanout) *Dispatcher               { return &Dispatcher{store: store, bus: bus} }
func (d *Dispatcher) WithMetrics(m *observability.Metrics) *Dispatcher { d.metrics = m; return d }

func RetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 10 {
		attempt = 10
	}
	delay := time.Second * time.Duration(1<<uint(attempt-1))
	if delay > 5*time.Minute {
		return 5 * time.Minute
	}
	return delay
}

// One short claim at a time avoids a batch's later rows losing their lease
// while earlier publishes wait. Database ownership tokens fence late callbacks.
func (d *Dispatcher) RunBatch(ctx context.Context) (int, error) {
	batch, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sent := 0
	for n := 0; n < 20; n++ {
		op, cancel := context.WithTimeout(batch, 3*time.Second)
		claim, ok, err := d.store.Claim(op, 30*time.Second)
		cancel()
		if err != nil {
			d.metrics.Inc("dispatch_claim_failed")
			return sent, err
		}
		if !ok {
			return sent, nil
		}
		d.metrics.Inc("dispatch_claimed")
		op, cancel = context.WithTimeout(batch, 3*time.Second)
		started := time.Now()
		err = d.bus.Publish(op, claim.ID)
		d.metrics.Observe("publish", time.Since(started))
		cancel()
		op, cancel = context.WithTimeout(batch, 3*time.Second)
		if err != nil {
			d.metrics.Inc("dispatch_failed")
			retryErr := d.store.Retry(op, claim, RetryDelay(claim.Attempt))
			if retryErr == nil {
				d.metrics.Inc("dispatch_retried")
			} else {
				d.metrics.Inc("dispatch_retry_failed")
			}
			cancel()
			return sent, errors.Join(err, retryErr)
		}
		d.metrics.Inc("dispatch_published")
		if at, parseErr := time.Parse(time.RFC3339Nano, claim.OccurredAt); parseErr == nil {
			d.metrics.Observe("capture_to_publish", time.Since(at))
		}
		err = d.store.Dispatched(op, claim)
		cancel()
		if err != nil {
			d.metrics.Inc("dispatch_ack_failed")
			return sent, err
		}
		sent++
	}
	return sent, nil
}
