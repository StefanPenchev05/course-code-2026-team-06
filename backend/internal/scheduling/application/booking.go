package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	coordination "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/coordination/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
	tutoring "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	tutordomain "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

const (
	ExpirationBatchSize    = 50
	MaxExpirationBatchSize = 500
)

type BookingTransaction interface {
	Student(context.Context, string) (identity.Principal, error)
	Catalog() catalog.AcademicCatalog
	Offerings() tutoring.BookingOfferings
	Discipline() coordination.Discipline
	Source(ctx context.Context, slotID, tutorID string) (domain.Window, domain.Semester, error)
	Policy(context.Context) (string, error)
	InsertSession(ctx context.Context, s domain.Session, policy, faculty string) (domain.Session, error)
	ParticipantSession(ctx context.Context, id, actor string, tutorOnly, lock bool) (domain.Session, error)
	UpdateSession(context.Context, domain.Session, domain.Status) error
	DueRequests(context.Context, time.Time, int) ([]domain.Session, error)
	RecordEvent(context.Context, Event) error
}

type Event struct {
	SessionID, Actor, Reason string
	From, To                 domain.Status
	At                       time.Time
	Recipients               []string
}

type BookingRepository interface {
	TransactBooking(context.Context, func(BookingTransaction) error) error
	ParticipantSession(ctx context.Context, id, actor string) (domain.Session, error)
	ListSessions(ctx context.Context, actor string, status domain.Status, tutorOnly bool, p Page) ([]domain.Session, error)
}

type BookingService struct {
	repo BookingRepository
	now  func() time.Time
}

func NewBooking(repo BookingRepository, now func() time.Time) *BookingService {
	return &BookingService{repo: repo, now: now}
}

func (s *BookingService) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

func lockParticipants(ctx context.Context, tx BookingTransaction, student, tutor string) (identity.Principal, identity.Principal, error) {
	ids := []string{student, tutor}
	sort.Strings(ids)
	first, err := tx.Student(ctx, ids[0])
	if err != nil {
		return identity.Principal{}, identity.Principal{}, err
	}
	second, err := tx.Student(ctx, ids[1])
	if err != nil {
		return identity.Principal{}, identity.Principal{}, err
	}
	if first.ID == student {
		return first, second, nil
	}
	return second, first, nil
}

func eligible(ctx context.Context, tx BookingTransaction, student, tutor identity.Principal, offering string) (tutoring.BookingOffer, error) {
	if !student.VerifiedStudent() || !tutor.CanTutor() {
		return tutoring.BookingOffer{}, domain.ErrForbidden
	}
	o, err := tx.Offerings().LockApprovedOffering(ctx, offering, tutor.ID)
	if errors.Is(err, tutordomain.ErrNotFound) {
		return o, fmt.Errorf("%w: offering is no longer approved and active", domain.ErrBookingInvalid)
	}
	if err != nil {
		return o, err
	}
	for _, program := range []string{student.ProgramID, tutor.ProgramID} {
		ok, err := tx.Catalog().SubjectBelongsToProgram(ctx, program, o.SubjectID)
		if err != nil {
			return o, err
		}
		if !ok {
			return o, fmt.Errorf("%w: subject must belong to both programs", domain.ErrBookingInvalid)
		}
	}
	return o, nil
}

func (s *BookingService) Request(ctx context.Context, actor identity.Principal, input domain.Request) (domain.Session, error) {
	if !actor.VerifiedStudent() {
		return domain.Session{}, domain.ErrForbidden
	}
	if !uuid.MatchString(input.OfferingID) || !uuid.MatchString(input.SlotID) {
		return domain.Session{}, domain.ErrBookingInvalid
	}
	if err := input.Validate(); err != nil {
		return domain.Session{}, err
	}
	var result domain.Session
	err := s.repo.TransactBooking(ctx, func(tx BookingTransaction) error {
		tutorID, err := tx.Offerings().ResolveTutor(ctx, input.OfferingID)
		if errors.Is(err, tutordomain.ErrNotFound) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		if tutorID == actor.ID {
			return fmt.Errorf("%w: cannot tutor yourself", domain.ErrBookingInvalid)
		}
		student, tutor, err := lockParticipants(ctx, tx, actor.ID, tutorID)
		if err != nil {
			return err
		}
		o, err := eligible(ctx, tx, student, tutor, input.OfferingID)
		if err != nil {
			return err
		}
		faculty, err := tx.Catalog().GetProgramFaculty(ctx, tutor.ProgramID)
		if err != nil {
			return err
		}
		window, semester, err := tx.Source(ctx, input.SlotID, tutorID)
		if err != nil {
			return err
		}
		if !input.Fits(window) || input.StartsAt.Before(semester.StartsAt) || input.End().After(semester.EndsAt) {
			return fmt.Errorf("%w: session time and mode must fit the window and semester", domain.ErrBookingInvalid)
		}
		policy, err := tx.Policy(ctx)
		if err != nil {
			return err
		}
		now := s.clock()
		expires, err := domain.Expiration(now, input.StartsAt)
		if err != nil {
			return err
		}
		result, err = tx.InsertSession(ctx, domain.Session{
			StudentID: student.ID, TutorID: tutor.ID, OfferingID: o.ID, SlotID: input.SlotID, SemesterID: semester.ID,
			StartsAt: input.StartsAt, EndsAt: input.End(), Minutes: input.Minutes, Mode: input.Mode, Purpose: input.Purpose,
			Description: strings.TrimSpace(input.Description), Status: domain.Requested, RequestedAt: now, ExpiresAt: expires,
		}, policy, faculty)
		if err != nil {
			return err
		}
		return tx.RecordEvent(ctx, Event{SessionID: result.ID, Actor: actor.ID, To: domain.Requested, At: now, Recipients: []string{tutor.ID}})
	})
	if err != nil {
		return domain.Session{}, err
	}
	result.SetViewer(actor.ID)
	return result, nil
}

func (s *BookingService) Accept(ctx context.Context, actor identity.Principal, id string, meeting domain.Meeting) (domain.Session, error) {
	meeting.Link, meeting.Room = strings.TrimSpace(meeting.Link), strings.TrimSpace(meeting.Room)
	return s.respond(ctx, actor, id, domain.Accepted, meeting, "")
}

func (s *BookingService) Decline(ctx context.Context, actor identity.Principal, id, reason string) (domain.Session, error) {
	return s.respond(ctx, actor, id, domain.Declined, domain.Meeting{}, reason)
}

func (s *BookingService) Cancel(ctx context.Context, actor identity.Principal, id, reason string) (domain.Session, error) {
	return s.respond(ctx, actor, id, domain.Cancelled, domain.Meeting{}, reason)
}

func (s *BookingService) respond(ctx context.Context, actor identity.Principal, id string, to domain.Status, meeting domain.Meeting, reason string) (domain.Session, error) {
	tutorOnly := to != domain.Cancelled
	if !actor.VerifiedStudent() || (tutorOnly && !actor.CanTutor()) {
		return domain.Session{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Session{}, domain.ErrBookingInvalid
	}
	if to != domain.Accepted {
		var err error
		reason, err = domain.Reason(reason)
		if err != nil {
			return domain.Session{}, err
		}
	}
	var result domain.Session
	err := s.repo.TransactBooking(ctx, func(tx BookingTransaction) error {
		hint, err := tx.ParticipantSession(ctx, id, actor.ID, tutorOnly, false)
		if err != nil {
			return err
		}
		student, tutor, err := lockParticipants(ctx, tx, hint.StudentID, hint.TutorID)
		if err != nil {
			return err
		}
		participant := student
		recipient := tutor.ID
		if actor.ID == tutor.ID {
			participant, recipient = tutor, student.ID
		}
		if !participant.VerifiedStudent() || (tutorOnly && !tutor.CanTutor()) {
			return domain.ErrForbidden
		}
		session, err := tx.ParticipantSession(ctx, id, actor.ID, tutorOnly, true)
		if err != nil {
			return err
		}
		if err := session.Transition(to, s.clock()); err != nil {
			return err
		}
		if to == domain.Accepted {
			if _, err := eligible(ctx, tx, student, tutor, session.OfferingID); err != nil {
				return err
			}
			if err := meeting.Validate(session.Mode); err != nil {
				return err
			}
		}
		var home string
		if to == domain.Cancelled {
			home, err = tx.Catalog().GetProgramFaculty(ctx, participant.ProgramID)
			if err != nil {
				return err
			}
		}
		now := s.clock()
		if err := session.Transition(to, now); err != nil {
			return err
		}
		previous := session.Status
		session.Status = to
		switch to {
		case domain.Accepted:
			session.RespondedAt = &now
			session.MeetingLink, session.MeetingRoom = meeting.Link, meeting.Room
		case domain.Declined:
			session.RespondedAt = &now
			session.DeclineReason = reason
		case domain.Cancelled:
			session.CancelledAt = &now
			session.CancelledBy, session.CancellationReason = actor.ID, reason
			session.Late = domain.LateCancellation(now, session.StartsAt)
		}
		if err := tx.UpdateSession(ctx, session, previous); err != nil {
			return err
		}
		if session.Late {
			if err := tx.Discipline().RecordLateCancellation(ctx, coordination.LateCancellation{
				UserID: actor.ID, SemesterID: session.SemesterID, SessionID: session.ID, HomeFacultyID: home, OccurredAt: now,
			}); err != nil {
				return err
			}
		}
		if err := tx.RecordEvent(ctx, Event{SessionID: id, Actor: actor.ID, From: previous, To: to, At: now, Reason: reason, Recipients: []string{recipient}}); err != nil {
			return err
		}
		result = session
		return nil
	})
	if err != nil {
		return domain.Session{}, err
	}
	result.SetViewer(actor.ID)
	return result, nil
}

// ExpireDueRequests processes a single bounded batch. Stage 3 can call this
// repeatedly. An unprocessed request past its deadline cannot be answered.
func (s *BookingService) ExpireDueRequests(ctx context.Context, now time.Time) (int, error) {
	return s.ExpireDueRequestsBatch(ctx, now, ExpirationBatchSize)
}

func (s *BookingService) ExpireDueRequestsBatch(ctx context.Context, now time.Time, limit int) (int, error) {
	if now.IsZero() || limit < 1 || limit > MaxExpirationBatchSize {
		return 0, domain.ErrBookingInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	count := 0
	err := s.repo.TransactBooking(ctx, func(tx BookingTransaction) error {
		sessions, err := tx.DueRequests(ctx, now, limit)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if err := session.Transition(domain.Expired, now); err != nil {
				return err
			}
			session.Status = domain.Expired
			if err := tx.UpdateSession(ctx, session, domain.Requested); err != nil {
				return err
			}
			if err := tx.RecordEvent(ctx, Event{SessionID: session.ID, From: domain.Requested, To: domain.Expired, At: now,
				Recipients: []string{session.StudentID, session.TutorID}}); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *BookingService) Get(ctx context.Context, actor identity.Principal, id string) (domain.Session, error) {
	if !actor.VerifiedStudent() {
		return domain.Session{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Session{}, domain.ErrBookingInvalid
	}
	session, err := s.repo.ParticipantSession(ctx, id, actor.ID)
	session.SetViewer(actor.ID)
	return session, err
}

func (s *BookingService) List(ctx context.Context, actor identity.Principal, status domain.Status, tutorOnly bool, p Page) ([]domain.Session, error) {
	if !actor.VerifiedStudent() || (tutorOnly && !actor.CanTutor()) {
		return nil, domain.ErrForbidden
	}
	if (status != "" && !status.Valid()) || !validPage(p) {
		return nil, domain.ErrBookingInvalid
	}
	p.Limit++
	sessions, err := s.repo.ListSessions(ctx, actor.ID, status, tutorOnly, p)
	for i := range sessions {
		sessions[i].SetViewer(actor.ID)
	}
	return sessions, err
}
