package redis

import (
	"context"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/redis/go-redis/v9"
)

const Channel = "tutti:realtime:v1"

type Fanout struct {
	client  *redis.Client
	channel string
	metrics *observability.Metrics
}

func New(client *redis.Client) *Fanout                         { return &Fanout{client: client, channel: Channel} }
func (f *Fanout) WithMetrics(m *observability.Metrics) *Fanout { f.metrics = m; return f }
func (f *Fanout) Publish(ctx context.Context, id string) error {
	if !app.ValidID(id) {
		return app.ErrInvalidEvent
	}
	n, err := f.client.Publish(ctx, f.channel, id).Result()
	if err != nil {
		f.metrics.Inc("fanout_failures")
		return err
	}
	if n == 0 {
		f.metrics.Inc("fanout_failures")
		return errors.New("no realtime subscribers")
	}
	return nil
}

type Subscription struct {
	pubsub  *redis.PubSub
	channel string
	metrics *observability.Metrics
}

func (f *Fanout) Subscribe(ctx context.Context) (*Subscription, error) {
	if f.channel != Channel {
		legacy, err := f.client.PubSubNumSub(ctx, Channel).Result()
		if err != nil {
			return nil, errors.New("could not check legacy realtime subscribers")
		}
		if legacy[Channel] != 0 {
			return nil, errors.New("legacy realtime subscribers present; coordinated API rollout required")
		}
	}
	ps := f.client.Subscribe(ctx, f.channel)
	if _, err := ps.Receive(ctx); err != nil {
		_ = ps.Close()
		return nil, err
	}
	f.metrics.Subscribed(true)
	return &Subscription{ps, f.channel, f.metrics}, nil
}
func (s *Subscription) Close() error { return s.pubsub.Close() }

// Every API subscribes independently. A reconnect/error closes local sockets so
// clients refetch/re-register watches rather than silently missing invalidations.
func (s *Subscription) Run(ctx context.Context, receiver *app.Receiver, resync func(), failed func()) {
	defer s.metrics.Subscribed(false)
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
			s.metrics.Subscribed(false)
			s.metrics.Inc("subscription_interruptions")
			s.metrics.Inc("subscription_reconnect_attempts")
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
			s.metrics.Subscribed(true)
			s.metrics.Inc("subscription_recoveries")
		case *redis.Message:
			if m.Channel != s.channel || !app.ValidID(m.Payload) {
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
