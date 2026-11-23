package redis

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/redis/go-redis/v9"
)

const oidcPrefix = "tutti:identity:oidc:"

type OIDCTransactions struct{ client *redis.Client }

func NewOIDCTransactions(client *redis.Client) *OIDCTransactions {
	return &OIDCTransactions{client: client}
}

func (s *OIDCTransactions) Save(ctx context.Context, state string, tx domain.OIDCTransaction, ttl time.Duration) error {
	if ttl <= 0 || ttl > 10*time.Minute || !domain.ValidOIDCToken(state) {
		return domain.ErrOIDCTransaction
	}
	data, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	ok, err := s.client.SetNX(ctx, oidcPrefix+domain.OIDCHash(state), data, ttl).Result()
	if err == nil && !ok {
		return errors.New("OIDC transaction collision")
	}
	return err
}

var consumeOIDC = redis.NewScript(`
local value=redis.call('GET',KEYS[1])
if not value then return false end
local tx=cjson.decode(value)
if tx.binding_hash~=ARGV[1] then return false end
redis.call('DEL',KEYS[1])
return value
`)

// Consume is an internal single-use primitive for Stage 2, not a callback handler.
func (s *OIDCTransactions) Consume(ctx context.Context, state, binding string) (domain.OIDCTransaction, error) {
	if !domain.ValidOIDCToken(state) || !domain.ValidOIDCToken(binding) {
		return domain.OIDCTransaction{}, domain.ErrOIDCTransaction
	}
	data, err := consumeOIDC.Run(ctx, s.client, []string{oidcPrefix + domain.OIDCHash(state)}, domain.OIDCHash(binding)).Text()
	if errors.Is(err, redis.Nil) {
		return domain.OIDCTransaction{}, domain.ErrOIDCTransaction
	}
	if err != nil {
		return domain.OIDCTransaction{}, err
	}
	var tx domain.OIDCTransaction
	if err := json.Unmarshal([]byte(data), &tx); err != nil {
		return tx, err
	}
	if !time.Now().Before(tx.ExpiresAt) {
		return domain.OIDCTransaction{}, domain.ErrOIDCTransaction
	}
	return tx, nil
}
