import { expect, test } from '@playwright/test';
import { bookWindow, verifiedStudent } from './backend';
import { E2E } from './env';
import { staleRequest } from './history-fixture';
import { cuttableGateway, markPage, notReloaded, openAs, recordEvents, tutorWithFreeTime, waitUntilLive } from './live';

// Issue #94: session requests between separate browsers against the REAL Go API, worker,
// PostgreSQL, Redis and /ws gateway. Creation, acceptance and cancellation are in
// live-booking.spec.ts (#91) and availability editing in live-availability.spec.ts (#92);
// this file adds decline, expiry, a lost connection and changes during navigation.

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

const waiting = (count: number) => ({ name: `Requests waiting for you (${count})` });

test('a declined request reaches the student live, and an unrelated student receives nothing about it', async ({ browser }) => {
  test.setTimeout(150_000);
  const { tutor, tutorId, slotId, startsAt } = await tutorWithFreeTime('live-decline');
  const student = await verifiedStudent('live-decline-student');
  const bystander = await verifiedStudent('live-bystander');

  const tutorContext = await browser.newContext();
  const studentContext = await browser.newContext();
  const bystanderContext = await browser.newContext();
  const tutorPage = await tutorContext.newPage();
  const studentPage = await studentContext.newPage();
  const bystanderPage = await bystanderContext.newPage();
  const bystanderFrames = recordEvents(bystanderPage);

  await openAs(tutorPage, tutor, '/tutor');
  await expect(tutorPage.getByRole('heading', waiting(0))).toBeVisible();
  await openAs(studentPage, student, '/sessions');
  await expect(studentPage.getByRole('heading', { name: 'My sessions', level: 1 })).toBeVisible();
  await openAs(bystanderPage, bystander, '/sessions');
  await Promise.all([waitUntilLive(tutorPage), waitUntilLive(studentPage), waitUntilLive(bystanderPage)]);
  await markPage(tutorPage);
  await markPage(studentPage);

  const sessionId = await bookWindow(student, tutorId, slotId, startsAt.toISOString(), 'Live: please decline me.');
  const request = tutorPage.getByRole('listitem').filter({ hasText: 'Live: please decline me.' });
  const card = studentPage.getByRole('listitem').filter({ hasText: 'Live: please decline me.' });
  await expect(request).toBeVisible({ timeout: 20_000 });
  await expect(card).toBeVisible({ timeout: 20_000 });

  await request.getByRole('button', { name: /^Decline / }).click();
  await request.getByLabel('Reason for declining').fill('I am away that week.');
  await request.getByRole('button', { name: 'Decline the session' }).click();
  await expect(tutorPage.getByText('Declined. The student was notified with your reason.')).toBeVisible();

  // The student's active list drops it by itself and says why.
  await expect(card).toHaveCount(0, { timeout: 20_000 });
  await expect(studentPage.getByRole('status').filter({ hasText: /now Declined/ })).toBeVisible();
  expect(await notReloaded(studentPage)).toBe(true);
  expect(await notReloaded(tutorPage)).toBe(true);

  // The other student's open browser never received an event about this session.
  expect(bystanderFrames.some((frame) => frame.includes(sessionId))).toBe(false);

  await tutorContext.close();
  await studentContext.close();
  await bystanderContext.close();
});

test('an unanswered request expires live in the tutor’s and the student’s browser', async ({ browser }) => {
  test.skip(!E2E.worker, 'Needs the Tutti worker on the same database (E2E_WORKER=1, see tests/e2e/README.md).');
  test.setTimeout(180_000);
  const { tutor, tutorId, slotId, startsAt } = await tutorWithFreeTime('live-expiry');
  const student = await verifiedStudent('live-expiry-student');
  const answered = await bookWindow(student, tutorId, slotId, startsAt.toISOString(), 'Live: still waiting in time.');

  const tutorContext = await browser.newContext();
  const studentContext = await browser.newContext();
  const tutorPage = await tutorContext.newPage();
  const studentPage = await studentContext.newPage();
  await openAs(tutorPage, tutor, '/tutor');
  await expect(tutorPage.getByRole('heading', waiting(1))).toBeVisible();
  await openAs(studentPage, student, '/sessions');
  await expect(studentPage.getByRole('listitem').filter({ hasText: 'Live: still waiting in time.' })).toBeVisible();
  await Promise.all([waitUntilLive(tutorPage), waitUntilLive(studentPage)]);
  await markPage(tutorPage);
  await markPage(studentPage);

  // A request whose real answer deadline passes 30 seconds from now (see history-fixture.ts).
  staleRequest(answered, 'Live: nobody answers this.', 30);
  const request = tutorPage.getByRole('listitem').filter({ hasText: 'Live: nobody answers this.' });
  const card = studentPage.getByRole('listitem').filter({ hasText: 'Live: nobody answers this.' });
  await expect(request).toBeVisible({ timeout: 25_000 });
  await expect(card).toBeVisible({ timeout: 25_000 });

  // The real worker expires it after the deadline; both browsers drop it by themselves.
  await expect(request).toHaveCount(0, { timeout: 90_000 });
  await expect(card).toHaveCount(0, { timeout: 30_000 });
  await expect(tutorPage.getByRole('status').filter({ hasText: /now Expired/ })).toBeVisible();
  await expect(studentPage.getByRole('status').filter({ hasText: /now Expired/ })).toBeVisible();
  // The request still inside its deadline is untouched.
  await expect(tutorPage.getByRole('listitem').filter({ hasText: 'Live: still waiting in time.' })).toBeVisible();
  expect(await notReloaded(tutorPage)).toBe(true);
  expect(await notReloaded(studentPage)).toBe(true);

  await tutorContext.close();
  await studentContext.close();
});

test('after a lost connection the Teaching page catches up once, without duplicate rows or announcements', async ({ page }) => {
  test.setTimeout(150_000);
  const { tutor, tutorId, slotId, startsAt } = await tutorWithFreeTime('live-resync');
  const student = await verifiedStudent('live-resync-student');
  const gateway = await cuttableGateway(page);

  await openAs(page, tutor, '/tutor');
  await expect(page.getByRole('heading', waiting(0))).toBeVisible();
  await waitUntilLive(page);
  await markPage(page);

  // The connection drops and reconnects are refused, like a network outage.
  await gateway.cut();
  await expect(page.getByRole('status').filter({ hasText: 'Live updates are paused' })).toBeVisible({ timeout: 15_000 });

  // Missed while disconnected: the server never replays this event.
  await bookWindow(student, tutorId, slotId, startsAt.toISOString(), 'Live: sent while offline.');

  // Network back: the app reconnects by itself and refetches over HTTP.
  gateway.restore();
  const rows = page.getByRole('listitem').filter({ hasText: 'Live: sent while offline.' });
  await expect(rows).toHaveCount(1, { timeout: 45_000 });
  await expect(page.getByRole('heading', waiting(1))).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Live updates are paused' })).toHaveCount(0);
  await waitUntilLive(page);

  // One announcement for the one new request, not one per reconnect attempt.
  const announced = (await page.getByRole('status').filter({ hasText: /^New: / }).textContent()) ?? '';
  expect(announced.match(/New: /g) ?? []).toHaveLength(1);
  expect(await notReloaded(page)).toBe(true);
});

test('a request sent while the tutor is moving between pages shows up exactly once', async ({ page }) => {
  test.setTimeout(120_000);
  const { tutor, tutorId, slotId, startsAt, hour } = await tutorWithFreeTime('live-navigation');
  const student = await verifiedStudent('live-navigation-student');

  await openAs(page, tutor, '/tutor');
  await expect(page.getByRole('heading', waiting(0))).toBeVisible();
  await waitUntilLive(page);
  await markPage(page);

  // The booking happens while the route changes to My availability.
  await Promise.all([
    page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'My availability' }).click(),
    bookWindow(student, tutorId, slotId, startsAt.toISOString(), 'Live: sent during navigation.'),
  ]);
  await expect(page.getByRole('heading', { name: 'My availability', level: 1 })).toBeVisible();

  // Back on Teaching: the request is there once.
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Teaching' }).click();
  await expect(page.getByRole('listitem').filter({ hasText: 'Live: sent during navigation.' })).toHaveCount(1, { timeout: 20_000 });
  await expect(page.getByRole('heading', waiting(1))).toBeVisible();

  // The next request arrives live on the page just opened, still without duplicates.
  await bookWindow(student, tutorId, slotId, hour(1), 'Live: sent after navigation.');
  await expect(page.getByRole('listitem').filter({ hasText: 'Live: sent after navigation.' })).toHaveCount(1, { timeout: 20_000 });
  await expect(page.getByRole('listitem').filter({ hasText: 'Live: sent during navigation.' })).toHaveCount(1);
  await expect(page.getByRole('heading', waiting(2))).toBeVisible();
  expect(await notReloaded(page)).toBe(true);
});
