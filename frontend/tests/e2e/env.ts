import { randomBytes } from 'node:crypto';

/**
 * Settings for the browser journey tests. The backend must accept
 * E2E_EMAIL_DOMAIN in UNIVERSITY_EMAIL_DOMAINS (see tests/e2e/README.md).
 */
export const E2E = {
  baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:5173',
  databaseURL: process.env.E2E_DATABASE_URL ?? '',
  emailDomain: process.env.E2E_EMAIL_DOMAIN ?? 'example.test',
  /** Set only when the API runs with OIDC_ENABLED=true, e.g. http://localhost:8180/realms/tutti-demo (#68). */
  oidcIssuer: process.env.E2E_OIDC_ISSUER ?? '',
  /** "1" when the Tutti worker runs against the same database (CI does), so requests really expire (#94). */
  worker: process.env.E2E_WORKER === '1',
};

/** A new address for every run, so the journeys can be repeated on the same database. */
export function uniqueEmail(prefix: string): string {
  return `e2e-${prefix}-${Date.now()}-${randomBytes(3).toString('hex')}@${E2E.emailDomain}`;
}

export function uniqueStudentNumber(): string {
  return `E2E-${randomBytes(5).toString('hex').toUpperCase()}`;
}

export function newPassword(): string {
  return `e2e-${randomBytes(12).toString('hex')}`;
}

export interface TestCatalog {
  facultyId: string;
  facultyName: string;
  programId: string;
  programLabel: string;
  subjectId: string;
  subjectCode: string;
  subjectName: string;
  /** Runs from yesterday to 120 days from now. */
  semesterId: string;
  coordinatorEmail: string;
  coordinatorPassword: string;
}

/**
 * The faculty, program, subject and coordinator global-setup.ts created for this run.
 * Playwright passes them to the test workers through the environment.
 */
export function testCatalog(): TestCatalog {
  const value = process.env.E2E_TEST_CATALOG;
  if (!value) throw new Error('The test faculty was not created; global-setup.ts did not run.');
  return JSON.parse(value) as TestCatalog;
}
