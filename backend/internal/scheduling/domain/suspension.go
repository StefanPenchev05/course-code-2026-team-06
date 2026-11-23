package domain

import "time"

// SuspensionSessionReason is shared with both participants. The private account
// decision reason belongs exclusively to Identity's administrative history.
const SuspensionSessionReason = "Session cancelled following an account suspension."

func (s Session) CancelForSuspension(target, actor string, at time.Time) (Session, error) {
	if target == "" || (s.StudentID != target && s.TutorID != target) || s.StartsAt.Before(at) {
		return Session{}, ErrBookingState
	}
	return s.cancelAdministratively(actor, SuspensionSessionReason, at)
}
