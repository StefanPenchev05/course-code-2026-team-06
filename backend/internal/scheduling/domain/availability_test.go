package domain

import (
	"errors"
	"testing"
	"time"
)

func instant(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWeeklyBoundariesAndExceptions(t *testing.T) {
	term := Semester{StartsAt: instant(t, "2026-12-07T14:00:00Z"), EndsAt: instant(t, "2026-12-28T16:00:00Z")}
	rule := Rule{Weekday: 1, Start: "14:00", End: "17:00", Zone: "Europe/Luxembourg", Mode: Both}
	now := instant(t, "2026-12-01T00:00:00Z")
	got, err := Occurrences(rule, term, now, map[string]bool{"2026-12-21": true})
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	for i, start := range []string{"2026-12-14T13:00:00Z", "2026-12-28T13:00:00Z"} {
		if !got[i].StartsAt.Equal(instant(t, start)) || got[i].Mode != Both {
			t.Fatal(got)
		}
	}
	if !got[1].EndsAt.Equal(term.EndsAt) {
		t.Fatal("an occurrence ending at the semester boundary should fit")
	}
	term.EndsAt = term.EndsAt.Add(-time.Second)
	got, err = Occurrences(rule, term, now, nil)
	if err != nil || len(got) != 2 || got[1].StartsAt.Day() != 21 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := OccurrenceOn(rule, term, now, "2026-12-15"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestDSTUsesWallTime(t *testing.T) {
	now := instant(t, "2026-03-20T00:00:00Z")
	term := Semester{StartsAt: now, EndsAt: instant(t, "2026-04-01T00:00:00Z")}
	rule := Rule{Weekday: 7, Start: "14:00", End: "17:00", Zone: "Europe/Luxembourg", Mode: Online}
	got, err := Occurrences(rule, term, now, nil)
	if err != nil || len(got) != 2 || got[1].StartsAt.Sub(got[0].StartsAt) != 167*time.Hour ||
		got[0].StartsAt.Hour() != 13 || got[1].StartsAt.Hour() != 12 {
		t.Fatalf("%+v %v", got, err)
	}
	rule.Start = "02:30"
	if _, err := Occurrences(rule, term, now, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("spring gap was accepted", err)
	}
	got, err = Occurrences(rule, term, now, map[string]bool{"2026-03-29": true})
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	now = instant(t, "2026-10-23T00:00:00Z")
	term = Semester{StartsAt: now, EndsAt: instant(t, "2026-10-27T00:00:00Z")}
	if _, err := Occurrences(rule, term, now, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("autumn fold was accepted", err)
	}
	rule.Start = "03:00"
	if _, err := OccurrenceOn(rule, term, now, "2026-10-25"); err != nil {
		t.Fatal(err)
	}
}

func TestRuleValidation(t *testing.T) {
	valid := Rule{Weekday: 1, Start: "14:00", End: "17:00", Zone: "Europe/Luxembourg", Mode: Both}
	for name, change := range map[string]func(*Rule){
		"weekday":       func(r *Rule) { r.Weekday = 0 },
		"overnight":     func(r *Rule) { r.End = "02:00" },
		"clock":         func(r *Rule) { r.End = "24:00" },
		"zero duration": func(r *Rule) { r.End = r.Start },
		"zone":          func(r *Rule) { r.Zone = "Europe/Unknown" },
		"host zone":     func(r *Rule) { r.Zone = "Local" },
		"mode":          func(r *Rule) { r.Mode = "UNSUPPORTED" },
	} {
		r := valid
		change(&r)
		if _, err := NormalizeRule(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	valid.Start = "14:00:00.0100"
	if r, err := NormalizeRule(valid); err != nil || r.Start != "14:00:00.01" {
		t.Fatal(r, err)
	}
}

func TestReconcile(t *testing.T) {
	start := instant(t, "2026-12-07T13:00:00Z")
	a := Window{StartsAt: start, EndsAt: start.Add(time.Hour), Mode: Online}
	b := Window{StartsAt: a.EndsAt, EndsAt: a.EndsAt.Add(time.Hour), Mode: Onsite}
	existing := []Slot{{ID: "same", Window: a}, {ID: "changed", Window: b}}
	remove, add := Reconcile(existing, []Window{a, b})
	if len(remove) != 0 || len(add) != 0 {
		t.Fatalf("repeat generation must be idempotent: %v, %v", remove, add)
	}
	b.Mode = Both
	remove, add = Reconcile(existing, []Window{a, b})
	if len(remove) != 1 || remove[0] != "changed" || len(add) != 1 || add[0] != b {
		t.Fatalf("%v, %v", remove, add)
	}
	c := b
	c.EndsAt = c.StartsAt
	if c.Valid() || !b.Valid() {
		t.Fatal("one-off range validation failed")
	}
}
