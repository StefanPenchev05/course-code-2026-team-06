// Package observability provides bounded, process-local operational measurements.
// It has no domain identifiers, payloads, credentials or external dependencies.
package observability

import (
	"log/slog"
	"runtime"
	"sync"
	"time"
)

var counters = []string{
	"connections_accepted", "connections_rejected", "connections_disconnected", "connections_limit_rejected",
	"handshake_origin", "handshake_request", "handshake_session", "handshake_dependency", "handshake_upgrade",
	"slow_clients", "queue_drops", "authentication_failures", "authorization_failures", "authorization_denied",
	"write_failures", "heartbeat_failures", "resynchronizations", "shutdown_failures",
	"dispatch_claimed", "dispatch_published", "dispatch_failed", "dispatch_retried", "dispatch_claim_failed", "dispatch_ack_failed", "dispatch_retry_failed",
	"processing_failed", "events_processed", "duplicates_suppressed",
	"subscription_interruptions", "subscription_reconnect_attempts", "subscription_recoveries", "fanout_failures",
}

type Duration struct {
	Count        int64   `json:"count"`
	TotalSeconds float64 `json:"total_seconds"`
	MaxSeconds   float64 `json:"max_seconds"`
}
type Outbox struct {
	Pending       int64   `json:"pending"`
	OldestSeconds float64 `json:"oldest_pending_seconds"`
	Retries       int64   `json:"pending_retry_attempts"`
	ExpiredLeases int64   `json:"expired_leases"`
	FanoutFailed  int64   `json:"pending_fanout_failed"`
	Bytes         int64   `json:"table_and_index_bytes"`
}
type Snapshot struct {
	Counters        map[string]int64    `json:"counters"`
	Durations       map[string]Duration `json:"durations"`
	Connections     int64               `json:"connections"`
	PeakConnections int64               `json:"peak_connections"`
	Subscribed      bool                `json:"subscribed"`
	Postgres        bool                `json:"postgres"`
	Redis           bool                `json:"redis"`
	OutboxReady     bool                `json:"outbox_ready"`
	SampledAt       time.Time           `json:"sampled_at"`
	Outbox          Outbox              `json:"outbox"`
	Goroutines      int                 `json:"goroutines"`
	HeapBytes       uint64              `json:"heap_bytes"`
}
type Metrics struct {
	mu       sync.Mutex
	snapshot Snapshot
	logged   map[string]int64
}

func New() *Metrics {
	m := &Metrics{snapshot: Snapshot{Counters: map[string]int64{}, Durations: map[string]Duration{}}, logged: map[string]int64{}}
	for _, name := range counters {
		m.snapshot.Counters[name] = 0
	}
	for _, name := range []string{"publish", "capture_to_publish", "process", "handshake"} {
		m.snapshot.Durations[name] = Duration{}
	}
	return m
}

// Unknown keys are ignored: even a mistaken caller cannot create high cardinality.
func (m *Metrics) Inc(name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.snapshot.Counters[name]; ok {
		m.snapshot.Counters[name]++
	}
}
func (m *Metrics) Observe(name string, d time.Duration) {
	if m == nil || d < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.snapshot.Durations[name]
	if !ok {
		return
	}
	v.Count++
	v.TotalSeconds += d.Seconds()
	v.MaxSeconds = max(v.MaxSeconds, d.Seconds())
	m.snapshot.Durations[name] = v
}
func (m *Metrics) Connection(delta int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Connections += delta
	m.snapshot.PeakConnections = max(m.snapshot.PeakConnections, m.snapshot.Connections)
}
func (m *Metrics) Subscribed(value bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Subscribed = value
}
func (m *Metrics) Health(pg, redis, ready bool, outbox Outbox) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.snapshot.Postgres = pg
	m.snapshot.Redis = redis
	m.snapshot.OutboxReady = ready
	m.snapshot.Outbox = outbox
	m.snapshot.SampledAt = time.Now().UTC()
}
func (m *Metrics) Snapshot() Snapshot {
	m.mu.Lock()
	s := m.snapshot
	s.Counters = map[string]int64{}
	s.Durations = map[string]Duration{}
	for k, v := range m.snapshot.Counters {
		s.Counters[k] = v
	}
	for k, v := range m.snapshot.Durations {
		s.Durations[k] = v
	}
	m.mu.Unlock()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	s.HeapBytes = mem.HeapAlloc
	s.Goroutines = runtime.NumGoroutine()
	return s
}

// Called by the bounded operations poll, never while the hub is delivering.
// Aggregate deltas avoid attacker-controlled log volume and contain no raw errors.
func (m *Metrics) LogFailures(logger *slog.Logger) {
	m.mu.Lock()
	changes := map[string]int64{}
	for _, name := range []string{"connections_rejected", "connections_limit_rejected", "slow_clients", "queue_drops", "authentication_failures", "authorization_failures", "write_failures", "heartbeat_failures", "resynchronizations", "shutdown_failures", "dispatch_failed", "dispatch_claim_failed", "dispatch_ack_failed", "dispatch_retry_failed", "dispatch_retried", "processing_failed", "subscription_interruptions", "subscription_reconnect_attempts"} {
		n := m.snapshot.Counters[name]
		if n > m.logged[name] {
			changes[name] = n - m.logged[name]
			m.logged[name] = n
		}
	}
	m.mu.Unlock()
	if len(changes) > 0 {
		logger.Warn("Realtime operational changes", "counts", changes)
	}
}
