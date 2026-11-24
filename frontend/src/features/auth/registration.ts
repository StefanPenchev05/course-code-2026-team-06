import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';
import type { StudentDetails } from './types';

/**
 * Form checks for registration and resubmission. They only give quick
 * feedback: the server re-checks everything
 * (backend/internal/identity/application/service.go) and also checks the
 * university email domain, which the browser can't know. Limits are in bytes
 * on the server, so lengths are measured in UTF-8 bytes.
 */

/** The student details shared by registration and resubmission. */
export interface StudentDetailsForm {
  name: string;
  email: string;
  studentNumber: string;
  facultyId: string;
  programId: string;
}

export interface RegistrationForm extends StudentDetailsForm {
  password: string;
  passwordConfirm: string;
}

export type FieldErrors<F> = Partial<Record<keyof F, string>>;
export type RegistrationErrors = FieldErrors<RegistrationForm>;

export const LIMITS = { name: 200, studentNumber: 64, email: 254, passwordMin: 12, passwordMax: 256 } as const;

export const EMPTY_DETAILS: StudentDetailsForm = { name: '', email: '', studentNumber: '', facultyId: '', programId: '' };

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

export function validateStudentDetails(form: StudentDetailsForm): FieldErrors<StudentDetailsForm> {
  const errors: FieldErrors<StudentDetailsForm> = {};
  const name = form.name.trim();
  const email = form.email.trim();
  const studentNumber = form.studentNumber.trim();

  if (!name) errors.name = 'Enter your full name.';
  else if (byteLength(name) > LIMITS.name) errors.name = 'Your name is too long.';

  if (!email) errors.email = 'Enter your university email.';
  else if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) || byteLength(email) > LIMITS.email)
    errors.email = 'Enter a valid email address.';

  if (!studentNumber) errors.studentNumber = 'Enter your student number.';
  else if (byteLength(studentNumber) > LIMITS.studentNumber) errors.studentNumber = 'Your student number is too long.';

  if (!form.facultyId) errors.facultyId = 'Choose your faculty.';
  else if (!form.programId) errors.programId = 'Choose your study program.';

  return errors;
}

export function validateRegistration(form: RegistrationForm): RegistrationErrors {
  const errors: RegistrationErrors = validateStudentDetails(form);

  const passwordBytes = byteLength(form.password);
  if (!form.password) errors.password = 'Choose a password.';
  else if (passwordBytes < LIMITS.passwordMin) errors.password = `Use at least ${LIMITS.passwordMin} characters.`;
  else if (passwordBytes > LIMITS.passwordMax) errors.password = 'This password is too long.';

  if (!errors.password && form.passwordConfirm !== form.password) errors.passwordConfirm = "The passwords don't match.";

  return errors;
}

export function hasErrors(errors: object): boolean {
  return Object.values(errors).some(Boolean);
}

/** The JSON body the API expects, with the same trimming the server applies. */
export function toStudentDetails(form: StudentDetailsForm): StudentDetails {
  return {
    name: form.name.trim(),
    email: form.email.trim(),
    student_number: form.studentNumber.trim(),
    program_id: form.programId,
  };
}

export interface ServerProblem {
  fields: RegistrationErrors;
  form: string | null;
  /** Registration 409: the email or student number already has an account. */
  alreadyRegistered: boolean;
}

const DEV_ORIGIN_HINT =
  'The API refused this request. Open the app at http://localhost:5173 and set PUBLIC_ORIGIN=http://localhost:5173 in the backend .env.';

/**
 * Turns a failed POST /auth/register or /auth/resubmit-verification into
 * messages next to the right fields. The two share status codes, except
 * that 409 and 403 mean different things for each.
 */
export function studentDetailsProblem(error: unknown, kind: 'register' | 'resubmit'): ServerProblem {
  const none = { fields: {}, form: null, alreadyRegistered: false };
  if (error instanceof ApiError && error.status === 422) {
    const message = error.message.toLowerCase();
    if (message.includes('email')) {
      return { ...none, fields: { email: 'Use your university email address. Other email addresses can’t register.' } };
    }
    if (message.includes('password')) {
      return { ...none, fields: { password: `Use ${LIMITS.passwordMin} to ${LIMITS.passwordMax} characters.` } };
    }
  }
  if (error instanceof ApiError && error.status === 409) {
    return kind === 'register'
      ? { ...none, form: 'An account with this email or student number already exists.', alreadyRegistered: true }
      : {
          ...none,
          // The API uses 409 both for a taken email/student number and for a
          // registration that is no longer waiting for corrections.
          form: "These details couldn't be sent. The email or student number may belong to another account, or your registration was already resubmitted. Reload the page to see its current status.",
        };
  }
  if (error instanceof ApiError && error.status === 404) {
    return { ...none, fields: { programId: 'This program is no longer available. Choose another one.' } };
  }
  return {
    ...none,
    form: errorMessage(error, {
      403: import.meta.env.DEV
        ? kind === 'resubmit'
          ? `Your account can't send changes right now (it may be suspended or closed). ${DEV_ORIGIN_HINT}`
          : DEV_ORIGIN_HINT
        : kind === 'resubmit'
          ? "Your account can't send changes right now. If it was suspended or closed, contact your faculty coordinator."
          : "Registering isn't possible from this page.",
    }),
  };
}
