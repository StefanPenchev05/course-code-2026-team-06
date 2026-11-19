package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/mail"
	"os"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Opt-in only. test_university_smtp.ps1 creates this dedicated disposable DB
// and explicitly forwards credentials. Ordinary tests and CI never set the gate.
func TestUniversitySMTP(t *testing.T) {
	if os.Getenv("TUTTI_LIVE_SMTP_AUTHORIZE") != "send-one-university-test" {
		t.Skip("real university SMTP requires test_university_smtp.ps1 -SendLiveEmail")
	}
	mode := os.Getenv("SMTP_TLS_MODE")
	if mode != "starttls" && mode != "implicit" {
		t.Fatal("live probe requires verified STARTTLS or implicit TLS")
	}
	recipient := os.Getenv("TUTTI_LIVE_SMTP_RECIPIENT")
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient {
		t.Fatal("a single authorized university email address is required")
	}
	config, err := pgxpool.ParseConfig(os.Getenv("TUTTI_TEST_DATABASE_URL"))
	if err != nil || config.ConnConfig.Database != "tutti_smtp_probe" {
		t.Fatal("live probe requires its dedicated tutti_smtp_probe database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	db, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot open probe database")
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM notifications").Scan(&count); err != nil || count != 0 {
		t.Fatal("probe database must have an empty outbox")
	}
	var id string
	err = db.QueryRow(ctx, `WITH recipient AS (
		INSERT INTO users (name,email) VALUES ('Authorized SMTP probe',$1) RETURNING id
	), probe AS (SELECT gen_random_uuid() AS id)
	INSERT INTO notifications (id,recipient_user_id,type,subject,body,deduplication_key)
	SELECT probe.id,recipient.id,'SESSION_REMINDER','Tutti SMTP verification '||probe.id::text,
	       'Authorized delivery test. Reference: '||probe.id::text,'smtp-probe:'||probe.id::text
	FROM recipient,probe RETURNING id::text`, recipient).Scan(&id)
	if err != nil {
		t.Fatal("could not queue isolated probe")
	}
	t.Logf("queued probe notification_id=%s", id)
	for key, value := range map[string]string{
		"DATABASE_URL": os.Getenv("TUTTI_TEST_DATABASE_URL"), "WORKER_EMAIL_BATCH_SIZE": "1",
		"WORKER_EMAIL_MAX_ATTEMPTS": "1", "WORKER_EMAIL_INTERVAL": "1h", "WORKER_EXPIRATION_INTERVAL": "1h",
	} {
		t.Setenv(key, value)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- bootstrap.RunWorker(runCtx, slog.New(slog.NewJSONHandler(io.Discard, nil))) }()
	defer func() { stop(); <-done }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			// Put the result back so deferred shutdown can join the worker.
			done <- err
			t.Fatalf("probe worker stopped before acknowledgment: %v", err)
		case <-ctx.Done():
			t.Fatal("probe timed out; delivery may be ambiguous; check provider before repeating")
		case <-ticker.C:
			var state, category string
			if err := db.QueryRow(ctx, "SELECT email_status::text,coalesce(last_error,'') FROM notifications WHERE id=$1", id).Scan(&state, &category); err != nil {
				t.Fatal("cannot read probe result")
			}
			switch state {
			case "SENT":
				t.Logf("SMTP accepted notification_id=%s; inbox delivery still requires recipient confirmation", id)
				return
			case "FAILED":
				t.Fatalf("probe notification_id=%s failed category=%s", id, category)
			}
		}
	}
}
