package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func workerEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]string{
		"WORKER_EXPIRATION_INTERVAL": "10s", "WORKER_EMAIL_INTERVAL": "5s",
		"WORKER_EMAIL_LEASE": "90s", "WORKER_EMAIL_RETRY_BASE": "30s", "WORKER_EMAIL_RETRY_MAX": "30m",
		"SMTP_SEND_TIMEOUT": "20s", "SMTP_CONNECT_TIMEOUT": "5s", "SMTP_PORT": "587",
		"WORKER_EXPIRATION_BATCH_SIZE": "50", "WORKER_EMAIL_BATCH_SIZE": "20", "WORKER_EMAIL_MAX_ATTEMPTS": "5",
		"SMTP_HOST": "smtp.example", "SMTP_FROM": "tutti@uni.example", "SMTP_TLS_MODE": "starttls",
		"SMTP_USERNAME": "", "SMTP_PASSWORD": "", "SMTP_PASSWORD_FILE": "", "SMTP_CA_FILE": "",
		"DATABASE_URL": "",
	} {
		t.Setenv(key, value)
	}
}

func TestWorkerSettings(t *testing.T) {
	workerEnvironment(t)
	c, err := workerSettings()
	if err != nil {
		t.Fatal(err)
	}
	if c.email.Lease != 90*time.Second || c.email.MaxAttempts != 5 || c.smtp.SendTimeout != c.email.SendTimeout || c.smtp.TLSMode != "starttls" {
		t.Fatal("unexpected effective settings")
	}
	for _, tc := range []struct{ key, value string }{
		{"WORKER_EMAIL_INTERVAL", "0s"}, {"WORKER_EMAIL_INTERVAL", "1.5s"}, {"WORKER_EMAIL_INTERVAL", "2h"},
		{"WORKER_EXPIRATION_INTERVAL", "2h"}, {"WORKER_EMAIL_INTERVAL", "invalid"},
		{"WORKER_EMAIL_BATCH_SIZE", "0"}, {"WORKER_EMAIL_BATCH_SIZE", "101"},
		{"WORKER_EMAIL_MAX_ATTEMPTS", "21"}, {"WORKER_EMAIL_MAX_ATTEMPTS", "-1"},
		{"WORKER_EXPIRATION_BATCH_SIZE", "501"}, {"SMTP_PORT", "65536"}, {"SMTP_PORT", "abc"},
		{"SMTP_SEND_TIMEOUT", "61s"}, {"SMTP_CONNECT_TIMEOUT", "0s"},
		{"WORKER_EMAIL_LEASE", "30s"}, {"WORKER_EMAIL_LEASE", "3601s"},
		{"WORKER_EMAIL_RETRY_BASE", "31m"}, {"WORKER_EMAIL_RETRY_MAX", "25h"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := workerSettings(); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestSMTPStartupValidationBeforeDatabase(t *testing.T) {
	workerEnvironment(t)
	for _, tc := range []struct{ key, value, want string }{
		{"SMTP_HOST", "", "SMTP_HOST"}, {"SMTP_FROM", "", "SMTP_FROM"},
		{"SMTP_TLS_MODE", "invalid", "SMTP_TLS_MODE"}, {"SMTP_USERNAME", "fake-user", "SMTP_PASSWORD"},
		{"SMTP_CONNECT_TIMEOUT", "21s", "SMTP timeouts"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			err := RunWorker(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s, got %v", tc.want, err)
			}
		})
	}
}

func TestSMTPPasswordFile(t *testing.T) {
	workerEnvironment(t)
	path := filepath.Join(t.TempDir(), "smtp-password")
	t.Setenv("SMTP_PASSWORD_FILE", path)
	t.Setenv("SMTP_USERNAME", "fake-user")
	for _, secret := range []string{"fake-secret\n", "fake-secret\r\n", "fake-secret"} {
		if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := workerSettings()
		if err != nil || c.smtp.Password != "fake-secret" {
			t.Fatal("secret file not loaded correctly")
		}
	}
	for _, tc := range []struct{ name, content string }{
		{"empty", ""}, {"too large", strings.Repeat("s", 4097)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := workerSettings(); err == nil {
				t.Fatal("invalid secret file accepted")
			}
		})
	}
	t.Setenv("SMTP_PASSWORD", "never-print-this-secret")
	_, err := workerSettings()
	if err == nil || strings.Contains(err.Error(), "never-print-this-secret") {
		t.Fatal("expected safe conflict error")
	}
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("SMTP_PASSWORD_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := workerSettings(); err == nil {
		t.Fatal("missing secret file accepted")
	}
}
