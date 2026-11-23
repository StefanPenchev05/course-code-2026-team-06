import { execFileSync } from 'node:child_process';
import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { request, type APIRequestContext } from '@playwright/test';
import { E2E, newPassword, type TestCatalog } from './env';

/**
 * Runs once before the journeys:
 * 1. Checks that the real Go API answers through Vite and accepts this origin.
 * 2. Creates a faculty, a program with one subject, a semester and that
 *    faculty's coordinator just for this run. The schema allows one coordinator per faculty, so a fresh
 *    faculty keeps runs repeatable and never touches real faculties.
 *    Catalog data and coordinators are university-provisioned (there is no
 *    API to create them), so like the backend's integration tests
 *    (backend/tests/integration/identity_test.go) the rows are inserted
 *    directly; the coordinator is then activated through the real
 *    POST /auth/activate-coordinator endpoint.
 */
export default async function globalSetup() {
  const api = await request.newContext({ baseURL: E2E.baseURL, extraHTTPHeaders: { Origin: E2E.baseURL } });
  try {
    await checkBackend(api);
    process.env.E2E_TEST_CATALOG = JSON.stringify(await createTestCatalog(api));
  } finally {
    await api.dispose();
  }
}

async function checkBackend(api: APIRequestContext) {
  const me = await api.get('/api/v1/auth/me').catch(() => null);
  if (!me || me.status() !== 401) {
    throw new Error(
      `The Go API is not reachable through ${E2E.baseURL}/api (got ${me ? me.status() : 'no response'}). ` +
        'Start it as described in frontend/tests/e2e/README.md.',
    );
  }
  const login = await api.post('/api/v1/auth/login', { data: { email: `nobody@${E2E.emailDomain}`, password: 'x' } });
  if (login.status() === 403) {
    throw new Error(`The API rejects the origin ${E2E.baseURL}. Start it with PUBLIC_ORIGIN=${E2E.baseURL}.`);
  }
}

async function createTestCatalog(api: APIRequestContext): Promise<TestCatalog> {
  if (!E2E.databaseURL) {
    throw new Error('Set E2E_DATABASE_URL to the (disposable) database the API uses, so test data can be provisioned.');
  }
  const suffix = randomBytes(4).toString('hex');
  const facultyId = randomUUID();
  const programId = randomUUID();
  const coordinatorId = randomUUID();
  const facultyName = `E2E Faculty ${suffix}`;
  const programName = `E2E Program ${suffix}`;
  const subjectId = randomUUID();
  const semesterId = randomUUID();
  const subjectCode = `E2E-${suffix}`;
  const subjectName = `E2E Subject ${suffix}`;
  const coordinatorEmail = `e2e-coordinator-${suffix}@${E2E.emailDomain}`;
  const coordinatorPassword = newPassword();
  // Same token format and storage as Identity's IssueActivation: 32 random
  // bytes, base64url without padding; the database keeps only its SHA-256.
  const token = randomBytes(32).toString('base64url');
  const digest = createHash('sha256').update(token).digest('hex');

  // Every value is generated above (UUIDs, hex, fixed text), so inlining them is safe.
  runSql(
    [
      'BEGIN;',
      `INSERT INTO faculties (id, name) VALUES ('${facultyId}', '${facultyName}');`,
      `INSERT INTO programs (id, faculty_id, name, level) VALUES ('${programId}', '${facultyId}', '${programName}', 'BACHELOR');`,
      `INSERT INTO subjects (id, code, name) VALUES ('${subjectId}', '${subjectCode}', '${subjectName}');`,
      `INSERT INTO program_subjects (program_id, subject_id) VALUES ('${programId}', '${subjectId}');`,
      // Free times must lie inside a configured semester (docs/scheduling.md).
      `INSERT INTO semesters (id, name, starts_at, ends_at) VALUES ('${semesterId}', 'E2E Semester ${suffix}', now() - interval '1 day', now() + interval '120 days');`,
      `INSERT INTO users (id, name, email) VALUES ('${coordinatorId}', 'E2E Coordinator', '${coordinatorEmail}');`,
      `INSERT INTO coordinators (user_id, employee_number, faculty_id) VALUES ('${coordinatorId}', 'E2E-${suffix}', '${facultyId}');`,
      `INSERT INTO account_activation_tokens (coordinator_id, token_hash, expires_at) VALUES ('${coordinatorId}', decode('${digest}', 'hex'), now() + interval '1 hour');`,
      'COMMIT;',
    ].join('\n'),
  );

  const activated = await api.post('/api/v1/auth/activate-coordinator', { data: { token, password: coordinatorPassword } });
  if (activated.status() !== 204) {
    throw new Error(`Activating the test coordinator failed with HTTP ${activated.status()}.`);
  }
  return {
    facultyId,
    facultyName,
    programId,
    programLabel: `${programName} (Bachelor)`,
    subjectId,
    subjectCode,
    subjectName,
    semesterId,
    coordinatorEmail,
    coordinatorPassword,
  };
}

function runSql(sql: string) {
  try {
    execFileSync('psql', [E2E.databaseURL, '-v', 'ON_ERROR_STOP=1', '-q', '-c', sql], { stdio: ['ignore', 'ignore', 'pipe'] });
  } catch (error) {
    const stderr = (error as { stderr?: Buffer }).stderr?.toString().trim();
    throw new Error(`Could not provision test data with psql: ${stderr || String(error)}`, { cause: error });
  }
}
