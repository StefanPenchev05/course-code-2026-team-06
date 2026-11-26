package application

import (
	"context"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// Counts are aggregate evidence only; no participant IDs or private data.
type CalendarActivity struct {
	Counts          map[string]int64 `json:"counts"`
	Sessions        map[string]int64 `json:"sessions_by_status"`
	SessionsOutside map[string]int64 `json:"sessions_outside_proposed_bounds_by_status"`
}

// These consumer-owned ports are implemented by transaction-bound adapters in
// Scheduling and Coordination, injected by bootstrap. Every report uses one snapshot.
type CalendarScheduling interface {
	Inspect(context.Context, domain.Semester, domain.Semester, time.Time) (CalendarActivity, error)
	RolloverTutors(context.Context, domain.Semester, domain.Semester) (map[string]int64, error)
}
type CalendarDiscipline interface {
	Inspect(context.Context, string) (map[string]int64, error)
}
type CalendarTx interface {
	List(context.Context) ([]domain.Semester, error)
	Insert(context.Context, domain.Semester) error
	Scheduling() CalendarScheduling
	Discipline() CalendarDiscipline
}
type CalendarRepository interface {
	CalendarTransaction(context.Context, bool, func(CalendarTx) error) error
}
type CalendarService struct {
	repo CalendarRepository
	now  func() time.Time
}

func NewCalendar(repo CalendarRepository, now func() time.Time) *CalendarService {
	return &CalendarService{repo: repo, now: now}
}

type CalendarReceipt struct {
	Operation string                `json:"operation"`
	ActionAt  time.Time             `json:"action_at"`
	ReadOnly  bool                  `json:"read_only"`
	Outcome   string                `json:"outcome"`
	Manifest  *domain.CalendarInput `json:"manifest,omitempty"`
	Semester  *domain.Semester      `json:"semester,omitempty"`
	Semesters []domain.Semester     `json:"semesters,omitempty"`
	Overlaps  []domain.Semester     `json:"overlaps,omitempty"`
	Impact    *CalendarImpact       `json:"impact,omitempty"`
	Rollover  *CalendarRollover     `json:"rollover,omitempty"`
	Notes     []string              `json:"notes"`
}
type CalendarImpact struct {
	Current               *domain.Semester `json:"current,omitempty"`
	Proposed              domain.Semester  `json:"proposed"`
	Expands               bool             `json:"expands"`
	Contracts             bool             `json:"contracts"`
	ModificationSupported bool             `json:"modification_supported"`
	Activity              CalendarActivity `json:"activity"`
	Discipline            map[string]int64 `json:"discipline"`
	BlockingReasons       []string         `json:"blocking_reasons"`
}
type CalendarRollover struct {
	Previous          CalendarImpact   `json:"previous"`
	Next              CalendarImpact   `json:"next"`
	Tutors            map[string]int64 `json:"tutor_publication_counts"`
	OverlappingBounds bool             `json:"overlapping_bounds"`
}

func (s *CalendarService) receipt(operation string, readOnly bool) CalendarReceipt {
	return CalendarReceipt{Operation: operation, ActionAt: s.now().UTC().Truncate(time.Microsecond), ReadOnly: readOnly,
		Notes: []string{"Snapshot results can become stale. No schedules, sessions, reservations, approvals, history or notifications are changed."}}
}
func (s *CalendarService) List(ctx context.Context) (CalendarReceipt, error) {
	r := s.receipt("list", true)
	err := s.repo.CalendarTransaction(ctx, true, func(tx CalendarTx) (err error) { r.Semesters, err = tx.List(ctx); return err })
	r.Outcome = "inspected"
	return r, err
}

// ListForTutor reuses the CLI's read-only listing without changing the CLI's
// full-calendar behavior or exposing its operator receipt through HTTP.
func (s *CalendarService) ListForTutor(ctx context.Context, actor identity.Principal) ([]domain.Semester, error) {
	if !actor.CanTutor() {
		return nil, identity.ErrForbidden
	}
	receipt, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]domain.Semester, 0, len(receipt.Semesters))
	for _, entry := range receipt.Semesters {
		if entry.EndsAt.After(receipt.ActionAt) {
			entry.StartsAt, entry.EndsAt = entry.StartsAt.UTC(), entry.EndsAt.UTC()
			rows = append(rows, entry)
		}
	}
	return rows, nil
}
func (s *CalendarService) Create(ctx context.Context, input domain.CalendarInput, dryRun, acknowledgeOverlap bool) (CalendarReceipt, error) {
	r := s.receipt("create", dryRun)
	r.Manifest = &input
	if _, err := input.Validate(); err != nil {
		return r, err
	}
	err := s.repo.CalendarTransaction(ctx, dryRun, func(tx CalendarTx) error {
		// Repeat validation inside the transaction, after the write lock if applying.
		proposed, err := input.Validate()
		if err != nil {
			return err
		}
		all, err := tx.List(ctx)
		if err != nil {
			return err
		}
		match, overlaps, err := domain.CheckCalendar(proposed, all)
		if err != nil {
			return err
		}
		r.Semester, r.Overlaps = &proposed, overlaps
		if match != nil {
			r.Semester = match
			r.Outcome = "already_exists"
			if dryRun {
				impact, err := calendarImpact(ctx, tx, *match, *match, r.ActionAt)
				r.Impact = &impact
				return err
			}
			return nil
		}
		activity, err := tx.Scheduling().Inspect(ctx, proposed, proposed, r.ActionAt)
		if err != nil {
			return err
		}
		r.Impact = &CalendarImpact{Proposed: proposed, Activity: activity, BlockingReasons: []string{}}
		if len(overlaps) > 0 {
			r.Notes = append(r.Notes, "Overlaps are not forbidden by an approved institutional rule. Review future one-off booking attribution and explicitly acknowledge before creation.")
			if !dryRun && !acknowledgeOverlap {
				return domain.ErrCalendarOverlap
			}
		}
		if dryRun {
			r.Outcome = "would_create"
			return nil
		}
		if err := tx.Insert(ctx, proposed); err != nil {
			return err
		}
		r.Outcome = "created"
		return nil
	})
	return r, err
}
func findSemester(all []domain.Semester, id string) (domain.Semester, error) {
	for _, v := range all {
		if strings.EqualFold(v.ID, id) {
			return v, nil
		}
	}
	return domain.Semester{}, domain.ErrCalendarMissing
}
func calendarImpact(ctx context.Context, tx CalendarTx, current, proposed domain.Semester, at time.Time) (CalendarImpact, error) {
	r := CalendarImpact{Current: &current, Proposed: proposed, Expands: proposed.StartsAt.Before(current.StartsAt) || proposed.EndsAt.After(current.EndsAt),
		Contracts: proposed.StartsAt.After(current.StartsAt) || proposed.EndsAt.Before(current.EndsAt), BlockingReasons: []string{}}
	if !current.SameBounds(proposed) {
		r.BlockingReasons = append(r.BlockingReasons, "Date modification is unsupported. A separately reviewed remediation/migration plan is required; preserve stored session semester attribution and obtain tutor schedule reconfirmation.")
	}
	var err error
	r.Activity, err = tx.Scheduling().Inspect(ctx, current, proposed, at)
	if err != nil {
		return r, err
	}
	r.Discipline, err = tx.Discipline().Inspect(ctx, current.ID)
	return r, err
}
func (s *CalendarService) InspectImpact(ctx context.Context, id, start, end, zone string) (CalendarReceipt, error) {
	r := s.receipt("inspect-impact", true)
	if !domain.ValidSemesterID(id) {
		return r, domain.ErrCalendarInput
	}
	a, b, err := domain.ParseSemesterBounds(start, end, zone)
	if err != nil {
		return r, err
	}
	err = s.repo.CalendarTransaction(ctx, true, func(tx CalendarTx) error {
		all, err := tx.List(ctx)
		if err != nil {
			return err
		}
		current, err := findSemester(all, id)
		if err != nil {
			return err
		}
		proposed := current
		proposed.StartsAt, proposed.EndsAt = a, b
		impact, err := calendarImpact(ctx, tx, current, proposed, r.ActionAt)
		r.Impact = &impact
		for _, other := range all {
			if other.ID != current.ID && proposed.Overlaps(other) {
				r.Overlaps = append(r.Overlaps, other)
			}
		}
		return err
	})
	r.Outcome = "inspected"
	return r, err
}
func (s *CalendarService) Rollover(ctx context.Context, from, to string) (CalendarReceipt, error) {
	r := s.receipt("rollover-check", true)
	if !domain.ValidSemesterID(from) || !domain.ValidSemesterID(to) || strings.EqualFold(from, to) {
		return r, domain.ErrCalendarInput
	}
	err := s.repo.CalendarTransaction(ctx, true, func(tx CalendarTx) error {
		all, err := tx.List(ctx)
		if err != nil {
			return err
		}
		old, err := findSemester(all, from)
		if err != nil {
			return err
		}
		next, err := findSemester(all, to)
		if err != nil {
			return err
		}
		if !next.StartsAt.After(old.StartsAt) {
			return domain.ErrCalendarInput
		}
		rr := &CalendarRollover{OverlappingBounds: old.Overlaps(next)}
		r.Rollover = rr
		rr.Previous, err = calendarImpact(ctx, tx, old, old, r.ActionAt)
		if err != nil {
			return err
		}
		rr.Next, err = calendarImpact(ctx, tx, next, next, r.ActionAt)
		if err != nil {
			return err
		}
		rr.Tutors, err = tx.Scheduling().RolloverTutors(ctx, old, next)
		return err
	})
	r.Outcome = "review_required"
	r.Notes = append(r.Notes, "Publication counts are not evidence of human reconfirmation. Manually contact prior-semester tutors through approved channels, review eligibility, and ask them to publish/reconfirm next-semester availability. No rules are copied and no reminders are sent.")
	return r, err
}
