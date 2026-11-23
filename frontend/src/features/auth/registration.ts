import { ApiError } from '../../shared/api/client';
import { errorMessage } from '../../shared/api/errors';

/**
 * Form checks for registration. They only give quick feedback: the server
 * re-checks everything (backend/internal/identity/application/service.go)
 * and also checks the university email domain, which the browser can't know.
 * Limits are in bytes on the server, so lengths are measured in UTF-8 bytes.
 */

export interface RegistrationForm {
  name: string;
  email: string;
  studentNumber: string;
  facultyId: string;
  programId: string;
  password: string;
  passwordConfirm: string;
}

export type RegistrationField = keyof RegistrationForm;
export type RegistrationErrors = Partial<Record<RegistrationField, string>>;

export const LIMITS = { name: 200, studentNumber: 64, email: 254, passwordMin: 12, passwordMax: 256 } as const;

export function byteLength(value: string): number {
  return new TextEncoder().encode(value).length;
}

export function validateRegistration(form: RegistrationForm): RegistrationErrors {
  const errors: RegistrationErrors = {};
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

  const passwordBytes = byteLength(form.password);
  if (!form.password) errors.password = 'Choose a password.';
  else if (passwordBytes < LIMITS.passwordMin) errors.password = `Use at least ${LIMITS.passwordMin} characters.`;
  else if (passwordBytes > LIMITS.passwordMax) errors.password = 'This password is too long.';

  if (!errors.password && form.passwordConfirm !== form.password) errors.passwordConfirm = "The passwords don't match.";

  return errors;
}

export interface ServerProblem {
  fields: RegistrationErrors;
  form: string | null;
  /** 409: the email or student number already has an account. */
  alreadyRegistered: boolean;
}

/** Turns a failed POST /auth/register into messages next to the right fields. */
export function registrationProblem(error: unknown): ServerProblem {
  if (error instanceof ApiError && error.status === 422) {
    const message = error.message.toLowerCase();
    if (message.includes('email')) {
      return { fields: { email: 'Use your university email address. Other email addresses can’t register.' }, form: null, alreadyRegistered: false };
    }
    if (message.includes('password')) {
      return { fields: { password: `Use ${LIMITS.passwordMin} to ${LIMITS.passwordMax} characters.` }, form: null, alreadyRegistered: false };
    }
  }
  if (error instanceof ApiError && error.status === 409) {
    return {
      fields: {},
      form: 'An account with this email or student number already exists.',
      alreadyRegistered: true,
    };
  }
  if (error instanceof ApiError && error.status === 404) {
    return {
      fields: { programId: 'This program is no longer available. Choose another one.' },
      form: null,
      alreadyRegistered: false,
    };
  }
  return {
    fields: {},
    form: errorMessage(error, {
      403: import.meta.env.DEV
        ? 'The API refused this request. Open the app at http://localhost:5173 and set PUBLIC_ORIGIN=http://localhost:5173 in the backend .env.'
        : "Registering isn't possible from this page.",
    }),
    alreadyRegistered: false,
  };
}
