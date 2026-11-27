import { expect, request, test } from '@playwright/test';
import { suspendUser, verifiedStudent } from './backend';
import { E2E } from './env';
import { cuttableGateway, openAs, userIdOf, waitUntilLive } from './live';

// Issue #94: what an open browser does when the session behind it ends elsewhere, against the
// REAL Go API, Redis and /ws gateway. The gateway re-checks quiet sockets every 15 seconds
// (docs/realtime.md), so these journeys allow up to 45 seconds, without any fixed sleep.

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

test('logging out in one tab ends the session in the other tab without a click', async ({ browser }) => {
  test.setTimeout(120_000);
  const student = await verifiedStudent('live-tabs');
  const context = await browser.newContext();
  const first = await context.newPage();
  const second = await context.newPage();

  await openAs(first, student, '/sessions');
  await expect(first.getByRole('heading', { name: 'My sessions', level: 1 })).toBeVisible();
  // Same browser, so the second tab is logged in too and has its own live connection.
  await second.goto('/tutors');
  await expect(second.getByRole('heading', { name: 'Find a tutor', level: 1 })).toBeVisible();
  await Promise.all([waitUntilLive(first), waitUntilLive(second)]);

  await first.getByRole('button', { name: 'Log out' }).click();
  await expect(first.getByText("You're logged out of Tutti")).toBeVisible();

  // The other tab notices by itself and drops the protected page.
  await expect(second.getByText('Your session ended')).toBeVisible({ timeout: 45_000 });
  await expect(second.getByRole('heading', { name: 'Log in', level: 1 })).toBeVisible();
  await expect(second.getByRole('heading', { name: 'Find a tutor', level: 1 })).toHaveCount(0);

  await context.close();
});

test('a session the server forgets ends the open page by itself', async ({ page, context }) => {
  test.setTimeout(120_000);
  const student = await verifiedStudent('live-forgotten');
  await openAs(page, student, '/sessions');
  await expect(page.getByRole('heading', { name: 'My sessions', level: 1 })).toBeVisible();
  await waitUntilLive(page);

  // What an expired or revoked session looks like: the server forgets it while the browser
  // keeps its cookie. (Real POST /auth/logout with the same cookie, outside the browser.)
  const cookie = (await context.cookies()).find((c) => c.name === 'tutti_session');
  expect(cookie, 'the browser has a session cookie').toBeDefined();
  const outside = await request.newContext({
    baseURL: E2E.baseURL,
    extraHTTPHeaders: { Origin: E2E.baseURL, Cookie: `tutti_session=${cookie!.value}` },
  });
  expect((await outside.post('/api/v1/auth/logout')).status()).toBe(204);
  await outside.dispose();

  await expect(page.getByText('Your session ended')).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole('heading', { name: 'My sessions', level: 1 })).toHaveCount(0);
});

test('a suspension made while the browser was disconnected is not kept after reconnecting', async ({ page }) => {
  test.setTimeout(120_000);
  const student = await verifiedStudent('live-suspended');
  const studentId = await userIdOf(student);
  const gateway = await cuttableGateway(page);

  await openAs(page, student, '/sessions');
  await expect(page.getByRole('heading', { name: 'My sessions', level: 1 })).toBeVisible();
  await waitUntilLive(page);

  // Offline (live connection refused) while the coordinator suspends the account.
  await gateway.cut();
  await suspendUser(studentId, 'E2E: suspended while the browser was offline.');
  gateway.restore();

  // The app re-checks the session on its own: the protected page is gone, not kept.
  await expect(page.getByText("This account can't be used right now")).toBeVisible({ timeout: 45_000 });
  await expect(page.getByRole('heading', { name: 'My sessions', level: 1 })).toHaveCount(0);

  // And it stays gone after a reload.
  await page.goto('/sessions');
  await expect(page.getByRole('heading', { name: 'Log in', level: 1 })).toBeVisible();
});
