package application

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func TestContractFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/protocol.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Error string
		Message     json.RawMessage
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			_, err := Decode(tc.Message)
			want := map[string]error{"invalid": ErrInvalidEvent, "version": ErrUnknownVersion, "type": ErrUnknownType}[tc.Error]
			if !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}

func TestDecodeTruncatedJSON(t *testing.T) {
	wire := []byte(`{"version":1,"id":"00000000-0000-0000-0000-000000000851","type":"account.access_changed","occurredAt":"2026-11-23T16:00:00Z"}`)
	if _, err := Decode(wire); err != nil {
		t.Fatalf("complete message rejected: %v", err)
	}
	// Every incomplete prefix covers cuts inside keys, scalar values and the
	// closing delimiter, including an entirely empty message.
	for end := 0; end < len(wire); end++ {
		if _, err := Decode(wire[:end]); !errors.Is(err, ErrInvalidEvent) {
			t.Fatalf("prefix ending at byte %d: got %v, want %v", end, err, ErrInvalidEvent)
		}
	}
}

func TestDecodeTrailingJSON(t *testing.T) {
	wire := `{"version":1,"id":"00000000-0000-0000-0000-000000000851","type":"account.access_changed","occurredAt":"2026-11-23T16:00:00Z"}`
	want, err := Decode([]byte(wire))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Decode([]byte(wire + " \t\r\n")); err != nil || got != want {
		t.Fatalf("trailing whitespace changed the message: %v, %v", got, err)
	}
	for _, tc := range []struct{ name, suffix string }{
		{"object", `{}`},
		{"array", `[]`},
		{"string", `"extra"`},
		{"number", `1`},
		{"boolean", `true`},
		{"null", `null`},
		{"repeated_event", wire},
		{"malformed", `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode([]byte(wire + "\n" + tc.suffix)); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("got %v, want %v", err, ErrInvalidEvent)
			}
		})
	}
}

func TestGeneratedEventsAndDuplicateDelivery(t *testing.T) {
	now := time.Date(2026, 11, 23, 17, 0, 0, 0, time.FixedZone("Luxembourg", 3600))
	seen := map[string]bool{}
	for _, kind := range []Type{BookingChanged, AvailabilityChanged, TutorApplicationChanged, CoordinationChanged, ActivityChanged, AccountAccessChanged} {
		id := "00000000-0000-0000-0000-000000000314"
		if kind == AccountAccessChanged {
			id = ""
		}
		e, err := NewEvent(kind, id, now)
		if err != nil || !ValidID(e.ID) || seen[e.ID] || e.OccurredAt != "2026-11-23T16:00:00Z" {
			t.Fatalf("invalid generated event: %v", err)
		}
		seen[e.ID] = true
		wire, _ := json.Marshal(e)
		first, err := Decode(wire)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Decode(wire)
		if err != nil || first != second {
			t.Fatal("duplicate delivery must preserve identity")
		}
		// The transport accepts duplicates. This models the mandatory client ID check.
		dedupe := map[string]bool{}
		count := 0
		for _, event := range []Event{first, second} {
			if !dedupe[event.ID] {
				count++
				dedupe[event.ID] = true
			}
		}
		if count != 1 {
			t.Fatal("event ID cannot deduplicate repeated delivery")
		}
	}
	if _, err := NewEvent(BookingChanged, "", now); err == nil {
		t.Fatal("missing scoped ID")
	}
	if _, err := Decode(make([]byte, MaxMessageBytes+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
}
