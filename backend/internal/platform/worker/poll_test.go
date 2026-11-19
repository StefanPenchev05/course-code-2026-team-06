package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

type expirer func(context.Context, time.Time, int) (int, error)

func (f expirer) ExpireDueRequestsBatch(ctx context.Context, now time.Time, limit int) (int, error) {
	return f(ctx, now, limit)
}

func TestPollRepeatsEmptyAndFailedExpirationBatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	now := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	transient := errors.New("temporary database failure")
	w := Expiration{BatchSize: 7, Now: func() time.Time { return now }, Service: expirer(
		func(ctx context.Context, at time.Time, limit int) (int, error) {
			if !at.Equal(now) || limit != 7 {
				t.Fatal(at, limit)
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("database work must be bounded")
			}
			calls++
			switch calls {
			case 1:
				return 0, transient
			case 2:
				return 0, nil
			default:
				return 1, nil
			}
		})}
	Poll(ctx, time.Millisecond, func(ctx context.Context) {
		n, err := w.RunBatch(ctx)
		if calls == 1 && !errors.Is(err, transient) || calls == 2 && (err != nil || n != 0) {
			t.Fatal(n, err)
		}
		if calls == 3 {
			if err != nil || n != 1 {
				t.Fatal(n, err)
			}
			cancel()
		}
	})
	if calls != 3 {
		t.Fatalf("got %d calls", calls)
	}
	Poll(ctx, time.Hour, func(context.Context) { t.Fatal("cancelled worker took new work") })
}
