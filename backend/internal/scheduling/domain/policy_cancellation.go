package domain

import "time"

type CancellationOrigin string

const (
	ParticipantCancellation       CancellationOrigin = "PARTICIPANT"
	CoordinatorPolicyCancellation CancellationOrigin = "COORDINATOR_POLICY"
	SuspensionCancellation        CancellationOrigin = "SUSPENSION"
)

// CancelForPolicy shares the existing cancellation state machine. In particular,
// an unexpired deadline is required for acceptance, not for cancellation. A still
// active session may be cancelled until another terminal transition wins its lock.
func (s Session) CancelForPolicy(actor, input string, now time.Time) (Session, error) {
	return s.cancelAdministratively(actor, input, now)
}

func (s Session) cancelAdministratively(actor, input string, now time.Time) (Session, error) {
	reason, err := Reason(input)
	if err != nil {
		return Session{}, err
	}
	if actor == "" || now.IsZero() || now.Before(s.RequestedAt) {
		return Session{}, ErrBookingInvalid
	}
	if err := s.Transition(Cancelled, now); err != nil {
		return Session{}, err
	}
	s.Status, s.CancelledAt, s.CancelledBy, s.CancellationReason = Cancelled, &now, actor, reason
	s.Late = false
	return s, nil
}
