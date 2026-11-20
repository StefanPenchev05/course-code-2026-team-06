package application

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

// CancelForPolicy consumes an already authorized, locked session in the caller's
// transaction. It intentionally has no eligibility or discipline capability.
func CancelForPolicy(ctx context.Context, writer sessionOutcomeWriter, session domain.Session, actor, reason string, now time.Time) (domain.Session, error) {
	now = now.UTC().Truncate(time.Microsecond)
	updated, err := session.CancelForPolicy(actor, reason, now)
	if err != nil {
		return domain.Session{}, err
	}
	return recordCancellation(ctx, writer, session, updated, actor, domain.CoordinatorPolicyCancellation, "", now)
}

func recordCancellation(ctx context.Context, writer sessionOutcomeWriter, session, updated domain.Session, actor string, origin domain.CancellationOrigin, decision string, now time.Time) (domain.Session, error) {
	if err := writer.UpdateSession(ctx, updated, session.Status); err != nil {
		return domain.Session{}, err
	}
	err := writer.RecordEvent(ctx, Event{SessionID: session.ID, Actor: actor, From: session.Status, To: domain.Cancelled,
		At: now, Reason: updated.CancellationReason, CancellationOrigin: origin, SuspensionHistoryID: decision,
		Recipients: []string{session.StudentID, session.TutorID}})
	if err != nil {
		return domain.Session{}, err
	}
	return updated, nil
}
