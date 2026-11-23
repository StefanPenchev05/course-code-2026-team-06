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
