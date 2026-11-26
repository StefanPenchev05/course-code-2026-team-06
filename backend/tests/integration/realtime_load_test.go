package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	mathrand "math/rand"
	"net/http"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	sessionredis "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/redis"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

type loadConnection struct {
	socket    *websocket.Conn
	stream    *processStream
	cookie    *http.Cookie
	process   *realtimeProcess
	handshake time.Duration
}

func loadPercentiles(values []time.Duration) map[string]float64 {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	result := map[string]float64{}
	for _, p := range []int{50, 95, 99} {
		result[fmt.Sprintf("p%d_ms", p)] = float64(values[int(math.Ceil(float64(len(values))*float64(p)/100))-1]) / float64(time.Millisecond)
	}
	return result
}

// Opt-in, deterministic workload; normal CI runs the short process recovery test.
// Synthetic session seeding excludes password hashing from connection latency;
// every upgrade and delivery still uses production Redis/PostgreSQL authorization.
func TestRealtime200Users(t *testing.T) {
	if os.Getenv("TUTTI_REALTIME_LOAD") != "1" {
		t.Skip("opt-in: TUTTI_REALTIME_LOAD=1")
	}
	isolatedRealtimeDatabase(t)
	redisControl := realtimeFaultRedis(t)
	f, now := productionRealtimeFixture(t)
	cookies := make([]*http.Cookie, 200)
	for i := range cookies {
		id := fixture(89000 + i)
		f.exec(t, "INSERT INTO users(id,name,email,password_hash) SELECT $1,'Synthetic load user',$2,password_hash FROM users WHERE id=$3", id, fmt.Sprintf("load-%d@uni.example", i), fixture(bookingStudent))
		f.exec(t, "INSERT INTO students VALUES($1::uuid,$1::text,$2)", id, fixture(403))
		f.exec(t, "INSERT INTO student_verifications(student_id,faculty_id,status,reviewed_at,reviewed_by_coordinator_id) VALUES($1,$2,'APPROVED',now(),$3)", id, fixture(401), fixture(bookingCoordinator))
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Fatal(err)
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		digest := sha256.Sum256([]byte(token))
		if err := sessionredis.New(f.redis).Create(f.ctx, base64.RawURLEncoding.EncodeToString(digest[:]), id, time.Hour); err != nil {
			t.Fatal(err)
		}
		cookies[i] = &http.Cookie{Name: f.cookie[bookingStudent].Name, Value: token}
	}
	f.exec(t, "DELETE FROM realtime_events") // initial synthetic seed only
	a, b := startRealtimeProcess(t, "load-test"), startRealtimeProcess(t, "load-test")
	processes := []*realtimeProcess{a, b}
	baseline := []observability.Snapshot{a.stats(t), b.stats(t)}
	for i, cookie := range cookies {
		processes[i%2].call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, cookie)
	}
	connections := make([]loadConnection, 210)
	semaphore := make(chan struct{}, 10)
	var wg sync.WaitGroup
	failures := make(chan error, len(connections))
	for i := range connections {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			p, cookie := processes[i%2], cookies[i%200]
			start := time.Now()
			c, _, err := websocket.Dial(f.ctx, p.base+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}})
			if err != nil {
				failures <- err
				return
			}
			connections[i] = loadConnection{c, readProcessSocket(f.ctx, c), cookie, p, time.Since(start)}
		}(i)
	}
	wg.Wait()
	close(failures)
	for _, c := range connections {
		if c.socket != nil {
			t.Cleanup(func() { _ = c.socket.CloseNow() })
		}
	}
	for err := range failures {
		t.Error("handshake failed", err)
	}
	if t.Failed() {
		return
	}
	awaitRealtime(t, 5*time.Second, "210 simultaneous authenticated sockets", func() bool { return a.stats(t).Connections+b.stats(t).Connections == 210 })
	peak := []observability.Snapshot{a.stats(t), b.stats(t)}
	// Controlled sampling records observed peaks, not an assertion about unsampled
	// instantaneous maxima. The sampler joins before reporting or process shutdown.
	var samples []observability.Snapshot
	sampleCtx, cancelSamples := context.WithCancel(f.ctx)
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				return
			case <-tick.C:
				for _, p := range processes {
					r, err := (&http.Client{Timeout: time.Second}).Get(p.monitor + "/metrics")
					if err != nil {
						continue
					}
					var s observability.Snapshot
					if json.NewDecoder(r.Body).Decode(&s) == nil {
						samples = append(samples, s)
					}
					r.Body.Close()
				}
			}
		}
	}()
	t.Cleanup(func() { cancelSamples(); <-sampleDone })
	// Ten real availability transactions are a fanout burst to all 210 sockets.
	start := time.Now()
	for i := range 10 {
		at := now.Add(time.Duration(7*24+i*2) * time.Hour)
		processes[i%2].call(t, 201, "POST", "/tutors/me/availability/slots", map[string]string{"starts_at": at.Format(time.RFC3339), "ends_at": at.Add(time.Hour).Format(time.RFC3339), "allowed_mode": "ONLINE"}, f.cookie[bookingTutor])
	}
	var latencies, handshakes []time.Duration
	duplicates := 0
	deliveries := 0
	// Receive timestamps are captured in the read loop, not when this loop drains.
	for _, c := range connections {
		handshakes = append(handshakes, c.handshake)
		seen := map[string]bool{}
		for range 10 {
			e := c.stream.event(t, app.AvailabilityChanged, fixture(bookingTutor))
			if seen[e.ID] {
				duplicates++
			}
			seen[e.ID] = true
			at, err := time.Parse(time.RFC3339Nano, e.OccurredAt)
			if err != nil {
				t.Fatal(err)
			}
			latencies = append(latencies, c.stream.receivedAt(e.ID).Sub(at))
			deliveries++
		}
	}
	normalDuration := time.Since(start)
	if duplicates != 0 {
		t.Fatal("duplicate normal-phase browser delivery", duplicates)
	}
	// Seeded churn restores watches and authoritative state after every reconnect.
	random := mathrand.New(mathrand.NewSource(89))
	for _, i := range random.Perm(200)[:12] {
		c := &connections[i]
		_ = c.socket.CloseNow()
		select {
		case <-c.stream.closed:
		case <-time.After(3 * time.Second):
			t.Fatal("reader leak")
		}
		c.socket = c.process.socket(t, f.ctx, c.cookie)
		c.stream = readProcessSocket(f.ctx, c.socket)
		c.process.call(t, 200, "GET", "/tutors/"+fixture(bookingTutor)+"/availability", nil, c.cookie)
		c.process.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, c.cookie)
	}
	// Mixed traffic uses real private booking and coordinator issue workflows;
	// public watchers must never receive their identifiers.
	actor := readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingStudent]))
	coordinator := readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingCoordinator]))
	payload := f.payload(96*time.Hour, bookingOffer, bookingSlot)
	payload["starts_at"] = now.Add(96 * time.Hour).Format(time.RFC3339)
	data := a.call(t, 201, "POST", "/sessions", payload, f.cookie[bookingStudent])
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	actor.event(t, app.BookingChanged, created.Data.ID)
	b.call(t, 200, "POST", tutorRequestsPath+created.Data.ID+"/accept", testMeeting, f.cookie[bookingTutor])
	actor.event(t, app.BookingChanged, created.Data.ID)
	data = a.call(t, 201, "POST", "/issues", map[string]string{"description": "Synthetic load issue", "session_id": created.Data.ID}, f.cookie[bookingStudent])
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	coordinator.event(t, app.CoordinationChanged, created.Data.ID)
	// FIFO marker on every user stream proves no booking/coordination identifiers
	// were delivered to public watchers during the mixed workload.
	for i := range cookies {
		f.exec(t, "SELECT enqueue_realtime_event('account.access_changed',$1)", fixture(89000+i))
	}
	for _, c := range connections {
		for {
			select {
			case e := <-c.stream.events:
				if e.Type == app.BookingChanged || e.Type == app.CoordinationChanged {
					t.Fatal("private identifier leaked to public watcher")
				}
				if e.Type == app.AccountAccessChanged {
					goto marked
				}
			case <-time.After(10 * time.Second):
				t.Fatal("mixed traffic marker missing")
			}
		}
	marked:
	}
	// Normal phase records monitoring before separate disruptive phases.
	cancelSamples()
	<-sampleDone
	normal := []observability.Snapshot{a.stats(t), b.stats(t)}
	// Drop subscriptions with all 212 sockets present; all must resynchronize.
	if err := f.redis.Do(f.ctx, "CLIENT", "KILL", "TYPE", "pubsub").Err(); err != nil {
		t.Fatal(err)
	}
	for _, c := range connections {
		select {
		case <-c.stream.closed:
		case <-time.After(8 * time.Second):
			t.Fatal("gap failed to close client")
		}
	}
	awaitRealtime(t, 10*time.Second, "resubscribe after 200-user gap", func() bool { return a.stats(t).Subscribed && b.stats(t).Subscribed })
	awaitRealtime(t, 5*time.Second, "normal sockets cleaned up", func() bool { return a.stats(t).Connections+b.stats(t).Connections == 0 })
	// Two clients deliberately never read control frames. Default heartbeat (20s
	// plus 5s pong budget) must remove them without changing production limits.
	slowBefore := a.stats(t).Counters["heartbeat_failures"] + b.stats(t).Counters["heartbeat_failures"]
	_ = a.socket(t, f.ctx, cookies[0])
	_ = b.socket(t, f.ctx, cookies[1])
	slowStart := time.Now()
	awaitRealtime(t, 35*time.Second, "non-reading clients evicted", func() bool {
		return a.stats(t).Counters["heartbeat_failures"]+b.stats(t).Counters["heartbeat_failures"] >= slowBefore+2
	})
	slowDuration := time.Since(slowStart)
	// Whole Redis restart is timed separately from normal delivery latency.
	redisControl("stop")
	awaitRealtime(t, 8*time.Second, "subscription detects stopped Redis", func() bool { return !a.stats(t).Subscribed && !b.stats(t).Subscribed })
	redisControl("start")
	recoveryStart := time.Now()
	awaitRealtime(t, 15*time.Second, "load fixture Redis recovery", func() bool { return a.stats(t).Subscribed && b.stats(t).Subscribed })
	recoveryDuration := time.Since(recoveryStart)
	awaitRealtime(t, 8*time.Second, "all socket gauges return to zero", func() bool { return a.stats(t).Connections+b.stats(t).Connections == 0 })
	after := []observability.Snapshot{a.stats(t), b.stats(t)}
	a.stop(t, false)
	b.stop(t, false)
	report := map[string]any{"users": 200, "simultaneous_sockets": 210, "handshake_failures": 0, "handshake": loadPercentiles(handshakes), "delivery": loadPercentiles(latencies), "deliveries": deliveries, "normal_seconds": normalDuration.Seconds(), "deliveries_per_second": float64(deliveries) / normalDuration.Seconds(), "duplicate_browser_events": duplicates, "churn_reconnects": 12, "slow_client_eviction_seconds": slowDuration.Seconds(), "redis_resubscribe_seconds": recoveryDuration.Seconds(), "baseline": baseline, "connected": peak, "normal_end": normal, "after_cleanup": after, "go": runtime.Version(), "goos": runtime.GOOS, "cpu_count": runtime.NumCPU(), "gomaxprocs": runtime.GOMAXPROCS(0), "api_cpu_user_seconds": a.cmd.ProcessState.UserTime().Seconds() + b.cmd.ProcessState.UserTime().Seconds(), "api_cpu_system_seconds": a.cmd.ProcessState.SystemTime().Seconds() + b.cmd.ProcessState.SystemTime().Seconds()}
	var maxHeap uint64
	var maxGoroutines int
	var maxPending int64
	for _, s := range samples {
		maxHeap = max(maxHeap, s.HeapBytes)
		maxGoroutines = max(maxGoroutines, s.Goroutines)
		maxPending = max(maxPending, s.Outbox.Pending)
	}
	report["observed_max_heap_bytes_per_process"] = maxHeap
	report["observed_max_goroutines_per_process"] = maxGoroutines
	report["observed_max_pending"] = maxPending
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("REALTIME_LOAD_RESULT %s", encoded)
}
