import { expect, test } from '@playwright/test';
import { suspendUser, verifiedStudent } from './backend';
import { E2E } from './env';
import { logIn } from './ui';

// Browser journeys for issue #28 (session expiry and account blocking) against
// the REAL Go API, PostgreSQL and Redis. No request is mocked.

test('an expired session is explained, and logging in again returns to the same page', async ({ page, context }) => {
  const student = await verifiedStudent('expiry');
  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByRole('heading', { name: `Welcome, ${student.name}` })).toBeVisible();

  // The server forgets the session while the browser keeps its cookie: exactly what a timed-out
  // session looks like. (Logging out through the API revokes the Redis session; the cookie is
  // then put back.)
  const cookies = await context.cookies();
  const revoked = await page.request.post('/api/v1/auth/logout', { headers: { Origin: E2E.baseURL } });
  expect(revoked.status()).toBe(204);
  await context.addCookies(cookies);

  // The next request the app makes gets a real 401.
  await page.getByRole('link', { name: 'Find a tutor' }).first().click();
  await expect(page.getByText('Your session ended')).toBeVisible();
  await expect(page).toHaveURL(/\/login$/);

  await logIn(page, student.email, student.password);
  await expect(page).toHaveURL(/\/tutors$/);
  await expect(page.getByText('Your session ended')).toHaveCount(0);
});

test('a student suspended while logged in is told why the app stopped working', async ({ page }) => {
  const student = await verifiedStudent('suspended');
  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByRole('heading', { name: `Welcome, ${student.name}` })).toBeVisible();

  const me = (await (await page.request.get('/api/v1/auth/me')).json()) as { id: string };
  await suspendUser(me.id, 'E2E: suspended to test the session notice.');

  // The next request the app makes gets a real 403; the app re-checks /auth/me (also 403).
  await page.getByRole('link', { name: 'Find a tutor' }).first().click();
  await expect(page.getByText("This account can't be used right now")).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeVisible();

  // Logging in again is refused with the matching message.
  await logIn(page, student.email, student.password);
  await expect(page.getByRole('alert')).toContainText("This account can't log in right now");
});
