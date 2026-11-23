package application

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

const suspensionPageSize = 100

type suspensionSessionWriter interface {
	sessionOutcomeWriter
	LockFutureSessions(ctx context.Context, target string, at time.Time, after string, limit int) ([]domain.Session, error)
}

// CancelForSuspension requires the target's Identity account lock and the caller's
// transaction. Keyset pages bound memory, never the total number cancelled. Any
// error requires rollback of the account decision and all earlier pages.
func CancelForSuspension(ctx context.Context, writer suspensionSessionWriter, target, actor, decision string, at time.Time) (int, error) {
	if target == "" || actor == "" || decision == "" || at.IsZero() {
		return 0, domain.ErrBookingInvalid
	}
	at = at.UTC().Truncate(time.Microsecond)
	count, after := 0, ""
	for {
		sessions, err := writer.LockFutureSessions(ctx, target, at, after, suspensionPageSize)
		if err != nil {
			return 0, err
		}
		if len(sessions) == 0 {
			return count, nil
		}
		for _, session := range sessions {
			updated, err := session.CancelForSuspension(target, actor, at)
			if err != nil {
				return 0, err
			}
			if _, err := recordCancellation(ctx, writer, session, updated, actor, domain.SuspensionCancellation, decision, at); err != nil {
				return 0, err
			}
			count++
			after = session.ID
		}
	}
}
