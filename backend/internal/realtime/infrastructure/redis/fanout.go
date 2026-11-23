package redis

import (
	"context"
	"errors"
	"time"

	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/redis/go-redis/v9"
)

const Channel = "tutti:realtime:v1"

type Fanout struct{ client *redis.Client }

func New(client *redis.Client) *Fanout { return &Fanout{client} }
func (f *Fanout) Publish(ctx context.Context, id string) error {
	if !app.ValidID(id) {
		return app.ErrInvalidEvent
	}
	n, err := f.client.Publish(ctx, Channel, id).Result()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("no realtime subscribers")
	}
	return nil
}

type Subscription struct{ pubsub *redis.PubSub }

func (f *Fanout) Subscribe(ctx context.Context) (*Subscription, error) {
	ps := f.client.Subscribe(ctx, Channel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, err
	}
	return &Subscription{ps}, nil
}
func (s *Subscription) Close() error { return s.pubsub.Close() }

// Every API subscribes independently. A reconnect/error closes local sockets so
// clients refetch/re-register watches rather than silently missing invalidations.
func (s *Subscription) Run(ctx context.Context, receiver *app.Receiver, resync func(), failed func()) {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	defer s.Close()
	for ctx.Err() == nil {
		message, err := s.pubsub.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			resync()
			failed()
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		switch m := message.(type) {
		case *redis.Subscription:
			resync()
		case *redis.Message:
			if m.Channel != Channel || !app.ValidID(m.Payload) {
				continue
			}
			op, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := receiver.Receive(op, m.Payload)
			cancel()
			if err != nil && ctx.Err() == nil {
				resync()
				failed()
			}
		}
	}
}
