package application

import "context"

type BookingOffer struct{ ID, TutorID, SubjectID string }

// BookingOfferings is bound to the caller's transaction. ResolveTutor supplies
// a lock-order hint; LockApprovedOffering validates the current entitlement.
type BookingOfferings interface {
	ResolveTutor(ctx context.Context, offeringID string) (string, error)
	LockApprovedOffering(ctx context.Context, offeringID, tutorID string) (BookingOffer, error)
}
