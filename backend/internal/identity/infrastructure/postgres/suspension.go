package postgres

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/jackc/pgx/v5"
)

type suspensionAccounts struct{ tx pgx.Tx }

func SuspensionAccounts(tx pgx.Tx) application.SuspensionAccounts { return suspensionAccounts{tx} }

func (a suspensionAccounts) LockAdministration(ctx context.Context, actor, target string) (domain.Principal, string, error) {
	ids := []string{actor}
	if target != actor {
		ids = append(ids, target)
	}
	sort.Strings(ids)
	for i, id := range ids {
		query := "SELECT id::text FROM users WHERE id=$1 FOR NO KEY UPDATE"
		// Existing tutor review locks coordinator then student; attendance does
		// the reverse. Do not wait on a second account while holding the first.
		// The owning transaction retries 55P03 after rolling back all its locks.
		if i > 0 {
			query += " NOWAIT"
		}
		var locked string
		err := a.tx.QueryRow(ctx, query, id).Scan(&locked)
		if errors.Is(err, pgx.ErrNoRows) {
			if id == actor {
				return domain.Principal{}, "", domain.ErrForbidden
			}
			return domain.Principal{}, "", domain.ErrNotFound
		}
		if err != nil {
			return domain.Principal{}, "", err
		}
	}
	faculty, err := LockCoordinatorFaculty(ctx, a.tx, actor)
	if err != nil {
		return domain.Principal{}, "", err
	}
	p, err := LockStudent(ctx, a.tx, target)
	return p, faculty, err
}

func (a suspensionAccounts) Change(ctx context.Context, target domain.Principal, faculty, actor, input string, suspend bool, at time.Time) (domain.SuspensionDecision, error) {
	reason, err := domain.SuspensionReason(input)
	if err != nil {
		return domain.SuspensionDecision{}, err
	}
	if err := target.ChangeSuspension(suspend); err != nil {
		return domain.SuspensionDecision{}, err
	}
	// Keep the last suspension metadata on reinstatement; its original reason
	// remains intact, including on legacy accounts without a history backfill.
	tag, err := a.tx.Exec(ctx, `UPDATE users SET is_suspended=$2,
		suspended_at=CASE WHEN $2 THEN $3 ELSE suspended_at END,
		suspended_by_coordinator_id=CASE WHEN $2 THEN $4::uuid ELSE suspended_by_coordinator_id END,
		suspension_reason=CASE WHEN $2 THEN $5 ELSE suspension_reason END
		WHERE id=$1 AND is_suspended=$6 AND closed_at IS NULL`, target.ID, suspend, at, actor, reason, target.Suspended)
	if err != nil {
		return domain.SuspensionDecision{}, err
	}
	if tag.RowsAffected() != 1 {
		return domain.SuspensionDecision{}, domain.ErrConflict
	}
	d := domain.SuspensionDecision{UserID: target.ID, FacultyID: faculty, ActorID: actor, WasSuspended: target.Suspended, IsSuspended: suspend, Reason: reason, At: at}
	err = a.tx.QueryRow(ctx, `INSERT INTO user_suspension_history
		(user_id,faculty_id,changed_by_coordinator_id,was_suspended,is_suspended,reason,changed_at)
		VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`, d.UserID, d.FacultyID, d.ActorID, d.WasSuspended, d.IsSuspended, d.Reason, d.At).Scan(&d.ID)
	if err != nil {
		return domain.SuspensionDecision{}, err
	}
	kind, subject, body := "USER_REINSTATED", "Tutti account reinstated", "Your account suspension has been lifted. Existing verification and tutoring eligibility requirements still apply."
	if suspend {
		kind, subject, body = "USER_SUSPENDED", "Tutti account suspended", "Your Tutti account has been suspended. Contact your home-faculty coordinator for information."
	}
	_, err = a.tx.Exec(ctx, `INSERT INTO notifications(recipient_user_id,type,subject,body,deduplication_key)
		VALUES($1,$2::text::notification_type,$3,$4,$5)`, target.ID, kind, subject, body, "account-decision:"+d.ID)
	if err != nil {
		return domain.SuspensionDecision{}, err
	}
	return d, nil
}
