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

type Store struct{ client *redis.Client }

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

// A single Redis operation prevents a successful cookie response without also
// revoking the browser's preceding session. Both keys are server-side hashes.
var rotateSession = redis.NewScript(`
if KEYS[1]==KEYS[2] then return 0 end
local created=redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2],'NX')
if not created then return 0 end
if ARGV[3]=='1' then redis.call('DEL',KEYS[2]) end
return 1
`)

func (s *Store) Rotate(ctx context.Context, key, userID, previousKey string, ttl time.Duration) error {
	oldKey := keyPrefix + previousKey
	hasPrevious := "0"
	if previousKey != "" {
		hasPrevious = "1"
	}
	if ttl < time.Second {
		return errors.New("invalid session lifetime")
	}
	ok, err := rotateSession.Run(ctx, s.client, []string{keyPrefix + key, oldKey}, userID, ttl.Milliseconds(), hasPrevious).Int()
	if err == nil && ok != 1 {
		return errors.New("session identifier collision")
	}
	return err
}
