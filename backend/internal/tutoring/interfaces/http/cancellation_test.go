package tutoringhttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestCancellationErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cancel bool
		err    error
		status int
		silent bool
	}{
		{"client cancellation", true, context.Canceled, 0, true},
		{"wrapped client cancellation", true, fmt.Errorf("query: %w", context.Canceled), 0, true},
		{"unrelated error after cancellation", true, errors.New("database unavailable"), 500, false},
		{"operation cancelled on live request", false, context.Canceled, 500, false},
		{"genuine server failure", false, errors.New("database unavailable"), 500, false},
		{"deadline exceeded", false, context.DeadlineExceeded, 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			h := &Handler{logger: slog.New(slog.NewTextHandler(logs, nil))}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			r := httptest.NewRequest("GET", "/api/v1/test", nil).WithContext(ctx)
			r.Pattern = "GET /api/v1/test"
			w := httptest.NewRecorder()
			h.fail(w, r, tc.err)
			if tc.silent {
				if logs.Len() != 0 || w.Body.Len() != 0 || len(w.Header()) != 0 {
					t.Fatalf("client cancellation produced error response/log: %s %s", logs, w.Body)
				}
			} else {
				if w.Code != tc.status || !strings.Contains(logs.String(), "level=ERROR") {
					t.Fatalf("real error hidden: status %d logs %s", w.Code, logs)
				}
				if strings.Contains(w.Body.String(), "database unavailable") {
					t.Fatal("private persistence error leaked")
				}
			}
		})
	}
}
