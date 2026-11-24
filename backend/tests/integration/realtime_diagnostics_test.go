package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	"github.com/redis/go-redis/v9"
)

// Failure diagnostics run before deferred fixture/process cleanup. Use a fresh,
// bounded client so a broken pooled connection cannot obscure the server state.
func realtimeRecoveryDiagnostics(t *testing.T, namespace string, processes ...*realtimeProcess) {
	t.Helper()
	opts, err := redis.ParseURL(os.Getenv("TUTTI_TEST_REDIS_URL"))
	if err == nil {
		opts.DialTimeout, opts.ReadTimeout, opts.WriteTimeout = time.Second, time.Second, time.Second
		opts.MaxRetries, opts.ContextTimeoutEnabled = -1, true
		client := redis.NewClient(opts)
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ping := client.Ping(ctx).Err()
		counts, countErr := client.PubSubNumSub(ctx, "tutti:"+namespace+":realtime:v1").Result()
		cancel()
		t.Logf("Redis configured-endpoint PING=%t; subscriber-count-readable=%t; actual subscribers=%d", ping == nil, countErr == nil, counts["tutti:"+namespace+":realtime:v1"])
	}
	for i, process := range processes {
		if s, err := process.readStats(); err == nil {
			data, _ := json.Marshal(s) // fixed counters/gauges, no domain identifiers
			t.Logf("API %d monitoring: %s", i+1, data)
		} else {
			t.Logf("API %d monitoring unavailable", i+1)
		}
		t.Logf("API %d sanitized log summary: %s", i+1, process.sanitizedLogs())
	}
}

func (p *realtimeProcess) sanitizedLogs() string {
	file, err := os.Open(p.logPath)
	if err != nil {
		return "unavailable"
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "unavailable"
	}
	if _, err = file.Seek(max(0, info.Size()-16*1024), io.SeekStart); err != nil {
		return "unavailable"
	}
	data, err := io.ReadAll(io.LimitReader(file, 16*1024))
	if err != nil {
		return "unavailable"
	}
	return sanitizedRealtimeLogs(data)
}

// Emit only whitelisted numeric categories, never raw JSON fields or driver
// errors (which can contain endpoints, credentials or private domain data).
func sanitizedRealtimeLogs(data []byte) string {
	allowed := observability.New().Snapshot().Counters
	counts := map[string]int64{}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		var record struct {
			Message string           `json:"msg"`
			Counts  map[string]int64 `json:"counts"`
		}
		if json.Unmarshal(line, &record) == nil {
			switch record.Message {
			case "Realtime operational changes":
				for key, n := range record.Counts {
					if _, ok := allowed[key]; ok && n > 0 {
						counts[key] += n
					}
				}
			case "Tutti API listening":
				counts["api_listening"]++
			case "Realtime shutdown timed out":
				counts["shutdown_timeout_log"]++
			case "Realtime monitoring listener stopped":
				counts["monitor_stopped_log"]++
			}
		} else if strings.HasPrefix(string(line), "redis: ") {
			text := strings.ToLower(string(line))
			switch {
			case strings.Contains(text, "connection refused"), strings.Contains(text, "actively refused"):
				counts["redis_connection_refused_log"]++
			case strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"):
				counts["redis_timeout_log"]++
			case strings.Contains(text, "eof"):
				counts["redis_eof_log"]++
			case strings.Contains(text, "dial"):
				counts["redis_dial_error_log"]++
			default:
				counts["other_redis_log"]++
			}
		}
	}
	encoded, _ := json.Marshal(counts)
	return string(encoded)
}

func TestRealtimeDiagnosticLogRedaction(t *testing.T) {
	input := []byte("{\"msg\":\"Realtime operational changes\",\"counts\":{\"subscription_interruptions\":2,\"PRIVATE-cookie\":99},\"token\":\"PRIVATE-token\"}\n" +
		"{\"msg\":\"PRIVATE-email@example.test\",\"counts\":{\"dispatch_failed\":3}}\n" +
		"redis: dial tcp PRIVATE-endpoint: connection refused; password=PRIVATE-password\nPRIVATE-payload\n")
	result := sanitizedRealtimeLogs(input)
	if strings.Contains(result, "PRIVATE") || result != `{"redis_connection_refused_log":1,"subscription_interruptions":2}` {
		t.Fatal("unsafe or unhelpful log diagnostic", result)
	}
}

func TestRealtimeShutdownDuringRedisRecovery(t *testing.T) {
	isolatedRealtimeDatabase(t)
	redisControl := realtimeFaultRedis(t)
	f, _ := productionRealtimeFixture(t)
	const namespace = "shutdown-recovery"
	p := startRealtimeProcess(t, namespace)
	defer func() {
		if t.Failed() {
			realtimeRecoveryDiagnostics(t, namespace, p)
		}
	}()
	stream := readProcessSocket(f.ctx, p.socket(t, f.ctx, f.cookie[bookingStudent]))
	redisControl("stop")
	awaitRealtime(t, 8*time.Second, "subscriber is reconnecting while Redis is down", func() bool {
		s := p.stats(t)
		return !s.Subscribed && s.Counters["subscription_reconnect_attempts"] > 0
	})
	select {
	case <-stream.closed:
	case <-time.After(8 * time.Second):
		t.Fatal("interruption left a socket open")
	}
	started := time.Now()
	p.stop(t, false) // Existing 10-second bound includes joining all API goroutines.
	t.Logf("Shutdown during reconnection completed in %s", time.Since(started))
}
