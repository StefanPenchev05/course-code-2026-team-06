package application

import (
	"context"
	"fmt"
	"regexp"
	"time"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type Page struct{ Limit, Offset int }
type SlotFilter struct {
	From, To time.Time
	Page
}

type Transaction interface {
	Semester(context.Context, string) (domain.Semester, error)
	RequireSemesterFor(context.Context, domain.Window) error
	CreateRule(context.Context, string, domain.Rule) (string, error)
	Rule(context.Context, string, string) (domain.Rule, error)
	UpdateRule(context.Context, domain.Rule) error
	DeleteRule(context.Context, string) error
	ExceptionDates(context.Context, string) (map[string]bool, error)
	AddException(context.Context, string, string) error
	RemoveException(context.Context, string, string) error
	FutureRuleSlots(context.Context, string, time.Time) ([]domain.Slot, error)
	Slot(context.Context, string, string) (domain.Slot, error)
	CreateSlot(context.Context, string, domain.Window) (domain.Slot, error)
	CreateRuleSlots(context.Context, string, string, []domain.Window) error
	DeleteSlots(context.Context, []string) error
}

type Repository interface {
	InTransaction(ctx context.Context, tutor string, work func(Transaction, identity.Principal) error) error
	ListRules(context.Context, string, Page) ([]domain.Rule, error)
	ListExceptions(context.Context, string, string, Page) ([]domain.Exception, error)
	ListSlots(ctx context.Context, tutor string, freeOnly bool, filter SlotFilter) ([]domain.Slot, error)
}

// TutorAccess follows Tutoring's active profile and discovery rules.
type TutorAccess interface {
	CanManageAvailability(identity.Principal) bool
	CanViewAvailability(ctx context.Context, viewer identity.Principal, tutorID string) (bool, error)
}

type Service struct {
	repo   Repository
	tutors TutorAccess
	now    func() time.Time
}

func New(repo Repository, tutors TutorAccess, now func() time.Time) *Service {
	return &Service{repo: repo, tutors: tutors, now: now}
}

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validPage(p Page) bool {
	return p.Limit >= 1 && p.Limit <= 100 && p.Offset >= 0 && p.Offset <= 100000
}

func (s *Service) write(ctx context.Context, actor identity.Principal, work func(Transaction, time.Time) error) error {
	if !s.tutors.CanManageAvailability(actor) {
		return domain.ErrForbidden
	}
	return s.repo.InTransaction(ctx, actor.ID, func(tx Transaction, latest identity.Principal) error {
		if !s.tutors.CanManageAvailability(latest) {
			return domain.ErrForbidden
		}
		return work(tx, s.now().UTC().Truncate(time.Microsecond))
	})
}

func (s *Service) CreateRule(ctx context.Context, actor identity.Principal, input domain.Rule) (domain.Rule, error) {
	if !uuid.MatchString(input.SemesterID) {
		return domain.Rule{}, domain.ErrInvalid
	}
	rule, err := domain.NormalizeRule(input)
	if err != nil {
		return domain.Rule{}, err
	}
	err = s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		semester, err := activeSemester(ctx, tx, rule.SemesterID, now)
		if err != nil {
			return err
		}
		windows, err := domain.Occurrences(rule, semester, now, nil)
		if err != nil {
			return err
		}
		rule.ID, err = tx.CreateRule(ctx, actor.ID, rule)
		if err != nil {
			return err
		}
		return tx.CreateRuleSlots(ctx, actor.ID, rule.ID, windows)
	})
	if err != nil {
		return domain.Rule{}, err
	}
	return rule, nil
}

func activeSemester(ctx context.Context, tx Transaction, id string, now time.Time) (domain.Semester, error) {
	semester, err := tx.Semester(ctx, id)
	if err != nil {
		return domain.Semester{}, err
	}
	if !semester.EndsAt.After(now) {
		return domain.Semester{}, fmt.Errorf("%w: semester has ended", domain.ErrInvalid)
	}
	return semester, nil
}

type RulePatch struct {
	Weekday *int         `json:"iso_weekday"`
	Start   *string      `json:"local_start_time"`
	End     *string      `json:"local_end_time"`
	Zone    *string      `json:"time_zone"`
	Mode    *domain.Mode `json:"allowed_mode"`
}

func (p RulePatch) apply(rule domain.Rule) domain.Rule {
	if p.Weekday != nil {
		rule.Weekday = *p.Weekday
	}
	if p.Start != nil {
		rule.Start = *p.Start
	}
	if p.End != nil {
		rule.End = *p.End
	}
	if p.Zone != nil {
		rule.Zone = *p.Zone
	}
	if p.Mode != nil {
		rule.Mode = *p.Mode
	}
	return rule
}

func (s *Service) UpdateRule(ctx context.Context, actor identity.Principal, id string, patch RulePatch) (domain.Rule, error) {
	if !uuid.MatchString(id) || patch == (RulePatch{}) {
		return domain.Rule{}, domain.ErrInvalid
	}
	var result domain.Rule
	err := s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		rule, err := tx.Rule(ctx, actor.ID, id)
		if err != nil {
			return err
		}
		rule, err = domain.NormalizeRule(patch.apply(rule))
		if err != nil {
			return err
		}
		semester, err := activeSemester(ctx, tx, rule.SemesterID, now)
		if err != nil {
			return err
		}
		skipped, err := tx.ExceptionDates(ctx, id)
		if err != nil {
			return err
		}
		desired, err := domain.Occurrences(rule, semester, now, skipped)
		if err != nil {
			return err
		}
		existing, err := tx.FutureRuleSlots(ctx, id, now)
		if err != nil {
			return err
		}
		remove, add := domain.Reconcile(existing, desired)
		if err := tx.DeleteSlots(ctx, remove); err != nil {
			return err
		}
		if err := tx.UpdateRule(ctx, rule); err != nil {
			return err
		}
		if err := tx.CreateRuleSlots(ctx, actor.ID, id, add); err != nil {
			return err
		}
		result = rule
		return nil
	})
	if err != nil {
		return domain.Rule{}, err
	}
	return result, nil
}

func (s *Service) DeleteRule(ctx context.Context, actor identity.Principal, id string) error {
	if !uuid.MatchString(id) {
		return domain.ErrInvalid
	}
	return s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		if _, err := tx.Rule(ctx, actor.ID, id); err != nil {
			return err
		}
		slots, err := tx.FutureRuleSlots(ctx, id, now)
		if err != nil {
			return err
		}
		ids := make([]string, len(slots))
		for i, slot := range slots {
			ids[i] = slot.ID
		}
		if err := tx.DeleteSlots(ctx, ids); err != nil {
			return err
		}
		return tx.DeleteRule(ctx, id)
	})
}

func (s *Service) CreateSlot(ctx context.Context, actor identity.Principal, w domain.Window) (domain.Slot, error) {
	if !w.Valid() {
		return domain.Slot{}, fmt.Errorf("%w: provide a positive time range and a valid mode with at most microsecond precision", domain.ErrInvalid)
	}
	var slot domain.Slot
	err := s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		if !w.StartsAt.After(now) {
			return fmt.Errorf("%w: window must start in the future", domain.ErrInvalid)
		}
		if err := tx.RequireSemesterFor(ctx, w); err != nil {
			return err
		}
		var err error
		slot, err = tx.CreateSlot(ctx, actor.ID, w)
		return err
	})
	if err != nil {
		return domain.Slot{}, err
	}
	return slot, nil
}

func (s *Service) DeleteSlot(ctx context.Context, actor identity.Principal, id string) error {
	if !uuid.MatchString(id) {
		return domain.ErrInvalid
	}
	return s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		slot, err := tx.Slot(ctx, actor.ID, id)
		if err != nil {
			return err
		}
		if !slot.StartsAt.After(now) {
			return fmt.Errorf("%w: only future slots can be removed", domain.ErrInvalid)
		}
		if err := tx.DeleteSlots(ctx, []string{id}); err != nil {
			return err
		}
		if slot.RuleID == "" {
			return nil
		}
		rule, err := tx.Rule(ctx, actor.ID, slot.RuleID)
		if err != nil {
			return err
		}
		loc, err := time.LoadLocation(rule.Zone)
		if err != nil {
			return fmt.Errorf("%w: stored rule has an invalid time zone", domain.ErrInvalid)
		}
		return tx.AddException(ctx, rule.ID, slot.StartsAt.In(loc).Format(time.DateOnly))
	})
}

func (s *Service) RestoreException(ctx context.Context, actor identity.Principal, id, date string) error {
	d, err := time.Parse(time.DateOnly, date)
	if !uuid.MatchString(id) || err != nil || d.Format(time.DateOnly) != date {
		return domain.ErrInvalid
	}
	return s.write(ctx, actor, func(tx Transaction, now time.Time) error {
		rule, err := tx.Rule(ctx, actor.ID, id)
		if err != nil {
			return err
		}
		semester, err := activeSemester(ctx, tx, rule.SemesterID, now)
		if err != nil {
			return err
		}
		if err := tx.RemoveException(ctx, id, date); err != nil {
			return err
		}
		window, err := domain.OccurrenceOn(rule, semester, now, date)
		if err != nil {
			return err
		}
		return tx.CreateRuleSlots(ctx, actor.ID, id, []domain.Window{window})
	})
}

func (s *Service) Rules(ctx context.Context, actor identity.Principal, p Page) ([]domain.Rule, error) {
	if !s.tutors.CanManageAvailability(actor) {
		return nil, domain.ErrForbidden
	}
	if !validPage(p) {
		return nil, domain.ErrInvalid
	}
	p.Limit++
	return s.repo.ListRules(ctx, actor.ID, p)
}

func (s *Service) Exceptions(ctx context.Context, actor identity.Principal, id string, p Page) ([]domain.Exception, error) {
	if !s.tutors.CanManageAvailability(actor) {
		return nil, domain.ErrForbidden
	}
	if !uuid.MatchString(id) || !validPage(p) {
		return nil, domain.ErrInvalid
	}
	p.Limit++
	return s.repo.ListExceptions(ctx, actor.ID, id, p)
}

func (s *Service) Slots(ctx context.Context, actor identity.Principal, tutor string, own bool, f SlotFilter) ([]domain.Slot, error) {
	if own {
		if !s.tutors.CanManageAvailability(actor) {
			return nil, domain.ErrForbidden
		}
		tutor = actor.ID
	} else {
		if !actor.VerifiedStudent() {
			return nil, domain.ErrForbidden
		}
		if !uuid.MatchString(tutor) {
			return nil, domain.ErrInvalid
		}
		visible, err := s.tutors.CanViewAvailability(ctx, actor, tutor)
		if err != nil {
			return nil, err
		}
		if !visible {
			return nil, domain.ErrNotFound
		}
	}
	if !validPage(f.Page) || (!f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To)) {
		return nil, domain.ErrInvalid
	}
	now := s.now().UTC().Truncate(time.Microsecond)
	if f.From.Before(now) {
		f.From = now
	}
	if !f.To.IsZero() && !f.To.After(f.From) {
		return []domain.Slot{}, nil
	}
	f.Limit++
	return s.repo.ListSlots(ctx, tutor, !own, f)
}
