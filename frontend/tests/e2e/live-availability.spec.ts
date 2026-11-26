import { expect, test, type Page } from '@playwright/test';
import { approvedTutor, bookWindow, loggedIn, verifiedStudent } from './backend';
import { logIn } from './ui';

// Issue #92: a student watching a tutor's profile and the tutor editing availability, in two
// separate browsers, against the REAL Go API, Redis and the /ws gateway (with real availability
// watches). Nothing is reloaded.

const TIME_ZONE = 'Europe/Luxembourg';
test.use({ locale: 'en-US', timezoneId: TIME_ZONE });

const DAY_MS = 24 * 60 * 60 * 1000;

async function markPage(page: Page) {
  await page.evaluate(() => {
    (window as unknown as { __noReload: boolean }).__noReload = true;
  });
}
const notReloaded = (page: Page) => page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload === true);

/** "YYYY-MM-DD" of an instant in Luxembourg. */
const localDate = (instant: Date) =>
  new Intl.DateTimeFormat('en-CA', { timeZone: TIME_ZONE, year: 'numeric', month: '2-digit', day: '2-digit' }).format(instant);

test('free times change live for a watching student while the tutor edits and others book', async ({ browser }) => {
  test.setTimeout(120_000);
  const tutor = await approvedTutor('watched-tutor', 'Watched tutor.', 18);
  const student = await verifiedStudent('watcher');
  const other = await verifiedStudent('other-booker');
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const tutorId = ((await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string }).id;

  const studentContext = await browser.newContext();
  const tutorContext = await browser.newContext();
  const studentPage = await studentContext.newPage();
  const tutorPage = await tutorContext.newPage();

  // The student opens the tutor's profile: no free times yet.
  await studentPage.goto('/login');
  await logIn(studentPage, student.email, student.password);
  await expect(studentPage.getByRole('button', { name: 'Log out' })).toBeVisible();
  await studentPage.goto(`/tutors/${tutorId}`);
  await expect(studentPage.getByText('No free time in these two weeks.')).toBeVisible();
  await markPage(studentPage);

  // 1. The tutor adds a one-off time in their browser: it appears for the student by itself.
  await tutorPage.goto('/login');
  await logIn(tutorPage, tutor.email, tutor.password);
  await expect(tutorPage.getByRole('button', { name: 'Log out' })).toBeVisible();
  await tutorPage.goto('/tutor/availability');
  await markPage(tutorPage);
  const day = localDate(new Date(Date.now() + 3 * DAY_MS));
  const oneOff = tutorPage.getByRole('region', { name: 'Add a one-off free time' });
  await oneOff.getByLabel('Date', { exact: true }).fill(day);
  await oneOff.getByLabel('From', { exact: true }).fill('10:00');
  await oneOff.getByLabel('Until', { exact: true }).fill('11:00');
  await oneOff.getByLabel('Where', { exact: true }).selectOption('ONLINE');
  await oneOff.getByRole('button', { name: 'Add free time' }).click();
  await expect(oneOff.getByText('Added. Students can now book this time.')).toBeVisible();

  const bookButton = studentPage.getByRole('button', { name: /^Book .*10:00 AM–11:00 AM/ });
  await expect(bookButton).toBeVisible({ timeout: 20_000 });

  // 2. The student opens the booking form; another student books that time first.
  await bookButton.click();
  const form = studentPage.getByRole('region', { name: /^Book / });
  await form.getByLabel('What do you need help with?').fill('Typed before it was taken.');
  const own = (await (await tutorApi.get('/api/v1/tutors/me/availability')).json()) as { data: { id: string; starts_at: string }[] };
  const sessionId = await bookWindow(other, tutorId, own.data[0].id, own.data[0].starts_at, 'Booked first.');

  // The open form says the time is no longer free (not who took it), keeps the text, can't be sent.
  await expect(form.getByText('This time is no longer free')).toBeVisible({ timeout: 20_000 });
  await expect(form.getByRole('button', { name: 'Send request' })).toBeDisabled();
  await expect(form.getByLabel('What do you need help with?')).toHaveValue('Typed before it was taken.');
  await expect(bookButton).toHaveCount(0);

  // 3. The other student cancels: the reservation is released and the time is free again.
  const otherApi = await loggedIn(other.email, other.password);
  expect((await otherApi.post(`/api/v1/sessions/${sessionId}/cancel`, { data: { reason: 'Changed plans.' } })).status()).toBe(200);
  await otherApi.dispose();
  await expect(bookButton).toBeVisible({ timeout: 20_000 });

  // 4. The tutor removes the time in their browser: it disappears for the student.
  const window = tutorPage.getByRole('listitem').filter({ hasText: 'Online · One-off' });
  await window.getByRole('button', { name: /^Remove/ }).click();
  await window.getByRole('button', { name: 'Yes, remove' }).click();
  await expect(tutorPage.getByText('Removed.')).toBeVisible();
  await expect(bookButton).toHaveCount(0, { timeout: 20_000 });
  await expect(studentPage.getByText('No free time in these two weeks.')).toBeVisible();

  // The tutor's own confirmation survived the live echo of their change; nobody reloaded.
  await tutorPage.waitForTimeout(1500);
  await expect(tutorPage.getByText('Removed.')).toBeVisible();
  expect(await notReloaded(studentPage)).toBe(true);
  expect(await notReloaded(tutorPage)).toBe(true);

  await tutorApi.dispose();
  await studentContext.close();
  await tutorContext.close();
});
