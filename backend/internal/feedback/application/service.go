package application

import (
	"context"
	"regexp"
	"strings"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/feedback/domain"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	scheduling "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

const MaxPageSize = 100
const MaxOffset = 100000

type Page struct{ Limit, Offset int }

func (p Page) valid() bool {
	return p.Limit > 0 && p.Limit <= MaxPageSize && p.Offset >= 0 && p.Offset <= MaxOffset
}

type WriteTx interface {
	Student(context.Context, string) (identity.Principal, error)
	LockStudentSession(ctx context.Context, session, actor string) (scheduling.Session, error)
	Insert(context.Context, scheduling.Session, domain.Input) (domain.Entry, error)
}

type Repository interface {
	Transact(context.Context, func(WriteTx) error) error
	Get(ctx context.Context, session, actor string) (domain.Entry, error)
	List(ctx context.Context, tutor string, p Page) ([]domain.Entry, error)
	CoordinatorFeedback(ctx context.Context, actor, tutor string, p Page) (Review, error)
	Statistics(ctx context.Context, tutors []string) (map[string]domain.Statistics, error)
}

// TutorRatings is the batched, comment-free read port for public tutor views.
type TutorRatings interface {
	RatingSummaries(ctx context.Context, tutors []string) (map[string]domain.TutorRatingSummary, error)
}

type UpcomingSessions interface {
	UpcomingTutorSessions(ctx context.Context, actor identity.Principal, limit, offset int) ([]scheduling.Session, error)
}

type Review struct {
	domain.TutorRatingSummary
	Entries []domain.Entry
}

type Activity struct {
	domain.Statistics
	Upcoming []scheduling.Session
}

type Service struct {
	repo     Repository
	sessions UpcomingSessions
}

func New(repo Repository, sessions UpcomingSessions) *Service {
	return &Service{repo: repo, sessions: sessions}
}

var uuid = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *Service) Submit(ctx context.Context, actor identity.Principal, id string, input domain.Input) (domain.Entry, error) {
	if !actor.VerifiedStudent() {
		return domain.Entry{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Entry{}, domain.ErrQuery
	}
	id = strings.ToLower(id)
	in, err := input.Normalize()
	if err != nil {
		return domain.Entry{}, err
	}
	var result domain.Entry
	err = s.repo.Transact(ctx, func(tx WriteTx) error {
		current, err := tx.Student(ctx, actor.ID)
		if err != nil {
			return err
		}
		if !current.VerifiedStudent() {
			return domain.ErrForbidden
		}
		session, err := tx.LockStudentSession(ctx, id, actor.ID)
		if err != nil {
			return err
		}
		if session.StudentID != actor.ID || session.TutorID == actor.ID {
			return domain.ErrNotFound
		}
		if session.Status != scheduling.Completed {
			return domain.ErrIneligible
		}
		result, err = tx.Insert(ctx, session, in)
		return err
	})
	if err != nil {
		return domain.Entry{}, err
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, actor identity.Principal, id string) (domain.Entry, error) {
	if !actor.VerifiedStudent() && !actor.Coordinator() {
		return domain.Entry{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Entry{}, domain.ErrQuery
	}
	return s.repo.Get(ctx, id, actor.ID)
}

func (s *Service) Received(ctx context.Context, actor identity.Principal, p Page) ([]domain.Entry, error) {
	if !actor.CanTutor() {
		return nil, domain.ErrForbidden
	}
	if !p.valid() {
		return nil, domain.ErrQuery
	}
	p.Limit++
	return s.repo.List(ctx, actor.ID, p)
}

func (s *Service) CoordinatorFeedback(ctx context.Context, actor identity.Principal, tutor string, p Page) (Review, error) {
	if !actor.Coordinator() {
		return Review{}, domain.ErrForbidden
	}
	if !p.valid() || !uuid.MatchString(tutor) {
		return Review{}, domain.ErrQuery
	}
	p.Limit++
	return s.repo.CoordinatorFeedback(ctx, actor.ID, strings.ToLower(tutor), p)
}

func (s *Service) RatingSummaries(ctx context.Context, tutors []string) (map[string]domain.TutorRatingSummary, error) {
	stats, err := s.repo.Statistics(ctx, tutors)
	if err != nil {
		return nil, err
	}
	out := make(map[string]domain.TutorRatingSummary, len(stats))
	for id, v := range stats {
		out[id] = v.TutorRatingSummary
	}
	return out, nil
}

func (s *Service) Activity(ctx context.Context, actor identity.Principal, p Page) (Activity, error) {
	if !actor.CanTutor() {
		return Activity{}, domain.ErrForbidden
	}
	if !p.valid() {
		return Activity{}, domain.ErrQuery
	}
	stats, err := s.repo.Statistics(ctx, []string{actor.ID})
	if err != nil {
		return Activity{}, err
	}
	sessions, err := s.sessions.UpcomingTutorSessions(ctx, actor, p.Limit+1, p.Offset)
	if err != nil {
		return Activity{}, err
	}
	return Activity{Statistics: stats[actor.ID], Upcoming: sessions}, nil
}
