import { expect, test, type Page } from '@playwright/test';
import { approvedTutorWithFreeTime, loggedIn, verifiedStudent } from './backend';

// Issue #59: the core student journey with the keyboard only (Tab, typing, Enter), against the
// REAL Go API. Also checks that focus is always visible and that booking takes three steps
// after the tutor profile opens (NFR6).

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

/** What the focused element is, for matching and for error messages. */
async function focused(page: Page) {
  return page.evaluate(() => {
    const el = document.activeElement as HTMLElement | null;
    if (!el) return { name: '', tag: '', outline: 'none', boxShadow: 'none' };
    const style = getComputedStyle(el);
    const label = el.getAttribute('aria-label') ?? (el.id ? document.querySelector(`label[for="${el.id}"]`)?.textContent : null) ?? el.textContent ?? '';
    return { name: label.trim(), tag: el.tagName.toLowerCase(), outline: style.outlineStyle, boxShadow: style.boxShadow };
  });
}

/**
 * Presses Tab until the focused element's name matches, checking on the way that every
 * focused element shows a visible focus outline. Fails if it isn't reachable.
 */
async function tabTo(page: Page, name: RegExp, max = 60) {
  for (let i = 0; i < max; i++) {
    await page.keyboard.press('Tab');
    const current = await focused(page);
    // After the last element the browser focuses the page itself before wrapping around.
    if (current.tag === 'body') continue;
    expect(current.outline !== 'none' || current.boxShadow !== 'none', `visible focus on "${current.name}" (${current.tag})`).toBe(true);
    if (name.test(current.name)) return;
  }
  throw new Error(`Could not reach ${name} with the Tab key`);
}

test('a student logs in and books a session with the keyboard only, in three steps', async ({ page }) => {
  const startsAt = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime('keyboard-tutor', 'Keyboard friendly.', 18, {
    startsAt,
    endsAt: new Date(startsAt.getTime() + 2 * 60 * 60 * 1000),
  });
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const tutorId = ((await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string }).id;
  await tutorApi.dispose();
  const learner = await verifiedStudent('keyboard-learner');

  // The first Tab reaches the skip link, which is visible when focused.
  await page.goto('/login');
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeInViewport();

  // Log in: Tab to the fields, type, press Enter.
  await tabTo(page, /^University email$/);
  await page.keyboard.type(learner.email);
  await tabTo(page, /^Password$/);
  await page.keyboard.type(learner.password);
  await page.keyboard.press('Enter');
  await expect(page.getByRole('heading', { name: `Welcome, ${learner.name}` })).toBeVisible();

  // Open the tutor profile (the three steps are counted from here).
  await page.goto(`/tutors/${tutorId}`);
  await expect(page.getByRole('heading', { name: tutor.name, level: 1 })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Book / }).first()).toBeVisible(); // free times loaded

  // Step 1: choose a free time.
  await tabTo(page, /^Book /);
  await page.keyboard.press('Enter');
  // Focus moves into the form, so keyboard users continue right there.
  await expect(page.getByRole('heading', { name: /^Book / })).toBeFocused();

  // Step 2: fill in the form (keeping the defaults: exam preparation, 60 minutes, first start time).
  await tabTo(page, /^What do you need help with\?$/);
  await page.keyboard.type('Booked with the keyboard only.');

  // Step 3: send.
  await tabTo(page, /^Send request$/);
  await page.keyboard.press('Enter');

  // The confirmation receives focus, and the request exists on the server.
  await expect(page.getByText('Request sent')).toBeVisible();
  const confirmation = page.locator('div[tabindex="-1"]').filter({ hasText: 'Request sent' });
  await expect(confirmation).toBeFocused();
  const learnerApi = await loggedIn(learner.email, learner.password);
  const mine = (await (await learnerApi.get('/api/v1/sessions/me')).json()) as { data: { description: string; status: string }[] };
  expect(mine.data).toEqual([expect.objectContaining({ description: 'Booked with the keyboard only.', status: 'REQUESTED' })]);
  await learnerApi.dispose();
});
