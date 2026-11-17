package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

const keyPrefix = "tutti:identity:session:"

type Store struct { client *redis.Client }

func New(client *redis.Client) *Store { return &Store{client: client} }

func (s *Store) Create(ctx context.Context, key string, userID string, ttl time.Duration) error {
	ok, err := s.client.SetNX(ctx, keyPrefix+key, userID, ttl).Result()
	if err == nil && !ok {
		return fmt.Errorf("session identifier already exists")
	}
	return err
}

func (s *Store) Lookup(ctx context.Context, key string) (string, error) {
	id, err := s.client.Get(ctx, keyPrefix+key).Result()
	if errors.Is(err, redis.Nil) {
		return "", domain.ErrUnauthenticated
	}
	return id, err
}

func (s *Store) Revoke(ctx context.Context, key string) error {
	return s.client.Del(ctx, keyPrefix+key).Err()
}
