/** Shapes from backend/internal/identity (docs/identity.md, "API"). */

export type VerificationStatus = 'PENDING' | 'APPROVED' | 'REJECTED';

/**
 * A student's registration check. Name, email, student number and program are left out
 * when the student has since moved to another faculty (the old decision stays visible).
 */
export interface Verification {
  id: string;
  student_id: string;
  faculty_id: string;
  name?: string;
  email?: string;
  student_number?: string;
  program_id?: string;
  status: VerificationStatus;
  submitted_at: string;
  reviewed_at?: string;
  reviewed_by?: string;
  rejection_reason?: string;
}
