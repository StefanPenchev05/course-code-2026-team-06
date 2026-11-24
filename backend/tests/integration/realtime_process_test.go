package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/bootstrap"
	identitypg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/infrastructure/postgres"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/platform/observability"
	app "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	outboxpg "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/postgres"
	fanout "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/infrastructure/redis"
	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"
)

// Each child executes the complete production API bootstrap in a separate OS
// process. Reusing the race-instrumented test executable avoids nested builds.
func TestRealtimeAPIProcess(t *testing.T) {
	if os.Getenv("TUTTI_REALTIME_API_CHILD") != "1" {
		t.Skip("subprocess entry point")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	if err := bootstrap.Run(ctx, slog.New(slog.NewJSONHandler(os.Stderr, nil))); err != nil {
		t.Fatal(err)
	}
}

type realtimeProcess struct {
	cmd           *exec.Cmd
	input         io.WriteCloser
	done          chan error
	base, monitor string
	logPath       string
	stopped       bool
}

func freeRealtimeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := l.Addr().String()
	_ = l.Close()
	return a
}

func awaitRealtime(t *testing.T, budget time.Duration, description string, condition func() bool) {
	t.Helper()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("timed out: " + description)
		case <-tick.C:
		}
	}
}

func startRealtimeProcess(t *testing.T, namespace string) *realtimeProcess {
	t.Helper()
	addr, monitor := freeRealtimeAddress(t), freeRealtimeAddress(t)
	p := &realtimeProcess{base: "http://" + addr, monitor: "http://" + monitor, done: make(chan error, 1)}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestRealtimeAPIProcess$", "-test.timeout=8m")
	config := map[string]string{"TUTTI_REALTIME_API_CHILD": "1", "DATABASE_URL": os.Getenv("TUTTI_TEST_DATABASE_URL"), "REDIS_URL": os.Getenv("TUTTI_TEST_REDIS_URL"), "HTTP_ADDR": addr, "PUBLIC_ORIGIN": origin, "UNIVERSITY_EMAIL_DOMAINS": "uni.example", "OIDC_ENABLED": "false", "ARGON2_MEMORY_KIB": "19456", "ARGON2_ITERATIONS": "2", "ARGON2_THREADS": "1", "ARGON2_CONCURRENCY": "2", "REALTIME_NAMESPACE": namespace, "REALTIME_MONITOR_ADDR": monitor}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, replace := config[key]; !replace {
			p.cmd.Env = append(p.cmd.Env, entry)
		}
	}
	for k, v := range config {
		p.cmd.Env = append(p.cmd.Env, k+"="+v)
	}
	log, err := os.Create(filepath.Join(t.TempDir(), "api.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	p.cmd.Stdout = log
	p.cmd.Stderr = log
	p.logPath = log.Name()
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() { p.stop(t, false) })
	awaitRealtime(t, 15*time.Second, "independent API readiness", func() bool {
		select {
		case err := <-p.done:
			p.stopped = true
			t.Fatalf("API exited: %v; sanitized logs: %s", err, p.sanitizedLogs())
		default:
		}
		r, err := (&http.Client{Timeout: time.Second}).Get(p.monitor + "/readyz")
		if err != nil {
			return false
		}
		defer r.Body.Close()
		return r.StatusCode == 200
	})
	return p
}

func (p *realtimeProcess) stop(t *testing.T, crash bool) {
	t.Helper()
	if p.stopped {
		return
	}
	p.stopped = true
	if crash {
		_ = p.cmd.Process.Kill()
	}
	_ = p.input.Close()
	select {
	case err := <-p.done:
		if !crash && err != nil {
			t.Error("API graceful stop", err)
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Error("API shutdown exceeded 10 seconds")
	}
}
func (p *realtimeProcess) stats(t *testing.T) observability.Snapshot {
	t.Helper()
	s, err := p.readStats()
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func (p *realtimeProcess) readStats() (observability.Snapshot, error) {
	var s observability.Snapshot
	r, err := (&http.Client{Timeout: 2 * time.Second}).Get(p.monitor + "/metrics")
	if err != nil {
		return s, err
	}
	defer r.Body.Close()
	err = json.NewDecoder(r.Body).Decode(&s)
	return s, err
}
func (p *realtimeProcess) call(t *testing.T, status int, method, path string, body any, cookie *http.Cookie) []byte {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, p.base+"/api/v1"+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	r, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	data, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if r.StatusCode != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, r.StatusCode, status, data)
	}
	return data
}
func (p *realtimeProcess) socket(t *testing.T, ctx context.Context, cookie *http.Cookie) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, p.base+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}, "Cookie": {cookie.Name + "=" + cookie.Value}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

// A dedicated disposable Redis instance allows destructive fault injection.
// Only the exact container ID returned by our docker run may be stopped/removed.
func realtimeFaultRedis(t *testing.T) func(string) {
	t.Helper()
	if os.Getenv("TUTTI_TEST_DATABASE_URL") == "" {
		t.Skip("run disposable integration runner")
	}
	event, _ := app.NewEvent(app.AccountAccessChanged, "", time.Now())
	name := "tutti-rt89-" + event.ID
	args := []string{"run", "-d", "--name", name, "--tmpfs", "/data:rw,size=32m"}
	network := os.Getenv("TUTTI_TEST_DOCKER_NETWORK")
	if network != "" {
		args = append(args, "--network", network, "--network-alias", name)
	} else {
		// Docker assigns a NEW host port on start when HostPort is empty/zero.
		// CI runs the APIs on the host: retain one explicit free port across the
		// outage rather than leaving their Redis clients at an abandoned endpoint.
		args = append(args, "-p", freeRealtimeAddress(t)+":6379")
	}
	args = append(args, "redis:8.2-alpine", "redis-server", "--save", "", "--appendonly", "no")
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("disposable Redis: %v %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		if out, err := exec.Command("docker", "rm", "-f", id).CombinedOutput(); err != nil {
			t.Errorf("cleanup own Redis: %v %s", err, out)
		}
	})
	address := name + ":6379"
	if network == "" {
		out, err := exec.Command("docker", "port", id, "6379/tcp").Output()
		if err != nil {
			t.Fatal(err)
		}
		address = strings.TrimSpace(string(out))
	}
	t.Setenv("TUTTI_TEST_REDIS_URL", "redis://"+address+"/0")
	probe := redis.NewClient(&redis.Options{Addr: address, DialTimeout: 250 * time.Millisecond, ReadTimeout: 250 * time.Millisecond, WriteTimeout: 250 * time.Millisecond, MaxRetries: -1, ContextTimeoutEnabled: true})
	t.Cleanup(func() { _ = probe.Close() })
	t.Cleanup(func() {
		if t.Failed() {
			for _, args := range [][]string{{"inspect", "--format", "{{.State.Status}}", id}, {"port", id, "6379/tcp"}, {"exec", id, "redis-cli", "ping"}} {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				out, err := exec.CommandContext(ctx, "docker", args...).Output()
				cancel()
				t.Logf("Redis container %s: %q (success=%t); configured endpoint=%s", args[0], strings.TrimSpace(string(out)), err == nil, address)
			}
		}
	})
	waitReady := func() {
		t.Helper()
		started := time.Now()
		awaitRealtime(t, 10*time.Second, "disposable Redis PING at configured endpoint", func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			return probe.Ping(ctx).Err() == nil
		})
		t.Logf("Redis configured endpoint ready in %s", time.Since(started))
	}
	waitReady()
	return func(action string) {
		t.Helper()
		if action != "stop" && action != "start" {
			t.Fatal("unsupported Redis fault")
		}
		if out, err := exec.Command("docker", action, id).CombinedOutput(); err != nil {
			t.Fatalf("own Redis %s: %v %s", action, err, out)
		}
		if action == "start" {
			if network == "" {
				out, err := exec.Command("docker", "port", id, "6379/tcp").Output()
				if err != nil || strings.TrimSpace(string(out)) != address {
					t.Fatalf("Redis published endpoint changed: configured=%s actual=%q error=%v", address, strings.TrimSpace(string(out)), err)
				}
			}
			// The caller's recovery deadline begins only after Redis is reachable
			// at the endpoint already configured in BOTH independent API processes.
			waitReady()
		}
	}
}

func productionRealtimeFixture(t *testing.T) (*bookingFixture, time.Time) {
	t.Helper()
	f := newBookingFixture(t, 5*time.Minute)
	now := time.Now().UTC().Truncate(time.Hour)
	f.exec(t, "DELETE FROM availability_slots")
	f.exec(t, "UPDATE semesters SET starts_at=$1,ends_at=$2 WHERE id=$3", now.Add(-24*time.Hour), now.Add(31*24*time.Hour), fixture(406))
	f.exec(t, "INSERT INTO availability_slots(id,tutor_id,starts_at,ends_at,allowed_mode) VALUES($1,$2,$3,$4,'ONLINE')", fixture(bookingSlot), fixture(bookingTutor), now.Add(72*time.Hour), now.Add(120*time.Hour))
	f.exec(t, "DELETE FROM realtime_events")
	return f, now
}

type processStream struct {
	events chan app.Event
	closed chan struct{}
	mu     sync.Mutex
	times  map[string]time.Time
}

func (s *processStream) receivedAt(id string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.times[id]
}

func readProcessSocket(ctx context.Context, c *websocket.Conn) *processStream {
	s := &processStream{events: make(chan app.Event, 512), closed: make(chan struct{}), times: map[string]time.Time{}}
	go func() {
		defer close(s.closed)
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			e, err := app.Decode(b)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.times[e.ID] = time.Now()
			s.mu.Unlock()
			select {
			case s.events <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return s
}
func (s *processStream) event(t *testing.T, kind app.Type, entity string) app.Event {
	t.Helper()
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e := <-s.events:
			if e.Type == kind && e.EntityID == entity {
				return e
			}
		case <-s.closed:
			t.Fatal("socket unexpectedly closed")
		case <-timer.C:
			t.Fatalf("missing %s event", kind)
		}
	}
}

func TestRealtimeIndependentProcessesRecovery(t *testing.T) {
	isolatedRealtimeDatabase(t)
	redisControl := realtimeFaultRedis(t)
	f, now := productionRealtimeFixture(t)
	const namespace = "process-test"
	a, b := startRealtimeProcess(t, namespace), startRealtimeProcess(t, namespace)
	defer func() {
		if t.Failed() {
			realtimeRecoveryDiagnostics(t, namespace, a, b)
		}
	}()
	student := readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingStudent]))
	tutor := readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingTutor]))
	outsider := readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingOtherStudent]))
	wrongFaculty := readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingHomeCoordinator]))
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
	id := created.Data.ID
	first := tutor.event(t, app.BookingChanged, id)
	student.event(t, app.BookingChanged, id)
	b.call(t, 200, "POST", tutorRequestsPath+id+"/accept", testMeeting, f.cookie[bookingTutor])
	student.event(t, app.BookingChanged, id)
	tutor.event(t, app.BookingChanged, id)
	b.call(t, 404, "GET", "/sessions/"+id, nil, f.cookie[bookingOtherStudent])
	a.call(t, 403, "GET", "/sessions/"+id, nil, f.cookie[bookingHomeCoordinator])
	// A FIFO marker through the same subscription proves excluded sockets did not
	// receive a private booking before their own account invalidation.
	for _, x := range []struct {
		user int
		s    *processStream
	}{{bookingOtherStudent, outsider}, {bookingHomeCoordinator, wrongFaculty}} {
		f.exec(t, "SELECT enqueue_realtime_event('account.access_changed',$1)", fixture(x.user))
		select {
		case e := <-x.s.events:
			if e.Type != app.AccountAccessChanged || e.EntityID != "" {
				t.Fatal("private event leaked across user/faculty boundary")
			}
		case <-time.After(8 * time.Second):
			t.Fatal("isolation marker not delivered")
		}
	}
	// Verify namespace isolation for both channel routing and watch state.
	other, _ := fanout.NewScoped(f.redis, "other-deployment")
	otherSub, err := other.Subscribe(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer otherSub.Close()
	wa, _ := fanout.NewScopedWatches(f.redis, namespace)
	wb, _ := fanout.NewScopedWatches(f.redis, "other-deployment")
	a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingOtherStudent])
	if ok, err := wb.Contains(f.ctx, fixture(bookingOtherStudent), fixture(bookingTutor)); err != nil || ok {
		t.Fatal("watch namespace leak", err)
	}
	// Repeated/delayed IDs invalidate current state; they cannot roll back HTTP data.
	beforeA, beforeB := a.stats(t).Counters["duplicates_suppressed"], b.stats(t).Counters["duplicates_suppressed"]
	bus, _ := fanout.NewScoped(f.redis, namespace)
	if err = bus.Publish(f.ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	awaitRealtime(t, 5*time.Second, "duplicate suppressed on both processes", func() bool {
		return a.stats(t).Counters["duplicates_suppressed"] > beforeA && b.stats(t).Counters["duplicates_suppressed"] > beforeB
	})
	if !bytes.Contains(a.call(t, 200, "GET", "/sessions/"+id, nil, f.cookie[bookingStudent]), []byte("ACCEPTED")) {
		t.Fatal("refetch did not return current state")
	}
	// Force a subscription-only gap while HTTP authentication remains usable.
	recovery := a.stats(t).Counters["subscription_recoveries"]
	if err = f.redis.Do(f.ctx, "CLIENT", "KILL", "TYPE", "pubsub").Err(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-student.closed:
	case <-time.After(8 * time.Second):
		t.Fatal("gap did not close stale browser")
	}
	awaitRealtime(t, 10*time.Second, "subscription re-established", func() bool {
		return a.stats(t).Subscribed && b.stats(t).Subscribed && a.stats(t).Counters["subscription_recoveries"] > recovery
	})
	// A viewer misses a real availability mutation while its socket is down.
	windowStart := now.Add(7 * 24 * time.Hour)
	window := b.call(t, 201, "POST", "/tutors/me/availability/slots", map[string]string{"starts_at": windowStart.Format(time.RFC3339), "ends_at": windowStart.Add(time.Hour).Format(time.RFC3339), "allowed_mode": "ONLINE"}, f.cookie[bookingTutor])
	var newSlot struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err = json.Unmarshal(window, &newSlot); err != nil {
		t.Fatal(err)
	}
	_ = readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingOtherStudent]))
	availability := a.call(t, 200, "GET", "/tutors/"+fixture(bookingTutor)+"/availability?from="+windowStart.Format(time.RFC3339)+"&to="+windowStart.Add(24*time.Hour).Format(time.RFC3339), nil, f.cookie[bookingOtherStudent])
	if !bytes.Contains(availability, []byte(newSlot.Data.ID)) {
		t.Fatal("reconnected viewer did not refetch new availability")
	}
	student = readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingStudent]))
	a.call(t, 200, "GET", "/sessions/"+id, nil, f.cookie[bookingStudent])
	a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingOtherStudent])
	if ok, err := wa.Contains(f.ctx, fixture(bookingOtherStudent), fixture(bookingTutor)); err != nil || !ok {
		t.Fatal("watch renewal failed", err)
	}
	// Redis outage occurs after authentication and cannot roll back a durable
	// business commit. This trusted service call represents an in-flight request.
	principal, err := identitypg.New(f.db).Principal(f.ctx, fixture(bookingStudent))
	if err != nil {
		t.Fatal(err)
	}
	redisControl("stop")
	started := time.Now()
	f.setTime(time.Now().UTC().Sub(f.start))
	if _, err = f.service.Cancel(f.ctx, principal, id, "synthetic outage test"); err != nil {
		t.Fatal("business commit depended on Redis", err)
	}
	awaitRealtime(t, 12*time.Second, "pending dispatch failure retained", func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL AND last_error='fanout_failed'").Scan(&n)
		return err == nil && n > 0
	})
	if a.stats(t).Subscribed {
		t.Fatal("subscription remained healthy during outage")
	}
	select {
	case <-student.closed:
	case <-time.After(8 * time.Second):
		t.Fatal("Redis interruption did not close the socket")
	}
	recoveryA, recoveryB := a.stats(t).Counters["subscription_recoveries"], b.stats(t).Counters["subscription_recoveries"]
	redisControl("start")
	recoveryStarted := time.Now()
	awaitRealtime(t, 15*time.Second, "Redis subscription recovery", func() bool {
		x, y := a.stats(t), b.stats(t)
		count, err := f.redis.PubSubNumSub(f.ctx, "tutti:"+namespace+":realtime:v1").Result()
		return err == nil && count["tutti:"+namespace+":realtime:v1"] == 2 && x.Subscribed && y.Subscribed && x.Counters["subscription_recoveries"] > recoveryA && y.Counters["subscription_recoveries"] > recoveryB
	})
	t.Logf("Both actual subscriptions and monitoring recovered in %s after Redis readiness", time.Since(recoveryStarted))
	a.call(t, 401, "GET", "/sessions/"+id, nil, f.cookie[bookingStudent]) // volatile session store was lost
	f.cookie[bookingStudent] = f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": "book-student@uni.example", "password": password}, nil).Result().Cookies()[0]
	student = readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingStudent]))
	if !bytes.Contains(a.call(t, 200, "GET", "/sessions/"+id, nil, f.cookie[bookingStudent]), []byte("CANCELLED")) {
		t.Fatal("reconnect refetch lost committed cancellation")
	}
	// A NEW HTTP commit must cross the recovered subscriptions, not merely
	// disappear from the outbox. Recheck excluded users/faculties after reconnect.
	for actor, email := range map[int]string{bookingTutor: "book-tutor", bookingOtherStudent: "book-other-student", bookingHomeCoordinator: "book-home-coord"} {
		f.cookie[actor] = f.a.call(t, 200, "POST", "/auth/login", map[string]string{"email": email + "@uni.example", "password": password}, nil).Result().Cookies()[0]
	}
	tutor = readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingTutor]))
	outsider = readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingOtherStudent]))
	wrongFaculty = readProcessSocket(f.ctx, a.socket(t, f.ctx, f.cookie[bookingHomeCoordinator]))
	payload["starts_at"] = now.Add(98 * time.Hour).Format(time.RFC3339)
	data = a.call(t, 201, "POST", "/sessions", payload, f.cookie[bookingStudent])
	if err = json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	recoveredID := created.Data.ID
	student.event(t, app.BookingChanged, recoveredID)
	tutor.event(t, app.BookingChanged, recoveredID)
	b.call(t, 200, "POST", tutorRequestsPath+recoveredID+"/accept", testMeeting, f.cookie[bookingTutor])
	student.event(t, app.BookingChanged, recoveredID)
	tutor.event(t, app.BookingChanged, recoveredID)
	for _, x := range []struct {
		user   int
		stream *processStream
	}{{bookingOtherStudent, outsider}, {bookingHomeCoordinator, wrongFaculty}} {
		f.exec(t, "SELECT enqueue_realtime_event('account.access_changed',$1)", fixture(x.user))
		select {
		case e := <-x.stream.events:
			if e.Type != app.AccountAccessChanged || e.EntityID != "" {
				t.Fatal("private event leaked after recovery")
			}
		case <-time.After(8 * time.Second):
			t.Fatal("post-recovery isolation marker missing")
		}
	}
	awaitRealtime(t, 15*time.Second, "outbox drains after recovery", func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL").Scan(&n)
		return err == nil && n == 0
	})
	t.Logf("Redis stop/start and backlog recovery: %s", time.Since(started))
	// A committed event is published but its acknowledgment is lost. Inject the
	// lease atomically so live dispatchers cannot claim it before this fault point.
	var lostID, token string
	err = f.db.QueryRow(f.ctx, `INSERT INTO realtime_events(event_type,entity_id,claim_token,lease_until,attempt_count)
 VALUES('account.access_changed',$1,gen_random_uuid(),now()+interval '1 minute',1) RETURNING id::text,claim_token::text`, fixture(bookingStudent)).Scan(&lostID, &token)
	if err != nil {
		t.Fatal(err)
	}
	if err = bus.Publish(f.ctx, lostID); err != nil {
		t.Fatal(err)
	}
	student.event(t, app.AccountAccessChanged, "")
	baseline := a.stats(t).Counters["duplicates_suppressed"]
	f.exec(t, "UPDATE realtime_events SET lease_until=now()-interval '1 second' WHERE id=$1", lostID)
	awaitRealtime(t, 8*time.Second, "expired lease reclaimed without duplicate browser delivery", func() bool { return a.stats(t).Counters["duplicates_suppressed"] > baseline })
	if err = outboxpg.New(f.db).Dispatched(f.ctx, app.Claim{Event: app.Event{ID: lostID}, Token: token}); err != app.ErrClaimLost {
		t.Fatal("stale dispatch acknowledgment accepted", err)
	}
	// Deliver previously unseen events in reverse timestamp order. Each remains
	// an invalidation of current HTTP state; it carries no stale status to apply.
	var older, newer string
	for i, destination := range []*string{&older, &newer} {
		if err = f.db.QueryRow(f.ctx, `INSERT INTO realtime_events(event_type,entity_id,occurred_at,claim_token,lease_until)
		VALUES('account.access_changed',$1,now()-$2::int*interval '1 second',gen_random_uuid(),now()+interval '1 minute') RETURNING id::text`, fixture(bookingStudent), 10-i).Scan(destination); err != nil {
			t.Fatal(err)
		}
	}
	for _, eventID := range []string{newer, older} {
		if err = bus.Publish(f.ctx, eventID); err != nil {
			t.Fatal(err)
		}
		if e := student.event(t, app.AccountAccessChanged, ""); e.ID != eventID {
			t.Fatal("unexpected delayed event")
		}
		if !bytes.Contains(a.call(t, 200, "GET", "/sessions/"+id, nil, f.cookie[bookingStudent]), []byte("CANCELLED")) {
			t.Fatal("out-of-order event changed authoritative state")
		}
	}
	f.exec(t, "UPDATE realtime_events SET lease_until=now()-interval '1 second' WHERE id IN ($1,$2)", older, newer)
	// Independent worker commits expiry while both API processes are stopped.
	payload["starts_at"] = now.Add(100 * time.Hour).Format(time.RFC3339)
	data = a.call(t, 201, "POST", "/sessions", payload, f.cookie[bookingStudent])
	if err = json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	pendingID := created.Data.ID
	var expiry time.Time
	if err = f.db.QueryRow(f.ctx, "SELECT expires_at FROM tutoring_sessions WHERE id=$1", pendingID).Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	a.stop(t, true)
	b.stop(t, false)
	child := exec.CommandContext(f.ctx, os.Args[0], "-test.run=^TestRealtimeExpirationProcess$")
	child.Env = append(os.Environ(), "TUTTI_REALTIME_EXPIRATION_CHILD=1", "TUTTI_REALTIME_EXPIRATION_AT="+expiry.Add(time.Second).Format(time.RFC3339))
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("worker: %v %s", err, out)
	}
	a = startRealtimeProcess(t, namespace)
	b = startRealtimeProcess(t, namespace)
	restarted := readProcessSocket(f.ctx, b.socket(t, f.ctx, f.cookie[bookingStudent]))
	if !bytes.Contains(b.call(t, 200, "GET", "/sessions/"+pendingID, nil, f.cookie[bookingStudent]), []byte("EXPIRED")) {
		t.Fatal("restart refetch missed worker expiry")
	}
	a.call(t, 200, "POST", "/realtime/availability-watches", map[string]string{"tutor_id": fixture(bookingTutor)}, f.cookie[bookingStudent])
	awaitRealtime(t, 10*time.Second, "worker events dispatched after API restart", func() bool {
		var n int
		err := f.db.QueryRow(f.ctx, "SELECT count(*) FROM realtime_events WHERE dispatched_at IS NULL").Scan(&n)
		return err == nil && n == 0
	})
	payload["starts_at"] = now.Add(104 * time.Hour).Format(time.RFC3339)
	data = a.call(t, 201, "POST", "/sessions", payload, f.cookie[bookingStudent])
	if err = json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	restarted.event(t, app.BookingChanged, created.Data.ID)
}

func TestRealtimeNamespaceIsolation(t *testing.T) {
	url := os.Getenv("TUTTI_TEST_REDIS_URL")
	if url == "" {
		t.Skip("run disposable integration runner")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(opts)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, _ := fanout.NewScoped(client, "isolation-a")
	b, _ := fanout.NewScoped(client, "isolation-b")
	sa := client.Subscribe(ctx, "tutti:isolation-a:realtime:v1")
	defer sa.Close()
	sb := client.Subscribe(ctx, "tutti:isolation-b:realtime:v1")
	defer sb.Close()
	if _, err = sa.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = sb.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	ea, _ := app.NewEvent(app.AccountAccessChanged, "", time.Now())
	eb, _ := app.NewEvent(app.AccountAccessChanged, "", time.Now())
	if err = a.Publish(ctx, ea.ID); err != nil {
		t.Fatal(err)
	}
	if err = b.Publish(ctx, eb.ID); err != nil {
		t.Fatal(err)
	}
	ma, err := sa.ReceiveMessage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mb, err := sb.ReceiveMessage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ma.Payload != ea.ID || mb.Payload != eb.ID {
		t.Fatal("publication crossed namespaces")
	}
	legacy := client.Subscribe(ctx, fanout.Channel)
	defer legacy.Close()
	if _, err = legacy.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if sub, err := a.Subscribe(ctx); err == nil {
		_ = sub.Close()
		t.Fatal("mixed legacy/namespaced startup was accepted")
	}
}
