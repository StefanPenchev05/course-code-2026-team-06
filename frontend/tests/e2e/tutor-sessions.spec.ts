import { expect, test } from '@playwright/test';
import { approvedTutorWithFreeTime, bookWindow, loggedIn, verifiedStudent } from './backend';
import { logIn } from './ui';

// Browser journey for issue #33 (tutor answers and completes sessions) against the
// REAL Go API, PostgreSQL and Redis. No request is mocked.
//
// Completing a session needs its scheduled end to have passed, while booking needs at least
// three hours' notice; the real API uses the real clock and the database forbids moving a
// booked session. So this journey checks that completion is NOT offered (and is refused by the
// server) before the end. Completing an ended session is covered by session-lifecycle.spec.ts with a historical starting fixture.

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

test('a tutor declines one request with a reason and accepts another with a meeting link', async ({ page }) => {
  const startsAt = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime('answering-tutor', 'Happy to help.', 19, {
    startsAt,
    endsAt: new Date(startsAt.getTime() + 3 * 60 * 60 * 1000),
  });

  // Two students request the first two hours of the tutor's free time (real POST /sessions).
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const me = (await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string };
  const own = (await (await tutorApi.get('/api/v1/tutors/me/availability')).json()) as { data: { id: string }[] };
  const accepted = await verifiedStudent('accepted-learner');
  const declined = await verifiedStudent('declined-learner');
  const acceptedId = await bookWindow(accepted, me.id, own.data[0].id, startsAt.toISOString(), 'Please accept me: recursion.');
  const declinedId = await bookWindow(
    declined,
    me.id,
    own.data[0].id,
    new Date(startsAt.getTime() + 60 * 60 * 1000).toISOString(),
    'Please decline me: graphs.',
  );

  await page.goto('/login');
  await logIn(page, tutor.email, tutor.password);
  await page.getByRole('link', { name: 'Teaching', exact: true }).first().click();
  const requests = page.getByRole('region', { name: /^Requests waiting for you/ });
  await expect(page.getByRole('heading', { name: 'Requests waiting for you (2)' })).toBeVisible();

  // Decline: a reason is required (then a real decline).
  const toDecline = requests.getByRole('listitem').filter({ hasText: 'Please decline me: graphs.' });
  await toDecline.getByRole('button', { name: /^Decline / }).click();
  await toDecline.getByRole('button', { name: 'Decline the session' }).click();
  await expect(toDecline.getByText('Give a reason; the student sees it.')).toBeVisible();
  await toDecline.getByLabel('Reason for declining').fill('I am travelling that day.');
  await toDecline.getByRole('button', { name: 'Decline the session' }).click();
  await expect(page.getByText('Declined. The student was notified with your reason.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Requests waiting for you (1)' })).toBeVisible();

  // Accept: an online session needs a valid meeting link (then a real accept).
  const toAccept = requests.getByRole('listitem').filter({ hasText: 'Please accept me: recursion.' });
  await toAccept.getByRole('button', { name: /^Accept / }).click();
  await toAccept.getByLabel('Meeting link').fill('meet me online');
  await toAccept.getByRole('button', { name: 'Accept the session' }).click();
  await expect(toAccept.getByText(/Enter a meeting link that starts with https:\/\//)).toBeVisible();
  await toAccept.getByLabel('Meeting link').fill('https://teams.example/meet/e2e');
  await toAccept.getByRole('button', { name: 'Accept the session' }).click();

  const confirmed = page.getByRole('region', { name: /^Confirmed sessions/ });
  await expect(page.getByRole('heading', { name: 'Confirmed sessions (1)' })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Requests waiting for you (0)' })).toBeVisible();
  const confirmedCard = confirmed.getByRole('listitem').filter({ hasText: 'Please accept me: recursion.' });
  await expect(confirmedCard.getByRole('link', { name: 'https://teams.example/meet/e2e' })).toBeVisible();

  // Not ended yet: completion is not offered, and the real server refuses it too.
  await expect(confirmedCard.getByText(/You can mark it as completed after it ends/)).toBeVisible();
  await expect(confirmedCard.getByRole('button', { name: /completed/i })).toHaveCount(0);
  const early = await tutorApi.post(`/api/v1/tutors/me/sessions/${acceptedId}/complete`);
  expect(early.status()).toBe(409);

  // Final states offer no repeat actions: answering the declined request again is refused (real 409).
  const again = await tutorApi.post(`/api/v1/tutors/me/session-requests/${declinedId}/accept`, {
    data: { meeting_link: 'https://teams.example/meet/again' },
  });
  expect(again.status()).toBe(409);
  await tutorApi.dispose();

  // What the students see (real API): one confirmed with the link, one declined with the reason.
  const acceptedApi = await loggedIn(accepted.email, accepted.password);
  const acceptedView = (await (await acceptedApi.get(`/api/v1/sessions/${acceptedId}`)).json()) as {
    data: { status: string; meeting_link: string };
  };
  expect(acceptedView.data).toEqual(expect.objectContaining({ status: 'ACCEPTED', meeting_link: 'https://teams.example/meet/e2e' }));
  await acceptedApi.dispose();
  const declinedApi = await loggedIn(declined.email, declined.password);
  const declinedView = (await (await declinedApi.get(`/api/v1/sessions/${declinedId}`)).json()) as {
    data: { status: string; decline_reason: string };
  };
  expect(declinedView.data).toEqual(expect.objectContaining({ status: 'DECLINED', decline_reason: 'I am travelling that day.' }));
  await declinedApi.dispose();

  // The student also sees the confirmed session and its link in My sessions (browser).
  await page.getByRole('button', { name: 'Log out' }).click();
  await logIn(page, accepted.email, accepted.password);
  await page.getByRole('link', { name: 'My sessions' }).first().click();
  const studentCard = page.getByRole('listitem').filter({ hasText: 'Please accept me: recursion.' });
  await expect(studentCard.getByText('Confirmed', { exact: true })).toBeVisible();
  await expect(studentCard.getByRole('link', { name: 'https://teams.example/meet/e2e' })).toBeVisible();
});
