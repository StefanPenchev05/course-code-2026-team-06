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

/**
 * Everything that decides what this user may see: identity, roles, verification,
 * program and coordinator faculty (the same fields RealtimeProvider restarts on).
 * When it changes, protected pages load again from scratch (#93), so nothing from
 * the old access stays on screen.
 */
export function accessKey(user: CurrentUser): string {
  return [user.id, [...user.roles].sort().join('+'), user.verified_student, user.verification_status ?? '', user.program_id ?? '', user.coordinator_faculty_id ?? ''].join('|');
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
