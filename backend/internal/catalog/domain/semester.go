package domain

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

var (
	ErrCalendarInput    = errors.New("invalid calendar input")
	ErrCalendarConflict = errors.New("calendar identity conflicts with an existing entry; inspect the calendar and reuse its ID")
	ErrCalendarOverlap  = errors.New("overlapping calendar ranges require explicit acknowledgement after review")
	ErrCalendarMissing  = errors.New("semester not found")
)

type Semester struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// CalendarInput is an operator-reviewed change record, not an automatic import.
// Keep this manifest and the execution receipt in the approved operations record.
type CalendarInput struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StartsAt       string `json:"starts_at"`
	EndsAt         string `json:"ends_at"`
	TimeZone       string `json:"time_zone"`
	Source         string `json:"source"`
	SourceRevision string `json:"source_revision,omitempty"`
	Operator       string `json:"operator"`
	Approval       string `json:"approval"`
}

var calendarUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var calendarTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func ValidSemesterID(id string) bool {
	return calendarUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func CalendarName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func calendarText(s string, max int, required bool) bool {
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > max || (required && strings.TrimSpace(s) == "") {
		return false
	}
	for _, c := range s {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// ParseSemesterBounds accepts exact instants only. Offsets must agree with the
// supplied IANA zone, including at DST transitions; no local-time guessing occurs.
func ParseSemesterBounds(start, end, zone string) (time.Time, time.Time, error) {
	invalid := func() (time.Time, time.Time, error) { return time.Time{}, time.Time{}, ErrCalendarInput }
	if zone == "" || zone == "Local" || strings.HasPrefix(zone, "/") || strings.Contains(zone, "\\") {
		return invalid()
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return invalid()
	}
	parse := func(s string) (time.Time, error) {
		if !calendarTimestamp.MatchString(s) {
			return time.Time{}, ErrCalendarInput
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil || t.Year() < 1 || t.Year() > 9999 || t.Nanosecond()%1000 != 0 {
			return time.Time{}, ErrCalendarInput
		}
		_, offset := t.Zone()
		_, expected := t.In(loc).Zone()
		if offset != expected {
			return time.Time{}, ErrCalendarInput
		}
		if t.UTC().Year() < 1 || t.UTC().Year() > 9999 {
			return time.Time{}, ErrCalendarInput
		}
		return t.UTC(), nil
	}
	a, err := parse(start)
	if err != nil {
		return invalid()
	}
	b, err := parse(end)
	if err != nil || !b.After(a) {
		return invalid()
	}
	return a, b, nil
}

func (i CalendarInput) Validate() (Semester, error) {
	if !ValidSemesterID(i.ID) || !calendarText(i.Name, 200, true) ||
		!calendarText(i.Operator, 200, true) || !calendarText(i.Approval, 500, true) ||
		!calendarText(i.Source, 2000, true) || !calendarText(i.SourceRevision, 500, false) {
		return Semester{}, ErrCalendarInput
	}
	u, err := url.Parse(i.Source)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return Semester{}, ErrCalendarInput
	}
	a, b, err := ParseSemesterBounds(i.StartsAt, i.EndsAt, i.TimeZone)
	if err != nil {
		return Semester{}, err
	}
	return Semester{ID: strings.ToLower(i.ID), Name: strings.Join(strings.Fields(i.Name), " "), StartsAt: a, EndsAt: b}, nil
}

func (s Semester) SameBounds(other Semester) bool {
	return s.StartsAt.Equal(other.StartsAt) && s.EndsAt.Equal(other.EndsAt)
}
func (s Semester) Overlaps(other Semester) bool {
	return s.StartsAt.Before(other.EndsAt) && other.StartsAt.Before(s.EndsAt)
}

// CheckCalendar never repairs legacy duplicates. Equivalent names are a tool
// guard against operator mistakes, not a new database-wide naming constraint.
func CheckCalendar(proposed Semester, existing []Semester) (*Semester, []Semester, error) {
	var match *Semester
	overlaps := make([]Semester, 0)
	for _, s := range existing {
		sameID := strings.EqualFold(s.ID, proposed.ID)
		sameName := CalendarName(s.Name) == CalendarName(proposed.Name)
		sameBounds := s.SameBounds(proposed)
		if sameID || sameName || sameBounds {
			if !sameName || !sameBounds || match != nil {
				return nil, nil, ErrCalendarConflict
			}
			copy := s
			match = &copy
			continue
		}
		if s.Overlaps(proposed) {
			overlaps = append(overlaps, s)
		}
	}
	return match, overlaps, nil
}
