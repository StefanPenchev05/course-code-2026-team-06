// Package application defines the transport-independent realtime invalidation contract.
package application

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

const MaxMessageBytes = 1024

type Type string

const (
	BookingChanged          Type = "booking.changed"
	AvailabilityChanged     Type = "availability.changed"
	TutorApplicationChanged Type = "tutor_application.changed"
	CoordinationChanged     Type = "coordination.changed"
	ActivityChanged         Type = "activity.changed"
	AccountAccessChanged    Type = "account.access_changed"
)

var (
	ErrInvalidEvent   = errors.New("invalid realtime event")
	ErrUnknownType    = errors.New("unknown realtime event type")
	ErrUnknownVersion = errors.New("unsupported realtime version")
	uuidPattern       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func ValidID(id string) bool {
	return uuidPattern.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

// Event has no extensible payload: recipients refetch the authorized HTTP view.
// EntityID means session, tutor, application, case/flag, tutor, or absent respectively.
type Event struct {
	Version    int    `json:"version"`
	ID         string `json:"id"`
	Type       Type   `json:"type"`
	EntityID   string `json:"entityId,omitempty"`
	OccurredAt string `json:"occurredAt"`
}

func NewEvent(kind Type, entityID string, occurred time.Time) (Event, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Event{}, err
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	e := Event{Version: 1, ID: fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		Type: kind, EntityID: entityID, OccurredAt: occurred.UTC().Format(time.RFC3339Nano)}
	return e, e.Validate()
}

func (e Event) Validate() error {
	if e.Version != 1 {
		return ErrUnknownVersion
	}
	if !ValidID(e.ID) {
		return ErrInvalidEvent
	}
	t, err := time.Parse(time.RFC3339Nano, e.OccurredAt)
	if err != nil || t.IsZero() || t.UTC().Format(time.RFC3339Nano) != e.OccurredAt {
		return ErrInvalidEvent
	}
	switch e.Type {
	case BookingChanged, AvailabilityChanged, TutorApplicationChanged, CoordinationChanged, ActivityChanged:
		if !ValidID(e.EntityID) {
			return ErrInvalidEvent
		}
	case AccountAccessChanged:
		if e.EntityID != "" {
			return ErrInvalidEvent
		}
	default:
		return ErrUnknownType
	}
	return nil
}

// Decode is strict for server fixtures/publishers. Future browser clients must
// handle unknown versions/types without crashing, as documented in realtime.md.
func Decode(data []byte) (Event, error) {
	if len(data) > MaxMessageBytes {
		return Event{}, ErrInvalidEvent
	}
	// encoding/json otherwise silently accepts duplicate object keys.
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return Event{}, ErrInvalidEvent
	}
	raw = make(map[string]json.RawMessage)
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return Event{}, ErrInvalidEvent
		}
		name, ok := key.(string)
		if !ok {
			return Event{}, ErrInvalidEvent
		}
		if _, exists := raw[name]; exists {
			return Event{}, ErrInvalidEvent
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil || bytes.Equal(value, []byte("null")) {
			return Event{}, ErrInvalidEvent
		}
		raw[name] = value
	}
	if _, err := dec.Token(); err != nil {
		return Event{}, ErrInvalidEvent
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Event{}, ErrInvalidEvent
	}
	var e Event
	strict := json.NewDecoder(bytes.NewReader(data))
	strict.DisallowUnknownFields()
	if err := strict.Decode(&e); err != nil {
		return Event{}, ErrInvalidEvent
	}
	// Case-sensitive field names; json.Unmarshal itself matches case-insensitively.
	for name := range raw {
		switch name {
		case "version", "id", "type", "entityId", "occurredAt":
		default:
			return Event{}, ErrInvalidEvent
		}
	}
	return e, e.Validate()
}

// Authorize must check this event's resource against the freshly authenticated
// principal. It must be concurrency-safe and must not trust browser scope claims.
type Authorize func(context.Context, domain.Principal, Event) (bool, error)

// Publisher is best-effort live transport, not a transaction/outbox. Only trusted
// server code supplies explicit candidate recipients AND a resource policy.
type Publisher interface {
	Publish(context.Context, Event, []string, Authorize) error
}
