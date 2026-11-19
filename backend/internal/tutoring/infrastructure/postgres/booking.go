package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/application"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

type bookingOfferings struct{ tx pgx.Tx }

func BookingOfferings(tx pgx.Tx) application.BookingOfferings { return bookingOfferings{tx} }

func (b bookingOfferings) ResolveTutor(ctx context.Context, id string) (string, error) {
	var tutor string
	err := b.tx.QueryRow(ctx, "SELECT tutor_id::text FROM tutor_offerings WHERE id=$1", id).Scan(&tutor)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return tutor, err
}

func (b bookingOfferings) LockApprovedOffering(ctx context.Context, id, tutor string) (application.BookingOffer, error) {
	var o application.BookingOffer
	// Identity locks and verifies both users. Catalog locks current membership.
	// Keep each row that backs the entitlement stable through booking commit.
	err := b.tx.QueryRow(ctx, `
		SELECT o.id::text,o.tutor_id::text,o.subject_id::text
		FROM tutor_offerings o
		JOIN tutor_profiles tp ON tp.student_id=o.tutor_id
		JOIN tutor_qualifications q ON q.id=o.qualification_id
		JOIN tutor_applications a ON a.id=q.application_id
		WHERE o.id=$1 AND o.tutor_id=$2 AND o.is_active AND tp.is_active
		  AND tp.approval_ended_at IS NULL AND a.status='APPROVED'
		FOR SHARE OF o,tp,q,a`, id, tutor).Scan(&o.ID, &o.TutorID, &o.SubjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return o, err
}
