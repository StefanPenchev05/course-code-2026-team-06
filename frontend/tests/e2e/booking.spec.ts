import { expect, test, type Page } from '@playwright/test';
import { approvedTutorWithFreeTime, loggedIn, verifiedStudent, type Student } from './backend';
import { testCatalog } from './env';
import { logIn } from './ui';

// Browser journey for issue #32 (booking, my sessions, cancelling) against the
// REAL Go API, PostgreSQL and Redis. No request is mocked.

const TIME_ZONE = 'Europe/Luxembourg';
test.use({ locale: 'en-US', timezoneId: TIME_ZONE });

/** Two tutors free at the same 3 hours, three days from now (more than 25 hours of notice). */
function sharedWindow() {
  const startsAt = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  return { startsAt, endsAt: new Date(startsAt.getTime() + 3 * 60 * 60 * 1000) };
}

async function openProfileFromSearch(page: Page, tutor: Student) {
  const { subjectName } = testCatalog();
  await page.getByRole('link', { name: 'Find a tutor' }).first().click();
  await page.getByRole('list', { name: 'Subjects in your program' }).getByRole('button', { name: new RegExp(subjectName) }).click();
  await page.getByRole('link', { name: tutor.name }).click();
  await expect(page.getByRole('heading', { name: tutor.name, level: 1 })).toBeVisible();
}

/** The three booking steps on a tutor profile: choose the first free time, fill in, send. */
async function bookFirstFreeTime(page: Page, description: string) {
  await page.getByRole('button', { name: /^Book / }).first().click(); // 1
  const form = page.getByRole('region', { name: /^Book / });
  await form.getByLabel('Purpose').selectOption('EXAM_PREPARATION'); // 2
  await form.getByLabel('Length').selectOption('60');
  await form.getByLabel('What do you need help with?').fill(description);
  await form.getByRole('button', { name: 'Send request' }).click(); // 3
  return form;
}

test('a student books a tutor in three steps, sees it in My sessions, hits an overlap and cancels', async ({ page }) => {
  const window = sharedWindow();
  const tutor = await approvedTutorWithFreeTime('booked-tutor', 'Exam training.', 18, window);
  const otherTutor = await approvedTutorWithFreeTime('busy-tutor', 'Also free then.', 17, window);
  const learner = await verifiedStudent('learner');

  await page.goto('/login');
  await logIn(page, learner.email, learner.password);

  // Book: three steps after opening the profile.
  await openProfileFromSearch(page, tutor);
  await bookFirstFreeTime(page, 'Please help me with past exam questions.');
  await expect(page.getByText('Request sent')).toBeVisible();

  // My sessions: status and role.
  await page.getByRole('link', { name: 'My sessions' }).first().click();
  const card = page.getByRole('listitem').filter({ hasText: 'Please help me with past exam questions.' });
  await expect(card.getByText('Waiting for the tutor')).toBeVisible();
  await expect(card.getByText('You are the student')).toBeVisible();
  await expect(card.getByText(/60 min · Online or on site|60 min · Online/)).toBeVisible();

  // The tutor sees the same session with the tutor role (real API).
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const tutorView = (await (await tutorApi.get('/api/v1/sessions/me')).json()) as { data: { viewer_role: string; status: string }[] };
  expect(tutorView.data).toEqual([expect.objectContaining({ viewer_role: 'TUTOR', status: 'REQUESTED' })]);
  await tutorApi.dispose();

  // Booking another tutor at the same time is refused by the database's overlap rule (real 409).
  await openProfileFromSearch(page, otherTutor);
  const form = await bookFirstFreeTime(page, 'Same time, different tutor.');
  await expect(form.getByRole('alert')).toContainText('You or the tutor already have a session at this time.');

  // Cancel: a reason is required.
  await page.getByRole('link', { name: 'My sessions' }).first().click();
  await card.getByRole('button', { name: /^Cancel / }).click();
  await expect(card.getByText('This is a late cancellation')).toHaveCount(0); // three days ahead: not late
  await card.getByRole('button', { name: 'Cancel the session' }).click();
  await expect(card.getByText('Give a reason; the other person sees it.')).toBeVisible();
  await card.getByLabel('Reason for cancelling').fill('My exam was moved.');
  await card.getByRole('button', { name: 'Cancel the session' }).click();
  await expect(card).toHaveCount(0); // leaves the "requested and confirmed" list

  await page.getByRole('button', { name: 'All sessions' }).click();
  const cancelled = page.getByRole('listitem').filter({ hasText: 'Please help me with past exam questions.' });
  await expect(cancelled.getByText('Cancelled', { exact: true })).toBeVisible();
  await expect(cancelled.getByText('You cancelled. Reason: My exam was moved.')).toBeVisible();
});
