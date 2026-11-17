// Package worker runs bounded background iterations on a cancellable schedule.
package worker

import (
	"context"
	"time"
)

// Poll runs immediately, then waits between completed iterations.
func Poll(ctx context.Context, interval time.Duration, run func(context.Context)) {
	for ctx.Err() == nil {
		run(ctx)
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type Expirer interface {
	ExpireDueRequestsBatch(context.Context, time.Time, int) (int, error)
}

type Expiration struct {
	Service   Expirer
	BatchSize int
	Now       func() time.Time
}

func (e Expiration) RunBatch(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return e.Service.ExpireDueRequestsBatch(ctx, e.Now(), e.BatchSize)
}
