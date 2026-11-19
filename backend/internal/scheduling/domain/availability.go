package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata" // Include IANA data in the standalone API binary.
)

var (
	ErrInvalid   = errors.New("invalid availability")
	ErrForbidden = errors.New("permission denied")
	ErrNotFound  = errors.New("availability resource not found")
	ErrOverlap   = errors.New("availability overlaps an existing window")
	ErrReserved  = errors.New("an active session depends on this availability")
	ErrConflict  = errors.New("availability changed concurrently; retry the operation")
)

type Mode string

const (
	Online Mode = "ONLINE"
	Onsite Mode = "ONSITE"
	Both   Mode = "BOTH"
)

func (m Mode) Valid() bool { return m == Online || m == Onsite || m == Both }

type Semester struct {
	ID               string
	StartsAt, EndsAt time.Time
}

type Rule struct {
	ID         string `json:"id"`
	SemesterID string `json:"semester_id"`
	Weekday    int    `json:"iso_weekday"`
	Start      string `json:"local_start_time"`
	End        string `json:"local_end_time"`
	Zone       string `json:"time_zone"`
	Mode       Mode   `json:"allowed_mode"`
}

type Window struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Mode     Mode      `json:"allowed_mode"`
}

type Slot struct {
	ID      string `json:"id"`
	TutorID string `json:"tutor_id"`
	Window
	Recurring bool   `json:"is_recurring"`
	RuleID    string `json:"-"`
}

type Exception struct {
	Date   string `json:"local_date"`
	Reason string `json:"reason,omitempty"`
}

func (w Window) Valid() bool {
	return w.Mode.Valid() && w.StartsAt.Before(w.EndsAt) &&
		w.StartsAt.Nanosecond()%1000 == 0 && w.EndsAt.Nanosecond()%1000 == 0
}

func NormalizeRule(rule Rule) (Rule, error) {
	if rule.Weekday < 1 || rule.Weekday > 7 {
		return Rule{}, fmt.Errorf("%w: iso_weekday must be 1–7", ErrInvalid)
	}
	start, err := parseClock(rule.Start)
	if err != nil {
		return Rule{}, err
	}
	end, err := parseClock(rule.End)
	if err != nil {
		return Rule{}, err
	}
	if !end.After(start) {
		return Rule{}, fmt.Errorf("%w: local end must be after start on the same date", ErrInvalid)
	}
	if _, err := location(rule.Zone); err != nil {
		return Rule{}, err
	}
	if !rule.Mode.Valid() {
		return Rule{}, fmt.Errorf("%w: allowed_mode must be ONLINE, ONSITE or BOTH", ErrInvalid)
	}
	rule.Start, rule.End = start.Format(clockLayout), end.Format(clockLayout)
	return rule, nil
}

const clockLayout = "15:04:05.999999"

var clockFormat = regexp.MustCompile(`^[0-9]{2}:[0-9]{2}(:[0-9]{2}(\.[0-9]{1,6})?)?$`)

func parseClock(value string) (time.Time, error) {
	if clockFormat.MatchString(value) {
		format := clockLayout
		if len(value) == 5 {
			format = "15:04"
		}
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: use HH:MM or HH:MM:SS with up to six fractional digits", ErrInvalid)
}

func location(zone string) (*time.Location, error) {
	// LoadLocation treats empty and Local specially. Neither is an IANA zone ID.
	if zone != "" && zone != "Local" && !strings.HasPrefix(zone, "/") {
		if loc, err := time.LoadLocation(zone); err == nil {
			return loc, nil
		}
	}
	return nil, fmt.Errorf("%w: unknown IANA time zone", ErrInvalid)
}

func civilDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func isoWeekday(d time.Time) int { return (int(d.Weekday())+6)%7 + 1 }

// resolveClock finds all instants that round-trip to the requested wall time.
// Inspecting zone periods handles non-hour DST transitions and date-line changes.
func resolveClock(date, clock time.Time, loc *time.Location) (time.Time, bool) {
	wall := date.Add(time.Duration(clock.Hour())*time.Hour + time.Duration(clock.Minute())*time.Minute +
		time.Duration(clock.Second())*time.Second + time.Duration(clock.Nanosecond()))
	end := wall.Add(48 * time.Hour)
	offsets := map[int]bool{}
	for sample := wall.Add(-48 * time.Hour); !sample.After(end); {
		zoned := sample.In(loc)
		_, offset := zoned.Zone()
		offsets[offset] = true
		_, next := zoned.ZoneBounds()
		if next.IsZero() || !next.After(sample) {
			break
		}
		sample = next
	}
	var resolved time.Time
	matches := 0
	for offset := range offsets {
		instant := wall.Add(-time.Duration(offset) * time.Second)
		_, actual := instant.In(loc).Zone()
		if offset == actual {
			matches++
			resolved = instant.UTC()
		}
	}
	return resolved, matches == 1
}

// Occurrences advances civil dates, then independently resolves both endpoints.
func Occurrences(rule Rule, semester Semester, after time.Time, skipped map[string]bool) ([]Window, error) {
	return occurrences(rule, semester, after, skipped, "")
}

// OccurrenceOn uses the same boundaries and clock rules for the restored date.
func OccurrenceOn(rule Rule, semester Semester, after time.Time, date string) (Window, error) {
	windows, err := occurrences(rule, semester, after, nil, date)
	if err != nil {
		return Window{}, err
	}
	if len(windows) != 1 {
		return Window{}, fmt.Errorf("%w: date is not a future occurrence of this rule", ErrInvalid)
	}
	return windows[0], nil
}

func occurrences(rule Rule, semester Semester, after time.Time, skipped map[string]bool, onlyDate string) ([]Window, error) {
	rule, err := NormalizeRule(rule)
	if err != nil {
		return nil, err
	}
	loc, _ := location(rule.Zone)
	start, _ := parseClock(rule.Start)
	end, _ := parseClock(rule.End)
	first := civilDate(latest(semester.StartsAt, after).In(loc))
	last := civilDate(semester.EndsAt.In(loc))
	if onlyDate != "" {
		date, err := time.Parse(time.DateOnly, onlyDate)
		if err != nil || date.Before(first) || date.After(last) || isoWeekday(date) != rule.Weekday {
			return nil, nil
		}
		first, last = date, date
	} else {
		first = first.AddDate(0, 0, (rule.Weekday-isoWeekday(first)+7)%7)
	}
	var result []Window
	for date := first; !date.After(last); date = date.AddDate(0, 0, 7) {
		key := date.Format(time.DateOnly)
		if skipped[key] {
			continue
		}
		from, okStart := resolveClock(date, start, loc)
		to, okEnd := resolveClock(date, end, loc)
		// An unambiguous boundary can establish that a candidate is outside scope
		// even if its other endpoint falls in a clock transition.
		if okStart && (!from.After(after) || from.Before(semester.StartsAt) || !from.Before(semester.EndsAt)) {
			continue
		}
		if okEnd && (to.After(semester.EndsAt) || !to.After(after) || !to.After(semester.StartsAt)) {
			continue
		}
		if !okStart || !okEnd || !from.Before(to) {
			return nil, fmt.Errorf("%w: local time on %s is ambiguous or nonexistent in %s", ErrInvalid, key, rule.Zone)
		}
		result = append(result, Window{StartsAt: from, EndsAt: to, Mode: rule.Mode})
	}
	return result, nil
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Reconcile preserves slot IDs when boundaries and mode are unchanged.
func Reconcile(existing []Slot, desired []Window) (remove []string, add []Window) {
	pending := make(map[Window]bool, len(desired))
	canonical := func(w Window) Window {
		w.StartsAt, w.EndsAt = w.StartsAt.UTC(), w.EndsAt.UTC()
		return w
	}
	for _, w := range desired {
		pending[canonical(w)] = true
	}
	for _, slot := range existing {
		key := canonical(slot.Window)
		if pending[key] {
			delete(pending, key)
		} else {
			remove = append(remove, slot.ID)
		}
	}
	for _, w := range desired {
		key := canonical(w)
		if pending[key] {
			add = append(add, w)
			delete(pending, key)
		}
	}
	return remove, add
}
