package redis

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/redis/go-redis/v9"
)

// Bound both per-peer attempts and total saved transactions across API replicas.
// Ignore proxy headers: only the actual TCP peer is trusted by the HTTP adapter.
type OIDCLimiter struct{ client *redis.Client }

func NewOIDCLimiter(client *redis.Client) *OIDCLimiter { return &OIDCLimiter{client: client} }

var limitOIDC = redis.NewScript(`
local global=redis.call('INCR',KEYS[1])
if global==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end
if global>120 then return 0 end
local peer=redis.call('INCR',KEYS[2])
if peer==1 then redis.call('PEXPIRE',KEYS[2],ARGV[1]) end
if peer>10 then return 0 end
return 1
`)

func (s *OIDCLimiter) Allow(ctx context.Context, peer string) (bool, error) {
	ok, err := limitOIDC.Run(ctx, s.client, []string{"tutti:identity:oidc-limit:global", "tutti:identity:oidc-limit:peer:" + domain.OIDCHash(peer)}, time.Minute.Milliseconds()).Int()
	return ok == 1, err
}
