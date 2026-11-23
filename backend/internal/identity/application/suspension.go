package application

import (
	"context"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
)

// SuspensionAccounts is a transaction-bound capability. LockAdministration locks
// both accounts, rechecks the active coordinator and locks the target membership.
// Change persists the account, history and account notice in the same transaction.
type SuspensionAccounts interface {
	LockAdministration(ctx context.Context, actor, target string) (domain.Principal, string, error)
	Change(ctx context.Context, target domain.Principal, faculty, actor, reason string, suspend bool, at time.Time) (domain.SuspensionDecision, error)
}
