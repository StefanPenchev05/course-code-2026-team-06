package application

import (
	"context"

	catalog "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/catalog/application"
	identity "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/identity/domain"
	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/tutoring/domain"
)

// EligibilityTransaction is the narrow, transaction-bound port for reconciliation.
// Student must lock the Identity account before reading its current program;
// Catalog and offering operations must use that same transaction.
type EligibilityTransaction interface {
	Student(context.Context, string) (identity.Principal, error)
	Catalog() catalog.AcademicCatalog
	ActiveOfferings(context.Context, string) ([]domain.Offering, error)
	// Program and faculty come from the locked Identity/Catalog reads above,
	// and become the withdrawal's immutable event context.
	Deactivate(ctx context.Context, offering, program, faculty string) error
}

// RevalidateTutorEligibility reconciles current offerings in its own transaction.
// It never reactivates offerings or changes applications, qualifications or sessions.
func (s *Service) RevalidateTutorEligibility(ctx context.Context, studentID string) error {
	if !uuid.MatchString(studentID) {
		return domain.ErrInvalid
	}
	return s.repo.InTransaction(ctx, func(tx Transaction) error {
		return RevalidateTutorEligibilityInTransaction(ctx, tx, studentID)
	})
}

// RevalidateTutorEligibilityInTransaction shares reconciliation with a trusted
// caller's program update. The caller owns commit/rollback and MUST roll back on
// error. Lock the Identity account before updating the program, using the same
// account order as booking (UUID order if multiple accounts are involved).
// This function does not open, commit or roll back a transaction.
func RevalidateTutorEligibilityInTransaction(ctx context.Context, tx EligibilityTransaction, studentID string) error {
	if !uuid.MatchString(studentID) {
		return domain.ErrInvalid
	}
	student, err := tx.Student(ctx, studentID)
	if err != nil {
		return err
	}
	reader := tx.Catalog()
	// Lock the current program as well as each existing subject membership.
	faculty, err := reader.GetProgramFaculty(ctx, student.ProgramID)
	if err != nil {
		return err
	}
	offerings, err := tx.ActiveOfferings(ctx, studentID)
	if err != nil {
		return err
	}
	for _, offering := range offerings {
		ok, err := reader.SubjectBelongsToProgram(ctx, student.ProgramID, offering.SubjectID)
		if err != nil {
			return err
		}
		if !ok {
			if err := tx.Deactivate(ctx, offering.ID, student.ProgramID, faculty); err != nil {
				return err
			}
		}
	}
	return nil
}
