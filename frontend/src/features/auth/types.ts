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
}

export function hasRole(user: CurrentUser | null, role: Role): boolean {
  return user !== null && user.roles.includes(role);
}
