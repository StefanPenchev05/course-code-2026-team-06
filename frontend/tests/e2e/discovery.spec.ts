import { expect, test } from '@playwright/test';
import { approvedTutorWithFreeTime, loggedIn, verifiedStudent } from './backend';
import { testCatalog } from './env';
import { logIn } from './ui';

// Browser journey for issue #29 (find a tutor) against the REAL Go API,
// PostgreSQL and Redis. No request is mocked.

const TIME_ZONE = 'Europe/Luxembourg';
// Fixed locale and zone so the expected time text is the same on every machine.
test.use({ locale: 'en-US', timezoneId: TIME_ZONE });

const PRIVATE_GRADE = 19.75;

/** A 90-minute window two days from now, on a full hour. */
function freeWindow() {
  const startsAt = new Date(Date.now() + 2 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  return { startsAt, endsAt: new Date(startsAt.getTime() + 90 * 60 * 1000) };
}

test('a student finds an approved tutor for a subject and sees the profile with free times', async ({ page }) => {
  const { subjectCode, subjectName, subjectId } = testCatalog();
  const window = freeWindow();
  const description = 'I explain this subject step by step.';
  const tutor = await approvedTutorWithFreeTime('tutor', description, PRIVATE_GRADE, window);

  // A second student applied for the same subject but was NOT approved: they must not appear.
  const applicant = await verifiedStudent('applicant');
  const applicantApi = await loggedIn(applicant.email, applicant.password);
  const applied = await applicantApi.post('/api/v1/tutor-applications', {
    data: { subject_id: subjectId, grade: 18, description: 'Not approved yet.' },
  });
  expect(applied.status()).toBe(201);
  await applicantApi.dispose();

  const learner = await verifiedStudent('learner');

  await page.goto('/login');
  await logIn(page, learner.email, learner.password);
  await page.getByRole('link', { name: 'Find a tutor' }).first().click();
  await expect(page.getByRole('heading', { name: 'Find a tutor' })).toBeVisible();

  // Subject filter: the student's own program subjects are listed without typing (program_id from
  // /auth/me), and typing the code filters the list.
  const ownSubjects = page.getByRole('list', { name: 'Subjects in your program' });
  await expect(ownSubjects.getByRole('button', { name: new RegExp(subjectName) })).toBeVisible();
  await page.getByLabel('Subject', { exact: true }).fill(subjectCode);
  await expect(ownSubjects.getByRole('button')).toHaveCount(1);
  await ownSubjects.getByRole('button', { name: new RegExp(subjectName) }).click();

  // Only the approved tutor is listed, with the matching offering and aggregate rating.
  const card = page.getByRole('listitem').filter({ has: page.getByRole('link', { name: tutor.name }) });
  await expect(card).toContainText(`${subjectName} · No ratings yet`);
  await expect(card).toContainText(description);
  // (Other journeys may add approved tutors for the same subject, so the total isn't checked.)
  await expect(page.getByRole('link', { name: applicant.name })).toHaveCount(0);

  // Academic filter: the tutor's degree level (the test program is a bachelor).
  await page.getByLabel("Tutor's degree").selectOption('MASTER');
  await expect(page.getByText('No tutors found')).toBeVisible();
  await page.getByLabel("Tutor's degree").selectOption('BACHELOR');
  await expect(page.getByRole('link', { name: tutor.name })).toBeVisible();

  // Profile: offering, rating count and the published free time in the student's time zone.
  await page.getByRole('link', { name: tutor.name }).click();
  await expect(page.getByRole('heading', { name: tutor.name, level: 1 })).toBeVisible();
  await expect(page.getByText('No ratings yet')).toBeVisible();
  await expect(page.getByRole('heading', { name: subjectName })).toBeVisible();

  const time = new Intl.DateTimeFormat('en-US', { timeZone: TIME_ZONE, hour: '2-digit', minute: '2-digit' });
  const day = new Intl.DateTimeFormat('en-US', { timeZone: TIME_ZONE, weekday: 'long', day: 'numeric', month: 'long' });
  await expect(page.getByRole('heading', { name: day.format(window.startsAt) })).toBeVisible();
  await expect(page.getByText(`${time.format(window.startsAt)}–${time.format(window.endsAt)}`)).toBeVisible();
  await expect(page.getByText('Online', { exact: true })).toBeVisible();

  // The private grade from the application never reaches the page.
  await expect(page.locator('body')).not.toContainText(String(PRIVATE_GRADE));
  await expect(page.locator('body')).not.toContainText(/grade/i);
});
