import { expect, test, type Page } from '@playwright/test';
import { approvedTutorWithFreeTime, bookWindow, loggedIn, verifiedStudent } from './backend';
import { logIn } from './ui';

// Issue #91: a tutor and a student in two separate browsers, against the REAL Go API, Redis
// and the /ws gateway. Every change shows up in the other browser without a reload.

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

async function markPage(page: Page) {
  await page.evaluate(() => {
    (window as unknown as { __noReload: boolean }).__noReload = true;
  });
}

const notReloaded = (page: Page) => page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload === true);

test('booking, accepting and cancelling show up live in the other person’s browser', async ({ browser }) => {
  test.setTimeout(120_000);
  const HOUR = 60 * 60 * 1000;
  const startsAt = new Date(Date.now() + 3 * 24 * HOUR);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime('live-tutor', 'Live tutor.', 18, { startsAt, endsAt: new Date(startsAt.getTime() + 4 * HOUR) });
  const student = await verifiedStudent('live-student');
  const other = await verifiedStudent('live-other');
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const tutorId = ((await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string }).id;
  const slotId = ((await (await tutorApi.get('/api/v1/tutors/me/availability')).json()) as { data: { id: string }[] }).data[0].id;
  await tutorApi.dispose();

  const tutorContext = await browser.newContext();
  const studentContext = await browser.newContext();
  const tutorPage = await tutorContext.newPage();
  const studentPage = await studentContext.newPage();

  // The tutor has the Teaching page open.
  await tutorPage.goto('/login');
  await logIn(tutorPage, tutor.email, tutor.password);
  await expect(tutorPage.getByRole('button', { name: 'Log out' })).toBeVisible();
  await tutorPage.goto('/tutor');
  await expect(tutorPage.getByRole('heading', { name: 'Requests waiting for you (0)' })).toBeVisible();
  await markPage(tutorPage);

  // 1. The student books in their own browser; the request appears for the tutor by itself.
  await studentPage.goto('/login');
  await logIn(studentPage, student.email, student.password);
  await expect(studentPage.getByRole('button', { name: 'Log out' })).toBeVisible();
  await studentPage.goto(`/tutors/${tutorId}`);
  await studentPage.getByRole('button', { name: /^Book / }).first().click();
  const form = studentPage.getByRole('region', { name: /^Book / });
  await form.getByLabel('What do you need help with?').fill('Live: first request.');
  await form.getByRole('button', { name: 'Send request' }).click();
  await expect(studentPage.getByText('Request sent')).toBeVisible();

  await expect(tutorPage.getByRole('heading', { name: 'Requests waiting for you (1)' })).toBeVisible({ timeout: 20_000 });
  await expect(tutorPage.getByRole('status').filter({ hasText: /^New: / })).toBeVisible();

  // 2. The student watches My sessions; the tutor accepts; the student's card updates by itself.
  await studentPage.getByRole('link', { name: 'My sessions' }).first().click();
  const studentCard = studentPage.getByRole('listitem').filter({ hasText: 'Live: first request.' });
  await expect(studentCard.getByText('Waiting for the tutor')).toBeVisible();
  await markPage(studentPage);

  const request = tutorPage.getByRole('listitem').filter({ hasText: 'Live: first request.' });
  await request.getByRole('button', { name: /^Accept / }).click();
  await request.getByLabel('Meeting link').fill('https://teams.example/meet/live');
  await request.getByRole('button', { name: 'Accept the session' }).click();

  await expect(studentCard.getByText('Confirmed', { exact: true })).toBeVisible({ timeout: 20_000 });
  await expect(studentCard.getByRole('link', { name: 'https://teams.example/meet/live' })).toBeVisible();
  await expect(studentPage.getByRole('status').filter({ hasText: /now Confirmed/ })).toBeVisible();

  // 3. A second request; the tutor starts writing a decline reason ...
  const secondId = await bookWindow(student, tutorId, slotId, new Date(startsAt.getTime() + HOUR).toISOString(), 'Live: second request.');
  const second = tutorPage.getByRole('listitem').filter({ hasText: 'Live: second request.' });
  await expect(second).toBeVisible({ timeout: 20_000 });
  await second.getByRole('button', { name: /^Decline / }).click();
  await second.getByLabel('Reason for declining').fill('Half-written reason');

  // ... another student's request arrives meanwhile: the typed reason is kept.
  await bookWindow(other, tutorId, slotId, new Date(startsAt.getTime() + 2 * HOUR).toISOString(), 'Live: third request.');
  await expect(tutorPage.getByRole('heading', { name: 'Requests waiting for you (2)' })).toBeVisible({ timeout: 20_000 });
  await expect(second.getByLabel('Reason for declining')).toHaveValue('Half-written reason');

  // The student cancels the second request in their browser: it leaves the tutor's list, with the reason why.
  const secondCard = studentPage.getByRole('listitem').filter({ hasText: 'Live: second request.' });
  await expect(secondCard).toBeVisible({ timeout: 20_000 });
  await secondCard.getByRole('button', { name: /^Cancel session/ }).click();
  await secondCard.getByLabel('Reason for cancelling').fill('Found another time.');
  await secondCard.getByRole('button', { name: 'Cancel the session' }).click();

  await expect(tutorPage.getByRole('listitem').filter({ hasText: 'Live: second request.' })).toHaveCount(0, { timeout: 20_000 });
  await expect(tutorPage.getByRole('status').filter({ hasText: /now Cancelled/ })).toBeVisible();
  await expect(tutorPage.getByRole('heading', { name: 'Requests waiting for you (1)' })).toBeVisible();

  // Nobody reloaded anything.
  expect(await notReloaded(tutorPage)).toBe(true);
  expect(await notReloaded(studentPage)).toBe(true);
  expect(secondId).toBeTruthy();

  await tutorContext.close();
  await studentContext.close();
});
