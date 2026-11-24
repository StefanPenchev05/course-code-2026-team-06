/** Shapes returned by the Identity API (backend/internal/identity/interfaces/http/handler.go). */

export type Role = 'student' | 'tutor' | 'coordinator';

export type VerificationStatus = 'PENDING' | 'APPROVED' | 'REJECTED';

export interface CurrentUser {
  id: string;
  name: string;
  email: string;
  roles: Role[];
  verified_student: boolean;
  verification_status?: VerificationStatus;
  rejection_reason?: string;
  coordinator_faculty_id?: string;
  /** The student's current program (students and tutors only). A UI hint: the server still checks membership. */
  program_id?: string;
}

export function hasRole(user: CurrentUser | null, role: Role): boolean {
  return user !== null && user.roles.includes(role);
}

/** Body of POST /auth/register (and POST /auth/resubmit-verification, without password). */
export interface StudentDetails {
  name: string;
  email: string;
  student_number: string;
  program_id: string;
}

export interface RegistrationInput extends StudentDetails {
  password: string;
}

/** 201 response of POST /auth/register. Registering does not log the student in. */
export interface VerificationCreated {
  verification_id: string;
  verification_status: VerificationStatus;
}
