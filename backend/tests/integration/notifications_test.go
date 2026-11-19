package integration_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	osexec "os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	email "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/application"
	emailpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/infrastructure/smtp"
)

func startTestSMTP(t *testing.T) (int, <-chan string) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	messages := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			serveTestSMTP(conn, messages)
		}
	}()
	t.Cleanup(func() { _ = listener.Close(); <-done })
	return listener.Addr().(*net.TCPAddr).Port, messages
}

func serveTestSMTP(conn net.Conn, messages chan<- string) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	reply := func(line string) { _, _ = fmt.Fprint(conn, line+"\r\n") }
	reply("220 local.test")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		switch {
		case strings.HasPrefix(line, "EHLO"), strings.HasPrefix(line, "HELO"), strings.HasPrefix(line, "MAIL FROM:"):
			reply("250 OK")
		case strings.HasPrefix(line, "RCPT TO:"):
			switch {
			case strings.Contains(line, "permanent@"):
				reply("550 Recipient rejected")
			case strings.Contains(line, "temporary@"):
				reply("450 Try later")
			default:
				reply("250 OK")
			}
		case strings.HasPrefix(line, "DATA"):
			reply("354 End with a dot")
			var message strings.Builder
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if line == ".\r\n" {
					break
				}
				message.WriteString(line)
			}
			messages <- message.String()
			reply("250 Accepted locally")
		default:
			reply("500 Unsupported test command")
		}
	}
}

// Exercise a notification created by the authenticated booking API, including
// its committed event, rather than only inserting an artificial outbox record.
func (f *bookingFixture) testBookingEmail(t *testing.T, session string) {
	t.Helper()
	var id string
	if err := f.db.QueryRow(f.ctx, "SELECT id::text FROM notifications WHERE session_id=$1 AND type='NEW_REQUEST'", session).Scan(&id); err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(f.ctx)
	if _, err := lock.Exec(f.ctx, "SELECT id FROM notifications WHERE id<>$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	port, messages := startTestSMTP(t)
	transport, err := smtp.New(smtp.Config{Host: "127.0.0.1", Port: port, From: "tutti@local.test", TLSMode: "plain", ConnectTimeout: time.Second, SendTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := email.New(emailpg.New(f.db), transport, email.Config{BatchSize: 1, MaxAttempts: 2, Lease: 30 * time.Second, SendTimeout: 2 * time.Second, RetryBase: time.Minute, RetryMax: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	stats, err := delivery.RunBatch(f.ctx)
	if err != nil || stats.Sent != 1 {
		t.Fatal("booking email not delivered", stats, err)
	}
	select {
	case message := <-messages:
		if !strings.Contains(message, "To: book-tutor@uni.example") || !strings.Contains(message, "Message-ID: <"+id+"@local.test>") {
			t.Fatal("wrong booking email")
		}
	case <-f.ctx.Done():
		t.Fatal("booking email was not accepted locally")
	}
	f.count(t, 1, "SELECT count(*) FROM notifications WHERE id=$1 AND email_status='SENT' AND email_sent_at IS NOT NULL", id)
}

func TestNotificationDelivery(t *testing.T) {
	dsn := os.Getenv("TUTTI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("run tests/integration/test_identity.sh for PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := emailpg.New(db)
	if err := store.Check(ctx); err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(id int, address string) string {
		t.Helper()
		exec("INSERT INTO users (id,name,email) VALUES ($1,'Mail fixture',$2)", fixture(id), address)
		exec(`INSERT INTO notifications (id,recipient_user_id,type,subject,body,deduplication_key,next_attempt_at)
			VALUES ($1,$2,'SESSION_REMINDER','Tutti fixture','Private fixture','fixture',now()-interval '1 day')`, fixture(id+30), fixture(id))
		return fixture(id + 30)
	}
	state := func(id, status, code string, attempt int) {
		t.Helper()
		var actual, last string
		var tries int
		var clean bool
		err := db.QueryRow(ctx, `SELECT email_status::text,coalesce(last_error,''),attempt_count,
			lease_until IS NULL AND claim_token IS NULL FROM notifications WHERE id=$1`, id).Scan(&actual, &last, &tries, &clean)
		if err != nil || actual != status || last != code || tries != attempt || !clean {
			t.Fatalf("%s: state=%s code=%s attempt=%d clean=%t err=%v", id, actual, last, tries, clean, err)
		}
	}
	port, messages := startTestSMTP(t)
	makeTransport := func(mode string) *smtp.Transport {
		t.Helper()
		transport, err := smtp.New(smtp.Config{Host: "127.0.0.1", Port: port, From: "tutti@local.test",
			TLSMode: mode, ConnectTimeout: time.Second, SendTimeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		return transport
	}
	delivery, err := email.New(store, makeTransport("plain"), email.Config{
		BatchSize: 1, MaxAttempts: 2, Lease: 30 * time.Second, SendTimeout: 2 * time.Second,
		RetryBase: time.Minute, RetryMax: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	success := seed(501, "success@local.test")
	stats, err := delivery.RunBatch(ctx)
	if err != nil || stats.Sent != 1 {
		t.Fatal(stats, err)
	}
	state(success, "SENT", "", 1)
	var accepted bool
	err = db.QueryRow(ctx, `SELECT email_sent_at>=created_at AND email_sent_at<=statement_timestamp()
		FROM notifications WHERE id=$1`, success).Scan(&accepted)
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}
	select {
	case got := <-messages:
		if !strings.Contains(got, "Message-ID: <"+success+"@local.test>") || !strings.Contains(got, "To: success@local.test") {
			t.Fatal(got)
		}
	case <-ctx.Done():
		t.Fatal("local SMTP server did not accept a message")
	}
	// Requiring STARTTLS must refuse the same plaintext test server.
	if err := makeTransport("starttls").Send(ctx, email.Message{ID: success, Recipient: "success@local.test", Subject: "Fixture"}); !errors.Is(err, email.TLSFailure) {
		t.Fatalf("TLS downgrade: %v", err)
	}

	temporary := seed(502, "temporary@local.test")
	stats, err = delivery.RunBatch(ctx)
	if err != nil || stats.Retried != 1 {
		t.Fatal(stats, err)
	}
	state(temporary, "PENDING", "smtp_temporary", 1)
	var delayed bool
	err = db.QueryRow(ctx, `SELECT next_attempt_at > statement_timestamp()+interval '50 seconds' AND email_failed_at IS NULL
		FROM notifications WHERE id=$1`, temporary).Scan(&delayed)
	if err != nil || !delayed {
		t.Fatal(delayed, err)
	}
	// A fresh worker must honor the persisted retry time. Lock unrelated test
	// records while inspecting the real queue; SKIP LOCKED must skip them.
	lock, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(ctx, "SELECT id FROM notifications WHERE id<>$1 FOR UPDATE", temporary); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := emailpg.New(db).Claim(ctx, time.Minute, 2); err != nil || ok {
		t.Fatalf("future retry became eligible: %t %v", ok, err)
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE notifications SET next_attempt_at=now()-interval '1 day' WHERE id=$1", temporary)
	stats, err = delivery.RunBatch(ctx)
	if err != nil || stats.Failed != 1 {
		t.Fatal(stats, err)
	}
	state(temporary, "FAILED", "smtp_temporary", 2)
	permanent := seed(503, "permanent@local.test")
	stats, err = delivery.RunBatch(ctx)
	if err != nil || stats.Failed != 1 {
		t.Fatal(stats, err)
	}
	state(permanent, "FAILED", "smtp_permanent", 1)

	first := seed(504, "first@local.test")
	second := seed(505, "second@local.test")
	type result struct {
		claim email.Claim
		ok    bool
		err   error
	}
	out := make(chan result, 2)
	for range 2 {
		go func() {
			c, ok, err := emailpg.New(db).Claim(ctx, time.Minute, 2)
			out <- result{c, ok, err}
		}()
	}
	x, y := <-out, <-out
	if x.err != nil || y.err != nil || !x.ok || !y.ok || x.claim.ID == y.claim.ID ||
		(x.claim.ID != first && x.claim.ID != second) || (y.claim.ID != first && y.claim.ID != second) {
		t.Fatal("workers did not claim distinct pending rows", x.err, y.err)
	}

	old := x.claim
	if old.LeaseUntil.IsZero() || old.LeaseUntil.Before(time.Now()) {
		t.Fatal("claim has no live lease")
	}
	// Simulate an abruptly stopped worker. Only its expired claim is available.
	exec("UPDATE notifications SET lease_until=now()-interval '1 second' WHERE id=$1", old.ID)
	if err := store.Sent(ctx, old); !errors.Is(err, email.ErrClaimLost) {
		t.Fatalf("expired owner acknowledged: %v", err)
	}
	if err := store.Failed(ctx, old, email.TransportFailure, time.Second); !errors.Is(err, email.ErrClaimLost) {
		t.Fatalf("expired owner scheduled retry: %v", err)
	}
	restarted := emailpg.New(db)
	recovered, ok, err := restarted.Claim(ctx, time.Minute, 2)
	if err != nil || !ok || recovered.ID != old.ID || recovered.Token == old.Token || recovered.Attempt != 2 {
		t.Fatalf("restart did not recover lease: %v", err)
	}
	if err := store.Sent(ctx, old); !errors.Is(err, email.ErrClaimLost) {
		t.Fatalf("stale claim won: %v", err)
	}
	if err := store.Failed(ctx, old, email.TransportFailure, time.Second); !errors.Is(err, email.ErrClaimLost) {
		t.Fatalf("stale failure won: %v", err)
	}
	if err := restarted.Sent(ctx, recovered); err != nil {
		t.Fatal(err)
	}
	state(old.ID, "SENT", "", 2)
	if err := store.Failed(ctx, recovered, email.TransportFailure, time.Second); !errors.Is(err, email.ErrClaimLost) {
		t.Fatal("terminal state overwritten", err)
	}

	exec("UPDATE notifications SET attempt_count=2,lease_until=now()-interval '1 second' WHERE id=$1", y.claim.ID)
	if n, err := restarted.FailExhausted(ctx, 2, 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	state(y.claim.ID, "FAILED", "lease_expired", 2)
	t.Run("claim transaction rollback and live final lease", func(t *testing.T) {
		id := seed(507, "rollback@local.test")
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		// This models termination before claim commit: neither ownership nor
		// the consumed attempt may survive rollback.
		if _, err := tx.Exec(ctx, `UPDATE notifications SET attempt_count=1,claim_token=gen_random_uuid(),lease_until=now()+interval '1 minute' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		if err := tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		state(id, "PENDING", "", 0)
		claim, ok, err := store.Claim(ctx, time.Minute, 1)
		if err != nil || !ok || claim.ID != id || claim.Attempt != 1 {
			t.Fatal("rolled-back attempt lost", err)
		}
		if n, err := store.FailExhausted(ctx, 1, 1); err != nil || n != 0 {
			t.Fatal("active final lease exhausted", n, err)
		}
		if err := store.Sent(ctx, claim); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("SMTP acceptance before lost acknowledgment", func(t *testing.T) {
		id := seed(508, "crash@local.test")
		claim, ok, err := store.Claim(ctx, time.Minute, 3)
		if err != nil || !ok || claim.ID != id {
			t.Fatal(err)
		}
		if err := makeTransport("plain").Send(ctx, claim.Message); err != nil {
			t.Fatal(err)
		}
		first := <-messages
		// No Sent call: simulate a crash after SMTP success. The same outbox
		// record recovers with a fresh token and the same Message-ID.
		exec("UPDATE notifications SET lease_until=now()-interval '1 second' WHERE id=$1", id)
		retry, ok, err := emailpg.New(db).Claim(ctx, time.Minute, 3)
		if err != nil || !ok || retry.ID != id || retry.Token == claim.Token || retry.Attempt != 2 {
			t.Fatal("lost acknowledgment not recovered", err)
		}
		if err := makeTransport("plain").Send(ctx, retry.Message); err != nil {
			t.Fatal(err)
		}
		second := <-messages
		for _, message := range []string{first, second} {
			if !strings.Contains(message, "Message-ID: <"+id+"@local.test>") {
				t.Fatal("unstable duplicate trace")
			}
		}
		if err := store.Sent(ctx, retry); err != nil {
			t.Fatal(err)
		}
		state(id, "SENT", "", 2)
	})

	t.Run("worker entry point", func(t *testing.T) {
		id := seed(506, "worker@local.test")
		for key, value := range map[string]string{
			"DATABASE_URL": dsn, "SMTP_HOST": "127.0.0.1", "SMTP_PORT": strconv.Itoa(port),
			"SMTP_FROM": "tutti@local.test", "SMTP_TLS_MODE": "plain", "SMTP_USERNAME": "", "SMTP_PASSWORD": "",
			"SMTP_PASSWORD_FILE": "", "SMTP_CA_FILE": "",
			"WORKER_EMAIL_BATCH_SIZE": "1", "WORKER_EMAIL_INTERVAL": "1h", "WORKER_EXPIRATION_INTERVAL": "1h",
		} {
			t.Setenv(key, value)
		}
		runCtx, stop := context.WithCancel(ctx)
		defer stop()
		result := make(chan error, 1)
		go func() { result <- bootstrap.RunWorker(runCtx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
		select {
		case message := <-messages:
			if !strings.Contains(message, "Message-ID: <"+id+"@local.test>") {
				t.Fatal(message)
			}
		case err := <-result:
			t.Fatalf("worker stopped before delivery: %v", err)
		case <-ctx.Done():
			t.Fatal("worker did not deliver queued mail")
		}
		stop()
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		state(id, "SENT", "", 1)
	})
	t.Run("worker runtime container", func(t *testing.T) {
		workerImage, runner := os.Getenv("TUTTI_TEST_WORKER_IMAGE"), os.Getenv("TUTTI_TEST_CONTAINER")
		if workerImage == "" || runner == "" {
			t.Skip("run test_docker.ps1 for the runtime image smoke test")
		}
		id := seed(509, "container@local.test")
		docker := func(args ...string) string {
			t.Helper()
			out, err := osexec.CommandContext(ctx, "docker", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("Docker failed: %v: %s", err, out)
			}
			return strings.TrimSpace(string(out))
		}
		container := docker("run", "-d", "--network", "container:"+runner,
			"-e", "DATABASE_URL="+dsn, "-e", "SMTP_HOST=127.0.0.1", "-e", "SMTP_PORT="+strconv.Itoa(port),
			"-e", "SMTP_FROM=tutti@local.test", "-e", "SMTP_TLS_MODE=plain",
			"-e", "WORKER_EMAIL_BATCH_SIZE=1", "-e", "WORKER_EMAIL_INTERVAL=1h", "-e", "WORKER_EXPIRATION_INTERVAL=1h",
			workerImage, "/tutti-worker")
		defer func() { _ = osexec.Command("docker", "rm", "-f", container).Run() }()
		select {
		case message := <-messages:
			if !strings.Contains(message, "Message-ID: <"+id+"@local.test>") {
				t.Fatal("wrong container notification")
			}
		case <-ctx.Done():
			t.Fatal("container delivery timed out")
		}
		docker("stop", "--time", "10", container)
		state(id, "SENT", "", 1)
		logs := docker("logs", container)
		if !strings.Contains(logs, `"notification_id":"`+id+`"`) || !strings.Contains(logs, "Tutti worker stopped") {
			t.Fatal("missing outcome/shutdown logs")
		}
		for _, secret := range []string{"container@local.test", "Private fixture", "tutti_test", "claim_token"} {
			if strings.Contains(logs, secret) {
				t.Fatal("worker logs contain private data")
			}
		}
		if code := docker("inspect", "--format", "{{.State.ExitCode}}", container); code != "0" {
			t.Fatalf("shutdown exit code %s", code)
		}
	})
}
