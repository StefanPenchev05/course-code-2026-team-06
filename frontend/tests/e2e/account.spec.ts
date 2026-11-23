import { expect, test, type Page } from '@playwright/test';
import { newStudent, rejectRegistration, type Student } from './backend';
import { testCatalog, uniqueStudentNumber } from './env';
import { logIn, logOut } from './ui';

// Browser journeys for issue #28 against the REAL Go API, PostgreSQL and Redis.
// No request is mocked.

async function fillRegistration(page: Page, student: Student) {
  await page.getByLabel('Full name').fill(student.name);
  await page.getByLabel('University email').fill(student.email);
  await page.getByLabel('Student number').fill(student.studentNumber);
  await chooseProgram(page);
  await page.getByLabel('Password', { exact: true }).fill(student.password);
  await page.getByLabel('Repeat password').fill(student.password);
}

/** Picks this run's test faculty, waits for its programs to load from the API, then picks the program. */
async function chooseProgram(page: Page) {
  const { facultyName, programLabel } = testCatalog();
  await page.getByLabel('Faculty').selectOption({ label: facultyName });
  await expect(page.getByRole('option', { name: programLabel })).toBeAttached();
  await page.getByLabel('Study program').selectOption({ label: programLabel });
}

async function register(page: Page, student: Student) {
  await page.goto('/register');
  await fillRegistration(page, student);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByText('Your registration is waiting for approval')).toBeVisible();
}

test('a new student registers, stays logged in, logs out and logs back in', async ({ page }) => {
  const student = newStudent('register');
  await register(page, student);
  await expect(page.getByRole('heading', { name: `Welcome, ${student.name}` })).toBeVisible();

  // Student pages stay locked until a coordinator approves.
  await page.goto('/sessions');
  await expect(page.getByText('This becomes available once a coordinator approves your registration.')).toBeVisible();

  // A full reload keeps the session (HttpOnly cookie + GET /auth/me).
  await page.goto('/');
  await expect(page.getByText('Your registration is waiting for approval')).toBeVisible();

  await logOut(page);

  // Logged out: protected pages redirect to login, then back after logging in.
  await page.goto('/sessions');
  await expect(page).toHaveURL(/\/login$/);
  await logIn(page, student.email, student.password);
  await expect(page).toHaveURL(/\/sessions$/);
});

test('registration explains what the server refused and keeps the input', async ({ page }) => {
  const existing = newStudent('existing');
  await register(page, existing);
  await logOut(page);

  // An email outside the allowed university domains (real 422).
  const outsider = { ...newStudent('outsider'), email: `e2e-${Date.now()}@not-a-university.example` };
  await page.goto('/register');
  await fillRegistration(page, outsider);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByText('Use your university email address. Other email addresses can’t register.')).toBeVisible();
  await expect(page.getByLabel('Full name')).toHaveValue(outsider.name);

  // An email that already has an account (real 409).
  await page.getByLabel('University email').fill(existing.email);
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByRole('alert')).toContainText('An account with this email or student number already exists.');
  await expect(page.getByRole('link', { name: 'Log in instead' })).toBeVisible();
});

test('login rejects a wrong password with a clear message', async ({ page }) => {
  const student = newStudent('wrongpw');
  await register(page, student);
  await logOut(page);

  await logIn(page, student.email, 'not-the-right-password');
  await expect(page.getByRole('alert')).toHaveText('Email or password is incorrect.');
  await expect(page).toHaveURL(/\/login$/);
});

test('a rejected student sees the reason, corrects the details and resubmits', async ({ page }) => {
  const student = newStudent('rejected');
  await register(page, student);
  await logOut(page);

  const reason = 'E2E: the student number does not match our records.';
  await rejectRegistration(student.email, reason);

  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByText('Your registration was not approved')).toBeVisible();
  await expect(page.getByText(`Reason: ${reason}`)).toBeVisible();
  await expect(page.getByLabel('Full name')).toHaveValue(student.name);
  await expect(page.getByLabel('University email')).toHaveValue(student.email);

  await page.getByLabel('Student number').fill(uniqueStudentNumber());
  await chooseProgram(page);
  await page.getByRole('button', { name: 'Send for approval again' }).click();

  await expect(page.getByText('Your registration is waiting for approval')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Correct your details' })).toHaveCount(0);
});
