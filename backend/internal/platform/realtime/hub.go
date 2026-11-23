// Package realtime implements bounded, process-local WebSocket transport.
package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	events "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/realtime/application"
	"github.com/coder/websocket"
)

var (
	ErrClosed = errors.New("realtime hub closed")
	ErrLimit  = errors.New("realtime connection limit reached")
	ErrPolicy = errors.New("explicit realtime recipients and authorization required")
)

type Config struct {
	MaxConnections, MaxPerUser, QueueSize                              int
	AuthInterval, AuthTimeout, PingInterval, PingTimeout, WriteTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{MaxConnections: 1024, MaxPerUser: 4, QueueSize: 32,
		AuthInterval: 15 * time.Second, AuthTimeout: 3 * time.Second,
		PingInterval: 20 * time.Second, PingTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
}

func (c Config) valid() bool {
	return c.MaxConnections > 0 && c.MaxConnections <= 10000 && c.MaxPerUser > 0 && c.MaxPerUser <= c.MaxConnections &&
		c.QueueSize > 0 && c.QueueSize <= 256 && c.AuthInterval > 0 && c.AuthInterval <= time.Minute &&
		c.AuthTimeout > 0 && c.AuthTimeout <= 5*time.Second && c.PingInterval > 0 && c.PingInterval <= time.Minute &&
		c.PingTimeout > 0 && c.PingTimeout <= 10*time.Second && c.WriteTimeout > 0 && c.WriteTimeout <= 10*time.Second
}

type delivery struct {
	event     events.Event
	data      []byte
	authorize events.Authorize
}

type connection struct {
	ctx       context.Context
	cancel    context.CancelFunc
	principal domain.Principal
	check     func(context.Context) (domain.Principal, error)
	queue     chan delivery
	closeCode atomic.Int32
}

func (c *connection) stop(code websocket.StatusCode) {
	c.closeCode.CompareAndSwap(0, int32(code))
	c.cancel()
}

type Hub struct {
	config Config
	mu     sync.Mutex
	closed bool
	users  map[string]map[*connection]struct{}
	total  int
	wg     sync.WaitGroup
	stop   sync.Once
	done   chan struct{}
}

var _ events.Publisher = (*Hub)(nil)

func NewHub(config Config) (*Hub, error) {
	if !config.valid() {
		return nil, errors.New("invalid realtime limits or timeouts")
	}
	return &Hub{config: config, users: make(map[string]map[*connection]struct{}), done: make(chan struct{})}, nil
}

// reserve counts pending upgrades too; no unbounded set of authenticated handshakes.
func (h *Hub) reserve(p domain.Principal, check func(context.Context) (domain.Principal, error)) (*connection, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if h.total >= h.config.MaxConnections || len(h.users[p.ID]) >= h.config.MaxPerUser {
		return nil, ErrLimit
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &connection{ctx: ctx, cancel: cancel, principal: p, check: check, queue: make(chan delivery, h.config.QueueSize)}
	if h.users[p.ID] == nil {
		h.users[p.ID] = make(map[*connection]struct{})
	}
	h.users[p.ID][c] = struct{}{}
	h.total++
	h.wg.Add(1)
	return c, nil
}

func (h *Hub) remove(c *connection) {
	c.cancel()
	h.mu.Lock()
	delete(h.users[c.principal.ID], c)
	if len(h.users[c.principal.ID]) == 0 {
		delete(h.users, c.principal.ID)
	}
	h.total--
	h.mu.Unlock()
	h.wg.Done()
}

// Publish queues only explicit user candidates. Each connection independently
// rechecks its session and resource policy before writing. No browser topics exist.
func (h *Hub) Publish(ctx context.Context, event events.Event, users []string, authorize events.Authorize) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if authorize == nil || len(users) == 0 || len(users) > 1024 {
		return ErrPolicy
	}
	for _, user := range users {
		if !events.ValidID(user) {
			return ErrPolicy
		}
	}
	data, err := json.Marshal(event)
	if err != nil || len(data) > events.MaxMessageBytes {
		return events.ErrInvalidEvent
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrClosed
	}
	seen := make(map[string]bool, len(users))
	for _, user := range users {
		if seen[user] {
			continue
		}
		seen[user] = true
		for c := range h.users[user] {
			if c.ctx.Err() != nil {
				continue
			}
			select {
			case c.queue <- delivery{event, data, authorize}:
			default:
				c.stop(websocket.StatusTryAgainLater) // Reconnect/refetch; never block a business write.
			}
		}
	}
	return nil
}

// Shutdown prevents new reservations, cancels hijacked sockets, and joins every
// handler/reader/watchdog. http.Server.Shutdown alone does not close hijacked IO.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.stop.Do(func() {
		h.mu.Lock()
		h.closed = true
		for _, connections := range h.users {
			for c := range connections {
				c.stop(websocket.StatusGoingAway)
			}
		}
		h.mu.Unlock()
		go func() { h.wg.Wait(); close(h.done) }()
	})
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Compare permission fields only. A role/program/faculty transition reconnects
// rather than retaining old authorization. Name/email are never cached as policy.
func sameAccess(a, b domain.Principal) bool {
	return a.ID == b.ID && a.Student == b.Student && a.Tutor == b.Tutor &&
		a.CoordinatorFaculty == b.CoordinatorFaculty && a.ProgramID == b.ProgramID &&
		a.Verification == b.Verification && a.Suspended == b.Suspended && a.Closed == b.Closed
}

func (h *Hub) authenticate(c *connection) (domain.Principal, error) {
	ctx, cancel := context.WithTimeout(c.ctx, h.config.AuthTimeout)
	defer cancel()
	p, err := c.check(ctx)
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return domain.Principal{}, err
	}
	if !p.Active() || (!p.Student && !p.Coordinator()) || !sameAccess(c.principal, p) {
		return domain.Principal{}, domain.ErrForbidden
	}
	return p, nil
}

func (h *Hub) serve(c *connection, socket *websocket.Conn) {
	socket.SetReadLimit(events.MaxMessageBytes)
	var tasks sync.WaitGroup
	tasks.Add(2)
	defer tasks.Wait()
	defer c.cancel()
	defer func() {
		// Bound the library's close handshake, including unresponsive clients.
		timer := time.AfterFunc(250*time.Millisecond, func() { _ = socket.CloseNow() })
		defer timer.Stop()
		code := websocket.StatusCode(c.closeCode.Load())
		if code == 0 {
			code = websocket.StatusGoingAway
		}
		_ = socket.Close(code, "connection closed")
		_ = socket.CloseNow()
	}()
	// Read is required to process pong and close control frames. All application
	// messages (text, binary, subscription attempts) are unsupported and disconnect.
	go func() {
		defer tasks.Done()
		defer c.cancel()
		// The close handshake needs this reader alive after c.ctx is cancelled.
		// The bounded deferred CloseNow above always releases it before Wait.
		_, _, err := socket.Read(context.Background())
		if err == nil {
			c.stop(websocket.StatusPolicyViolation)
		}
	}()
	go func() {
		defer tasks.Done()
		ticker := time.NewTicker(h.config.AuthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				if _, err := h.authenticate(c); err != nil {
					c.stop(websocket.StatusPolicyViolation)
					return
				}
			}
		}
	}()
	ticker := time.NewTicker(h.config.PingInterval)
	defer ticker.Stop()
	// Covers revocation between the initial handshake and completed upgrade.
	if _, err := h.authenticate(c); err != nil {
		c.stop(websocket.StatusPolicyViolation)
		return
	}
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(c.ctx, h.config.PingTimeout)
			err := socket.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		case message := <-c.queue:
			p, err := h.authenticate(c)
			if err != nil {
				c.stop(websocket.StatusPolicyViolation)
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, h.config.AuthTimeout)
			allowed, err := message.authorize(ctx, p, message.event)
			if err == nil {
				err = ctx.Err()
			}
			cancel()
			if err != nil {
				return
			} // Fail closed on unavailable resource policy.
			if !allowed {
				continue
			}
			ctx, cancel = context.WithTimeout(c.ctx, h.config.WriteTimeout)
			err = socket.Write(ctx, websocket.MessageText, message.data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
