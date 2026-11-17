package application

import (
	"context"
	"time"

	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type sessionOutcomeWriter interface {
	UpdateSession(context.Context, domain.Session, domain.Status) error
	RecordEvent(context.Context, Event) error
}

func (s *BookingService) Complete(ctx context.Context, actor identity.Principal, id string) (domain.Session, error) {
	// A former tutor can complete their own historic booking while their
	// verified student account is still active.
	if !actor.VerifiedStudent() {
		return domain.Session{}, domain.ErrForbidden
	}
	if !uuid.MatchString(id) {
		return domain.Session{}, domain.ErrBookingInvalid
	}
	var result domain.Session
	err := s.repo.TransactBooking(ctx, func(tx BookingTransaction) error {
		current, err := tx.Student(ctx, actor.ID)
		if err != nil {
			return err
		}
		if !current.VerifiedStudent() {
			return domain.ErrForbidden
		}
		session, err := tx.ParticipantSession(ctx, id, actor.ID, true, true)
		if err != nil {
			return err
		}
		now := s.clock()
		session.CompletedAt, session.CompletedBy = &now, actor.ID
		if err := changeOutcome(ctx, tx, session, domain.Completed, actor.ID, "", now); err != nil {
			return err
		}
		session.Status = domain.Completed
		result = session
		return nil
	})
	if err != nil {
		return domain.Session{}, err
	}
	result.SetViewer(actor.ID)
	return result, nil
}

// CorrectNoShow applies an authorized Coordination decision in the caller's
// transaction. A second confirmed allegation can concern the other participant.
func CorrectNoShow(ctx context.Context, writer sessionOutcomeWriter, session domain.Session, reviewer, note string, now time.Time) error {
	if session.Status == domain.NoShow {
		return nil
	}
	return changeOutcome(ctx, writer, session, domain.NoShow, reviewer, note, now)
}

func changeOutcome(ctx context.Context, writer sessionOutcomeWriter, session domain.Session, to domain.Status, actor, reason string, now time.Time) error {
	if err := session.Transition(to, now); err != nil {
		return err
	}
	from := session.Status
	session.Status = to
	if err := writer.UpdateSession(ctx, session, from); err != nil {
		return err
	}
	return writer.RecordEvent(ctx, Event{SessionID: session.ID, Actor: actor, From: from, To: to, At: now, Reason: reason})
}
