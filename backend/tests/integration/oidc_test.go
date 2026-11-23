package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	oidcprovider "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/oidc"
	identityhttp "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/interfaces/http"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/redis/go-redis/v9"
	"golang.org/x/oauth2"
)

func TestOIDCTransactions(t *testing.T) {
	address := os.Getenv("TUTTI_TEST_REDIS_URL")
	if address == "" {
		t.Skip("use test_identity.sh for disposable Redis")
	}
	opts, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store := sessionredis.NewOIDCTransactions(client)
	state, binding, nonce, verifier := oauth2.GenerateVerifier(), oauth2.GenerateVerifier(), oauth2.GenerateVerifier(), oauth2.GenerateVerifier()
	tx := domain.OIDCTransaction{BindingHash: domain.OIDCHash(binding), Nonce: nonce, Verifier: verifier, ExpiresAt: time.Now().Add(5 * time.Minute)}
	if err := store.Save(ctx, state, tx, 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	key := "tutti:identity:oidc:" + domain.OIDCHash(state)
	defer client.Del(context.Background(), key)
	if ttl, err := client.PTTL(ctx, key).Result(); err != nil || ttl <= 0 || ttl > 5*time.Minute {
		t.Fatal("missing expiration", ttl, err)
	}
	raw, err := client.Get(ctx, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, binding) || strings.Contains(raw, state) {
		t.Fatal("raw state or binding stored")
	}
	if client.Exists(ctx, "tutti:identity:session:"+domain.OIDCHash(state)).Val() != 0 {
		t.Fatal("created an application session")
	}
	if err := store.Save(ctx, state, tx, 5*time.Minute); err == nil {
		t.Fatal("overwrote an existing transaction")
	}
	if _, err := store.Consume(ctx, state, oauth2.GenerateVerifier()); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("wrong browser accepted", err)
	}
	// Exactly one matching consume succeeds, even under callback/replay concurrency.
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.Consume(ctx, state, binding)
			if err == nil && (got.Nonce != nonce || got.Verifier != verifier) {
				results <- errors.New("correlation changed")
				return
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, domain.ErrOIDCTransaction) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("replayed transaction", success)
	}
	if _, err := store.Consume(ctx, state, binding); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("second consume succeeded")
	}
	if err := store.Save(ctx, state, tx, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for client.Exists(ctx, key).Val() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("transaction did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := store.Consume(ctx, state, binding); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("expired transaction accepted")
	}
	// Stored explicit expiry also fails closed, even if a stale key has a longer TTL.
	tx.ExpiresAt = time.Now().Add(-time.Second)
	if err := store.Save(ctx, state, tx, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Consume(ctx, state, binding); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("explicit expiry ignored")
	}
	if _, err := store.Consume(ctx, "invalid", binding); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("invalid state")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, state, tx, time.Minute); err == nil {
		t.Fatal("Redis outage ignored")
	}
}

func TestOIDCLoginInitiation(t *testing.T) {
	address := os.Getenv("TUTTI_TEST_REDIS_URL")
	if address == "" {
		t.Skip("use test_identity.sh for disposable Redis")
	}
	opts, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var issuer string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			t.Error("unexpected provider request")
		}
		json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"}})
	}))
	defer mock.Close()
	issuer = mock.URL
	config := application.OIDCConfig{Issuer: issuer, ClientID: "tutti-backend", PublicOrigin: "http://localhost:8080", RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback", TransactionTTL: 5 * time.Minute}
	provider, err := oidcprovider.New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	store := sessionredis.NewOIDCTransactions(client)
	handler := identityhttp.New(nil, config.PublicOrigin, false, slog.Default())
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	handler.RegisterOIDC(mux, application.NewOIDCLogin(provider, store, config.TransactionTTL))
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	if out.Code != 302 || len(out.Result().Cookies()) != 1 {
		t.Fatal("initiation failed")
	}
	target, err := url.Parse(out.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := target.Query().Get("state")
	binding := out.Result().Cookies()[0].Value
	if _, err := store.Consume(ctx, state, oauth2.GenerateVerifier()); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("cross-browser initiation consumed")
	}
	tx, err := store.Consume(ctx, state, binding)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Nonce != target.Query().Get("nonce") || oauth2.S256ChallengeFromVerifier(tx.Verifier) != target.Query().Get("code_challenge") {
		t.Fatal("stored correlation differs from redirect")
	}
	if out.Result().Cookies()[0].Name != "tutti_oidc_tx" {
		t.Fatal("an application session was issued")
	}
	if _, err := store.Consume(ctx, state, binding); !errors.Is(err, domain.ErrOIDCTransaction) {
		t.Fatal("replayed initialization")
	}
	callback := httptest.NewRecorder()
	mux.ServeHTTP(callback, httptest.NewRequest("GET", "/api/v1/auth/oidc/callback", nil))
	if callback.Code != 404 {
		t.Fatal("callback implemented unexpectedly")
	}
	mock.Close()
	unavailable := httptest.NewRecorder()
	mux.ServeHTTP(unavailable, httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil))
	if unavailable.Code != 503 || unavailable.Header().Get("Location") != "" || len(unavailable.Result().Cookies()) != 0 {
		t.Fatal("provider outage did not fail closed")
	}
}
