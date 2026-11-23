package cli

import (
	"bytes"
	"context"
	"errors"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIValidationBeforeDatabase(t *testing.T) {
	for _, args := range [][]string{{"update"}, {"delete"}, {"list", "--unknown"}, {"create"}, {"create", "--dry-run", "--approve"}, {"list", "extra"}, {"create", "--file", "x", "--file", "y"}, {"inspect-impact", "--id", "bad"}, {"rollover-check", "--from", "bad"}} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), args, &out, &stderr, func(context.Context) (*application.CalendarService, func(), error) {
			t.Fatal("invalid input opened DB")
			return nil, nil, nil
		})
		if code != 2 || stderr.Len() == 0 {
			t.Fatalf("%v: %d %s", args, code, stderr.String())
		}
	}
	for _, args := range [][]string{nil, {"--help"}, {"create", "--help"}} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr, nil); code != 0 || !strings.Contains(out.String(), "rollover-check") {
			t.Fatalf("help %d", code)
		}
	}
}

func TestManifestStrictness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	for _, data := range []string{`{"id":"x","id":"y"}`, `{"ID":"x","id":"y"}`, `{"unknown":"x"}`, `{} {}`, `[]`, `{"id":1}`, `{"id":`, string([]byte{0xff}), strings.Repeat(" ", 16385)} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readManifest(path); err == nil {
			t.Fatalf("accepted invalid manifest %.60q", data)
		}
	}
}

func TestCLIConnectionErrorDoesNotExposeDSN(t *testing.T) {
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"list"}, &out, &stderr, func(context.Context) (*application.CalendarService, func(), error) {
		return nil, nil, errors.New("postgres://secret:password@host")
	})
	if code != 1 || strings.Contains(stderr.String(), "password") || strings.Contains(stderr.String(), "secret") {
		t.Fatalf("%d %s", code, stderr.String())
	}
}
