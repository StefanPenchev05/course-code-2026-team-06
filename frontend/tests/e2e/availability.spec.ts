import { expect, test } from '@playwright/test';
import { approvedTutor, bookWindow, loggedIn, verifiedStudent } from './backend';
import { testCatalog } from './env';
import { logIn } from './ui';

// Browser journey for issue #31 (tutor availability) against the REAL Go API,
// PostgreSQL and Redis. No request is mocked.

const TIME_ZONE = 'Europe/Luxembourg';
test.use({ locale: 'en-US', timezoneId: TIME_ZONE });

const DAY_MS = 24 * 60 * 60 * 1000;
const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

/** Calendar date ("YYYY-MM-DD") and ISO weekday of an instant in Luxembourg. */
function localDay(instant: Date): { date: string; isoWeekday: number } {
  const date = new Intl.DateTimeFormat('en-CA', { timeZone: TIME_ZONE, year: 'numeric', month: '2-digit', day: '2-digit' }).format(instant);
  const weekday = new Intl.DateTimeFormat('en-US', { timeZone: TIME_ZONE, weekday: 'long' }).format(instant);
  return { date, isoWeekday: WEEKDAYS.indexOf(weekday) + 1 };
}

test('a tutor manages weekly and one-off free times, and booked time is protected', async ({ page }) => {
  const tutor = await approvedTutor('availability', 'Patient explanations.', 18.5);

  // Setup: one weekly time, created through the real API. The page can't create weekly times yet,
  // although GET /semesters is now available; creating weekly times still has no UI control.
  const weekly = localDay(new Date(Date.now() + 3 * DAY_MS));
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const semesters = await tutorApi.get('/api/v1/semesters');
  expect(semesters.status()).toBe(200);
  expect((await semesters.json()).data).toContainEqual(expect.objectContaining({ id: testCatalog().semesterId }));
  const rule = await tutorApi.post('/api/v1/tutors/me/availability/rules', {
    data: {
      semester_id: testCatalog().semesterId,
      iso_weekday: weekly.isoWeekday,
      local_start_time: '14:00',
      local_end_time: '16:00',
      time_zone: TIME_ZONE,
      allowed_mode: 'BOTH',
    },
  });
  expect(rule.status(), await rule.text()).toBe(201);
  const day = WEEKDAYS[weekly.isoWeekday - 1];

  await page.goto('/login');
  await logIn(page, tutor.email, tutor.password);
  await page.getByRole('link', { name: 'My availability' }).first().click();
  await expect(page.getByRole('heading', { name: 'My availability' })).toBeVisible();

  // Weekly time in its own local time and zone; its occurrences are listed as free times.
  await expect(page.getByText(`Every ${day}, 14:00–16:00 (${TIME_ZONE}) · Online or on site`)).toBeVisible();
  await expect(page.getByText('Online or on site · Weekly').first()).toBeVisible();

  // Add a one-off time.
  const oneOff = localDay(new Date(Date.now() + 5 * DAY_MS));
  const form = page.getByRole('region', { name: 'Add a one-off free time' });
  await form.getByLabel('Date', { exact: true }).fill(oneOff.date);
  await form.getByLabel('From', { exact: true }).fill('09:00');
  await form.getByLabel('Until', { exact: true }).fill('10:30');
  await form.getByLabel('Where', { exact: true }).selectOption('ONLINE');
  await form.getByRole('button', { name: 'Add free time' }).click();
  await expect(form.getByText('Added. Students can now book this time.')).toBeVisible();
  await expect(page.getByText('Online · One-off')).toBeVisible();

  // The same time again overlaps (real 409).
  await form.getByLabel('Date', { exact: true }).fill(oneOff.date);
  await form.getByLabel('From', { exact: true }).fill('09:30');
  await form.getByLabel('Until', { exact: true }).fill('10:00');
  await form.getByRole('button', { name: 'Add free time' }).click();
  await expect(form.getByRole('alert')).toContainText('This overlaps another free time of yours.');

  // Edit the weekly time: end at 17:00 instead of 16:00.
  await page.getByRole('button', { name: `Edit Every ${day}, 14:00–16:00 (${TIME_ZONE}) · Online or on site` }).click();
  const editor = page.getByRole('form', { name: 'Edit weekly time' });
  await editor.getByLabel('Until', { exact: true }).fill('17:00');
  await editor.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.getByText(`Every ${day}, 14:00–17:00 (${TIME_ZONE}) · Online or on site`)).toBeVisible();

  // Skip the first weekly occurrence, then restore it.
  const firstWeekly = page.getByRole('listitem').filter({ hasText: 'Online or on site · Weekly' }).first();
  await firstWeekly.getByRole('button', { name: /^Remove/ }).click();
  await firstWeekly.getByRole('button', { name: 'Yes, skip it' }).click();
  await expect(page.getByText('That date is now skipped')).toBeVisible();
  await page.getByRole('button', { name: /^Skipped dates of Every/ }).click();
  const skipped = page.getByRole('list', { name: 'Skipped dates' });
  await expect(skipped.getByRole('listitem')).toHaveCount(1);
  await skipped.getByRole('button', { name: /^Restore/ }).click();
  await expect(page.getByText('No skipped dates.')).toBeVisible();

  // A student books the one-off time (real POST /sessions) ...
  const me = (await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string };
  const windows = (await (await tutorApi.get('/api/v1/tutors/me/availability')).json()) as {
    data: { id: string; starts_at: string; is_recurring: boolean }[];
  };
  const target = windows.data.find((window) => !window.is_recurring)!;
  const learner = await verifiedStudent('booker');
  const bookingId = await bookWindow(learner, me.id, target.id, target.starts_at);

  // ... so the tutor can't remove it, and the page says why.
  await page.reload();
  const booked = page.getByRole('listitem').filter({ hasText: 'Online · One-off' });
  await booked.getByRole('button', { name: /^Remove/ }).click();
  await booked.getByRole('button', { name: 'Yes, remove' }).click();
  await expect(booked.getByRole('alert')).toContainText('A student has a booking that depends on this time');
  await expect(page.getByText('Online · One-off')).toBeVisible();

  // Releasing the booking makes the same deletion legal; both UI and persisted lists agree.
  const learnerApi = await loggedIn(learner.email, learner.password);
  try {
    expect((await learnerApi.post(`/api/v1/sessions/${bookingId}/cancel`, { data: { reason: 'No longer needed.' } })).status()).toBe(200);
  } finally {
    await learnerApi.dispose();
  }
  await booked.getByRole('button', { name: 'Yes, remove' }).click();
  await expect(page.getByText('Online · One-off')).toHaveCount(0);
  await page.getByRole('button', { name: /^Delete Every/ }).click();
  await page.getByRole('button', { name: 'Yes, delete' }).click();
  await expect(page.getByText('You have no weekly times.')).toBeVisible();
  await page.reload();
  await expect(page.getByText('You have no weekly times.')).toBeVisible();
  const remainingRules = await tutorApi.get('/api/v1/tutors/me/availability/rules');
  expect(remainingRules.status()).toBe(200);
  expect((await remainingRules.json()).data).toEqual([]);
  const remainingSlots = await tutorApi.get('/api/v1/tutors/me/availability');
  expect(remainingSlots.status()).toBe(200);
  expect((await remainingSlots.json()).data).toEqual([]);
  await tutorApi.dispose();
});
