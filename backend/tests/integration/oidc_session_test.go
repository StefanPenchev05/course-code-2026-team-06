package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

func TestOIDCSessionRotationAndLimits(t *testing.T) {
	address := os.Getenv("TUTTI_TEST_REDIS_URL")
	if address == "" {
		t.Skip("use disposable test_identity.sh runner")
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	// Separate from the application's test sessions and limiter counters in DB 0.
	options.DB = 14
	cache := redis.NewClient(options)
	defer cache.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var owned []string
	defer func() {
		cleanup := redis.NewClient(options)
		defer cleanup.Close()
		cleanup.Del(context.Background(), owned...)
	}()
	session := func() string {
		key := domain.OIDCHash(oauth2.GenerateVerifier())
		owned = append(owned, "tutti:identity:session:"+key)
		return key
	}
	store := sessionredis.New(cache)
	old, fresh, collision := session(), session(), session()
	if err := store.Create(ctx, old, "synthetic-user", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx, fresh, "synthetic-user", old, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Lookup(ctx, old); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("old session retained")
	}
	if id, err := store.Lookup(ctx, fresh); err != nil || id != "synthetic-user" {
		t.Fatal("new session missing")
	}
	ttl, err := cache.PTTL(ctx, "tutti:identity:session:"+fresh).Result()
	if err != nil || ttl < 59*time.Minute || ttl > time.Hour {
		t.Fatal("invalid session expiration")
	}
	if err := store.Create(ctx, collision, "other-user", time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx, collision, "synthetic-user", fresh, time.Hour); err == nil {
		t.Fatal("collision overwritten")
	}
	if _, err := store.Lookup(ctx, fresh); err != nil {
		t.Fatal("failed rotation revoked existing session")
	}
	cache.ExpireAt(ctx, "tutti:identity:session:"+fresh, time.Now().Add(-time.Second))
	if _, err := store.Lookup(ctx, fresh); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("expired session reused")
	}
	limiter := sessionredis.NewOIDCLimiter(cache)
	global := "tutti:identity:oidc-limit:global"
	if exists, err := cache.Exists(ctx, global).Result(); err != nil || exists != 0 {
		t.Fatal("test requires unused limiter database")
	}
	owned = append(owned, global)
	peer := "concurrent-peer-" + oauth2.GenerateVerifier()
	peerKey := "tutti:identity:oidc-limit:peer:" + domain.OIDCHash(peer)
	owned = append(owned, peerKey)
	done := make(chan bool, 40)
	for range 40 {
		go func() {
			ok, err := limiter.Allow(ctx, peer)
			if err != nil {
				t.Error("limiter failed")
			}
			done <- ok
		}()
	}
	allowed := 0
	for range 40 {
		if <-done {
			allowed++
		}
	}
	if allowed != 10 {
		t.Fatal("peer budget not atomic", allowed)
	}
	if ttl := cache.PTTL(ctx, peerKey).Val(); ttl <= 0 || ttl > time.Minute {
		t.Fatal("unbounded limiter key")
	}
	cache.ExpireAt(ctx, global, time.Now().Add(-time.Second))
	cache.ExpireAt(ctx, peerKey, time.Now().Add(-time.Second))
	for i := 0; i < 121; i++ {
		peer := fmt.Sprintf("global-peer-%s-%d", fresh, i)
		owned = append(owned, "tutti:identity:oidc-limit:peer:"+domain.OIDCHash(peer))
		ok, err := limiter.Allow(ctx, peer)
		if err != nil || ok != (i < 120) {
			t.Fatal("global budget not enforced", i, err)
		}
	}
	cache.Close()
	if ok, err := limiter.Allow(ctx, peer); err == nil || ok {
		t.Fatal("failed cache allowed authentication")
	}
	// Cleanup uses a separate live connection after the failure probe above.
	if err := store.Rotate(ctx, session(), "synthetic-user", collision, time.Hour); err == nil {
		t.Fatal("session rotation hid Redis failure")
	}
}
