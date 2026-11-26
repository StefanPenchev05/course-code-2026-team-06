import { expect, test, type Page } from '@playwright/test';
import { approvedTutorWithFreeTime, bookWindow, loggedIn, verifiedStudent } from './backend';
import { historicalAcceptedSession } from './history-fixture';
import { runSql } from './database';
import { E2E } from './env';
import { logIn } from './ui';

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

async function requestedSession(prefix: string, mode: 'ONLINE' | 'ONSITE' = 'ONLINE') {
  const startsAt = new Date(Date.now() + 4 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime(prefix, 'Lifecycle journey tutor.', 18, {
    startsAt, endsAt: new Date(startsAt.getTime() + 60 * 60 * 1000), mode,
  });
  const api = await loggedIn(tutor.email, tutor.password);
  try {
    const meResponse = await api.get('/api/v1/auth/me');
    expect(meResponse.status()).toBe(200);
    const me = await meResponse.json() as { id: string };
    const slotsResponse = await api.get('/api/v1/tutors/me/availability');
    expect(slotsResponse.status()).toBe(200);
    const slots = await slotsResponse.json() as { data: { id: string }[] };
    const learner = await verifiedStudent(`${prefix}-learner`);
    const description = `Session with ${tutor.name}`;
    const id = await bookWindow(learner, me.id, slots.data[0].id, startsAt.toISOString(), description, mode);
    return { tutor, learner, id, tutorId: me.id, description };
  } finally {
    await api.dispose();
  }
}

async function signIn(page: Page, user: { email: string; password: string }, path: string) {
  await page.goto('/login');
  await logIn(page, user.email, user.password);
  await expect(page.getByRole('button', { name: 'Log out' })).toBeVisible();
  await page.goto(path);
}

test('an ended session is completed by its tutor and remains completed for both participants after reload', async ({ page, browser }) => {
  const session = await requestedSession('completion');
  const description = `Historical session with ${session.tutor.name}`;
  const id = historicalAcceptedSession(session.id, description);
  const api = await loggedIn(session.tutor.email, session.tutor.password);
  try {
    // Remove the future setup request through the real workflow, leaving just the historical session.
    expect((await api.post(`/api/v1/tutors/me/session-requests/${session.id}/decline`, {
      data: { reason: 'Future request used only to provision the historical fixture.' },
    })).status()).toBe(200);
    await signIn(page, session.tutor, '/tutor');
    const card = page.getByRole('region', { name: /^Confirmed sessions/ }).getByRole('listitem').filter({ hasText: description });
    await card.getByRole('button', { name: /as completed$/ }).click();
    await expect(card.getByText('Did this session take place?')).toBeVisible();
    await card.getByRole('button', { name: 'Yes, it took place' }).click();
    await expect(page.getByText('Marked as completed. The student can now rate the session.')).toBeVisible();
    await expect(card).toHaveCount(0);
    const result = await api.get(`/api/v1/sessions/${id}`);
    expect(result.status()).toBe(200);
    expect((await result.json()).data).toMatchObject({ status: 'COMPLETED', completed_by_tutor_id: session.tutorId });
    // Verify the durable transition and reservation release, not just the returned DTO.
    expect(runSql(`SELECT to_status::text FROM session_status_history WHERE session_id=:'id'::uuid ORDER BY changed_at DESC LIMIT 1`, { id })).toBe('COMPLETED');
    expect(runSql(`SELECT count(*) FROM session_participant_blocks WHERE session_id=:'id'::uuid`, { id })).toBe('0');
    await page.goto('/sessions?show=all');
    await page.reload();
    await expect(page.getByRole('listitem').filter({ hasText: description }).getByText('Completed', { exact: true })).toBeVisible();

    const learnerContext = await browser.newContext({ baseURL: E2E.baseURL, locale: 'en-US', timezoneId: 'Europe/Luxembourg' });
    try {
      const learnerPage = await learnerContext.newPage();
      await signIn(learnerPage, session.learner, '/sessions?show=all');
      const learnerCard = learnerPage.getByRole('listitem').filter({ hasText: description });
      await expect(learnerCard.getByText('Completed', { exact: true })).toBeVisible();
      await expect(learnerCard.getByRole('button', { name: /Cancel/ })).toHaveCount(0);
    } finally {
      await learnerContext.close();
    }
  } finally {
    await api.dispose();
  }
});

test('onsite acceptance validates the room and tutor cancellation reaches the student and releases the reservation', async ({ page, browser }) => {
  const session = await requestedSession('onsite', 'ONSITE');
  await signIn(page, session.tutor, '/tutor');
  const requestCard = page.getByRole('listitem').filter({ hasText: session.description });
  await requestCard.getByRole('button', { name: /^Accept / }).click();
  await requestCard.getByRole('button', { name: 'Accept the session' }).click();
  await expect(requestCard.getByText('Enter the room or place where you will meet.')).toBeVisible();
  await requestCard.getByLabel('Room or place').fill('Maison du Savoir, room E2E');
  await requestCard.getByRole('button', { name: 'Accept the session' }).click();
  await expect(page.getByRole('region', { name: /^Confirmed sessions/ }).getByText('Room: Maison du Savoir, room E2E')).toBeVisible();

  const learnerContext = await browser.newContext({ baseURL: E2E.baseURL, locale: 'en-US', timezoneId: 'Europe/Luxembourg' });
  try {
    const learnerPage = await learnerContext.newPage();
    await signIn(learnerPage, session.learner, '/sessions');
    const learnerCard = learnerPage.getByRole('listitem').filter({ hasText: session.description });
    await expect(learnerCard.getByText('Room: Maison du Savoir, room E2E')).toBeVisible();
    await page.goto('/sessions');
    const ownCard = page.getByRole('listitem').filter({ hasText: session.description });
    await expect(ownCard.getByText('You are the tutor')).toBeVisible();
    await ownCard.getByRole('button', { name: /^Cancel / }).click();
    await ownCard.getByRole('button', { name: 'Cancel the session' }).click();
    await expect(ownCard.getByText('Give a reason; the other person sees it.')).toBeVisible();
    await ownCard.getByLabel('Reason for cancelling').fill('The room is unavailable.');
    await ownCard.getByRole('button', { name: 'Cancel the session' }).click();
    await expect(ownCard).toHaveCount(0);
    await page.getByRole('button', { name: 'All sessions' }).click();
    await expect(page.getByText('You cancelled. Reason: The room is unavailable.')).toBeVisible();
    await learnerPage.reload();
    await expect(learnerCard).toHaveCount(0);
    await learnerPage.getByRole('button', { name: 'All sessions' }).click();
    await expect(learnerPage.getByText('Cancelled. Reason: The room is unavailable.')).toBeVisible();
    const result = await learnerPage.request.get(`/api/v1/sessions/${session.id}`);
    expect(result.status()).toBe(200);
    expect((await result.json()).data).toMatchObject({ status: 'CANCELLED', cancelled_by_user_id: session.tutorId, cancellation_reason: 'The room is unavailable.' });
    expect(runSql(`SELECT count(*) FROM session_participant_blocks WHERE session_id=:'id'::uuid`, { id: session.id })).toBe('0');
  } finally {
    await learnerContext.close();
  }
});

test('accepting a stale request reports the real conflict and preserves the student cancellation', async ({ page }) => {
  const session = await requestedSession('stale-request');
  await signIn(page, session.tutor, '/tutor');
  const card = page.getByRole('listitem').filter({ hasText: session.description });
  await card.getByRole('button', { name: /^Accept / }).click();
  await card.getByLabel('Meeting link').fill('https://meeting.example/stale');
  const learnerApi = await loggedIn(session.learner.email, session.learner.password);
  try {
    expect((await learnerApi.post(`/api/v1/sessions/${session.id}/cancel`, { data: { reason: 'Plans changed while tutor was answering.' } })).status()).toBe(200);
    const conflict = page.waitForResponse((response) => response.url().endsWith(`/session-requests/${session.id}/accept`) && response.request().method() === 'POST');
    await card.getByRole('button', { name: 'Accept the session' }).click();
    expect((await conflict).status()).toBe(409);
    await expect(card.getByRole('alert')).toContainText('This session was changed in the meantime');
    await page.reload();
    await expect(page.getByText(session.description)).toHaveCount(0);
    const result = await learnerApi.get(`/api/v1/sessions/${session.id}`);
    expect(result.status()).toBe(200);
    expect((await result.json()).data).toMatchObject({ status: 'CANCELLED', cancellation_reason: 'Plans changed while tutor was answering.' });
  } finally {
    await learnerApi.dispose();
  }
});

test('a verified student cannot open tutor tools or read another student booking', async ({ page }) => {
  const session = await requestedSession('private-session');
  const stranger = await verifiedStudent('unrelated-student');
  await signIn(page, stranger, '/tutor/availability');
  await expect(page.getByRole('heading', { name: "You don't have access to this page" })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Add a one-off free time' })).toHaveCount(0);
  await page.goto('/tutor');
  await expect(page.getByRole('heading', { name: "You don't have access to this page" })).toBeVisible();
  for (const path of ['/api/v1/tutors/me/availability', '/api/v1/tutors/me/session-requests', '/api/v1/semesters']) {
    expect((await page.request.get(path)).status(), path).toBe(403);
  }
  expect((await page.request.get(`/api/v1/sessions/${session.id}`)).status()).toBe(404);
  expect((await page.request.post(`/api/v1/sessions/${session.id}/cancel`, {
    headers: { Origin: E2E.baseURL }, data: { reason: 'Unauthorized cancellation attempt.' },
  })).status()).toBe(404);
  const owner = await loggedIn(session.learner.email, session.learner.password);
  try {
    const result = await owner.get(`/api/v1/sessions/${session.id}`);
    expect(result.status()).toBe(200);
    expect((await result.json()).data.status).toBe('REQUESTED');
  } finally {
    await owner.dispose();
  }
  await page.goto('/sessions?show=all');
  await expect(page.getByRole('heading', { name: 'My sessions' })).toBeVisible();
  await expect(page.getByText(session.description)).toHaveCount(0);
});
