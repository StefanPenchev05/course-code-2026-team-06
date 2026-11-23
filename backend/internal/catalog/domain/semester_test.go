package domain

import (
	"errors"
	"testing"
	"time"
)

func validCalendar() CalendarInput {
	return CalendarInput{ID: "10000000-0000-0000-0000-000000000001", Name: " Test semester ", StartsAt: "2060-01-01T00:00:00+01:00", EndsAt: "2060-07-01T00:00:00+02:00", TimeZone: "Europe/Luxembourg", Source: "https://www.uni.lu/en/about/academic-calendar/", SourceRevision: "synthetic test only", Operator: "test operator", Approval: "test approval"}
}

func TestCalendarValidation(t *testing.T) {
	valid := validCalendar()
	s, err := valid.Validate()
	if err != nil || s.Name != "Test semester" || s.StartsAt.Location() != time.UTC {
		t.Fatalf("%+v %v", s, err)
	}
	tests := map[string]func(*CalendarInput){
		"blank name": func(i *CalendarInput) { i.Name = "  " }, "invalid ID": func(i *CalendarInput) { i.ID = "bad" },
		"zero ID": func(i *CalendarInput) { i.ID = "00000000-0000-0000-0000-000000000000" },
		"equal":   func(i *CalendarInput) { i.EndsAt = i.StartsAt }, "reversed": func(i *CalendarInput) { i.StartsAt, i.EndsAt = i.EndsAt, i.StartsAt },
		"missing": func(i *CalendarInput) { i.StartsAt = "" }, "infinite": func(i *CalendarInput) { i.EndsAt = "infinity" },
		"date only": func(i *CalendarInput) { i.StartsAt = "2060-01-01" }, "missing offset": func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00" },
		"unknown zone": func(i *CalendarInput) { i.TimeZone = "Europe/Imaginary" }, "local zone": func(i *CalendarInput) { i.TimeZone = "Local" },
		"wrong offset":              func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00+02:00" },
		"normalized invalid offset": func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00+00:60" },
		"submicrosecond":            func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00.0000001+01:00" },
		"truncated nanoseconds":     func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00.0000000001+01:00" },
		"comma fraction":            func(i *CalendarInput) { i.StartsAt = "2060-01-01T00:00:00,001+01:00" },
		"zero year":                 func(i *CalendarInput) { i.StartsAt = "0000-01-01T00:00:00+01:00" },
		"no approval":               func(i *CalendarInput) { i.Approval = " " }, "no operator": func(i *CalendarInput) { i.Operator = "" },
		"credentials in source": func(i *CalendarInput) { i.Source = "https://name:secret@uni.lu/calendar" },
		"source query":          func(i *CalendarInput) { i.Source += "?token=secret" }, "plain source": func(i *CalendarInput) { i.Source = "http://uni.lu" },
		"control character": func(i *CalendarInput) { i.Name = "bad\nname" }, "long name": func(i *CalendarInput) {
			for j := 0; j < 201; j++ {
				i.Name += "a"
			}
		},
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			i := valid
			change(&i)
			if _, err := i.Validate(); !errors.Is(err, ErrCalendarInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCalendarDSTExplicitInstants(t *testing.T) {
	for _, tc := range []struct {
		start, end string
		valid      bool
	}{
		{"2026-03-29T02:30:00+01:00", "2026-03-30T00:00:00+02:00", false},
		{"2026-03-29T02:30:00+02:00", "2026-03-30T00:00:00+02:00", false},
		{"2026-03-29T01:00:00+01:00", "2026-03-29T03:00:00+02:00", true},
		{"2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00", true},
		{"2026-10-25T02:30:00", "2026-10-26T00:00:00+01:00", false},
	} {
		_, _, err := ParseSemesterBounds(tc.start, tc.end, "Europe/Luxembourg")
		if (err == nil) != tc.valid {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}

func TestCalendarIdentityAndHalfOpenOverlap(t *testing.T) {
	s, _ := validCalendar().Validate()
	alias := s
	alias.ID = "10000000-0000-0000-0000-000000000002"
	alias.Name = "test   SEMESTER"
	match, overlaps, err := CheckCalendar(alias, []Semester{s})
	if err != nil || match == nil || match.ID != s.ID || len(overlaps) != 0 {
		t.Fatalf("retry %v %v", match, err)
	}
	for _, change := range []func(*Semester){func(x *Semester) { x.EndsAt = x.EndsAt.Add(time.Hour) }, func(x *Semester) { x.Name = "another name" }} {
		p := s
		change(&p)
		if _, _, err := CheckCalendar(p, []Semester{s}); !errors.Is(err, ErrCalendarConflict) {
			t.Fatalf("conflict %v", err)
		}
	}
	if _, _, err := CheckCalendar(s, []Semester{s, alias}); !errors.Is(err, ErrCalendarConflict) {
		t.Fatalf("legacy duplicates %v", err)
	}
	adjacent := Semester{ID: alias.ID, Name: "Next", StartsAt: s.EndsAt, EndsAt: s.EndsAt.Add(time.Hour)}
	if s.Overlaps(adjacent) {
		t.Fatal("adjacent intervals overlap")
	}
	adjacent.StartsAt = adjacent.StartsAt.Add(-time.Microsecond)
	_, overlaps, err = CheckCalendar(adjacent, []Semester{s})
	if err != nil || len(overlaps) != 1 {
		t.Fatalf("overlap should be reported, not prohibited: %v", err)
	}
}
