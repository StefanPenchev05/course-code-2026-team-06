package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/scheduling/domain"
)

type suspensionWriter struct {
	events         []Event
	updates, pages int
	fail           int
	at             time.Time
}

func (w *suspensionWriter) LockFutureSessions(_ context.Context, target string, at time.Time, after string, limit int) ([]domain.Session, error) {
	w.pages++
	if limit != 100 || target != "target" || !at.Equal(w.at) {
		return nil, errors.New("bad scope")
	}
	start := 0
	if after != "" {
		_, _ = fmt.Sscanf(after, "%d", &start)
	}
	var sessions []domain.Session
	for i := start + 1; i <= 205 && len(sessions) < limit; i++ {
		sessions = append(sessions, domain.Session{ID: fmt.Sprintf("%04d", i), StudentID: "target", TutorID: "tutor", StartsAt: at, Status: domain.Requested})
	}
	return sessions, nil
}
func (w *suspensionWriter) UpdateSession(_ context.Context, s domain.Session, from domain.Status) error {
	w.updates++
	return nil
}
func (w *suspensionWriter) RecordEvent(_ context.Context, e Event) error {
	if w.updates == w.fail {
		return errors.New("outbox unavailable")
	}
	w.events = append(w.events, e)
	return nil
}
func TestSuspensionCancelsEveryPageOrFails(t *testing.T) {
	at := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, fail := range []int{0, 102} {
		w := &suspensionWriter{at: at, fail: fail}
		n, err := CancelForSuspension(context.Background(), w, "target", "actor", "decision", at)
		if fail != 0 {
			if err == nil || n != 0 {
				t.Fatal(n, err)
			}
			continue
		}
		if err != nil || n != 205 || len(w.events) != 205 || w.pages != 4 {
			t.Fatal(n, err, w.pages)
		}
		for _, e := range w.events {
			if e.CancellationOrigin != domain.SuspensionCancellation || e.SuspensionHistoryID != "decision" || e.Actor != "actor" || e.Reason != domain.SuspensionSessionReason || len(e.Recipients) != 2 {
				t.Fatal(e)
			}
		}
	}
}
