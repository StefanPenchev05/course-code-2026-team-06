package observability

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentMetricsAreBoundedAndSnapshotsDetached(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				m.Inc("dispatch_failed")
				m.Inc("private-user-id")
				m.Observe("publish", time.Millisecond)
				m.Connection(1)
				m.Connection(-1)
			}
		}()
	}
	wg.Wait()
	s := m.Snapshot()
	if s.Counters["dispatch_failed"] != 2000 || s.Connections != 0 || s.Durations["publish"].Count != 2000 {
		t.Fatal(s)
	}
	if _, ok := s.Counters["private-user-id"]; ok {
		t.Fatal("unbounded labels")
	}
	s.Counters["dispatch_failed"] = 0
	if m.Snapshot().Counters["dispatch_failed"] != 2000 {
		t.Fatal("snapshot aliases mutable state")
	}
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	m.LogFailures(logger)
	first := log.String()
	m.LogFailures(logger)
	if log.String() != first || !strings.Contains(first, "dispatch_failed") || strings.Contains(first, "private-user-id") {
		t.Fatal("logs must contain bounded aggregate changes only")
	}
	var disabled *Metrics
	disabled.Inc("dispatch_failed")
	disabled.Observe("publish", time.Second)
	disabled.Connection(1)
	disabled.Subscribed(true)
}
