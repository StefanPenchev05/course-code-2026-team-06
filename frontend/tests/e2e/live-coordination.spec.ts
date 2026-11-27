import { expect, test } from '@playwright/test';
import { loggedIn, verifiedStudent } from './backend';
import { testCatalog } from './env';
import { anotherFaculty, markPage, notReloaded, openAs, recordEvents, reportProblem, reportReference, userIdOf, verifiedStudentOf, waitUntilLive } from './live';

// Issue #94 (screens from #93): coordinator and applicant browsers against the REAL Go API,
// PostgreSQL, Redis and /ws gateway. A change made in one browser appears in the others without
// a reload, and a coordinator never receives or shows another faculty's cases.

test.use({ locale: 'en-US', timezoneId: 'Europe/Luxembourg' });

test('a tutor application and its decision appear live for another coordinator browser and the applicant', async ({ browser }) => {
  test.setTimeout(150_000);
  const catalog = testCatalog();
  const coordinator = { email: catalog.coordinatorEmail, password: catalog.coordinatorPassword };
  const student = await verifiedStudent('live-applicant');
  // The queue shows applicants by a short reference of their user ID (#30).
  const applicantRef = (await userIdOf(student)).slice(0, 8).toUpperCase();

  const applicantContext = await browser.newContext();
  const queueContext = await browser.newContext();
  const reviewerContext = await browser.newContext();
  const applicant = await applicantContext.newPage();
  const queue = await queueContext.newPage();
  const reviewer = await reviewerContext.newPage();

  await openAs(applicant, student, '/tutor-applications');
  await expect(applicant.getByText("You haven't applied to tutor a subject yet.")).toBeVisible();
  await openAs(queue, coordinator, '/coordinator/tutor-applications');
  await expect(queue.getByRole('heading', { name: 'Tutor applications', level: 1 })).toBeVisible();
  await Promise.all([waitUntilLive(applicant), waitUntilLive(queue)]);
  await markPage(applicant);
  await markPage(queue);
  const card = queue.getByRole('listitem').filter({ hasText: applicantRef });

  // 1. The student applies (the real POST /tutor-applications the form sends).
  const studentApi = await loggedIn(student.email, student.password);
  const applied = await studentApi.post('/api/v1/tutor-applications', {
    data: { subject_id: catalog.subjectId, grade: 17.5, description: 'Live: please review me.' },
  });
  expect(applied.status(), `application: ${await applied.text()}`).toBe(201);
  const { application_id: applicationId } = (await applied.json()) as { application_id: string };
  await studentApi.dispose();

  // Both open browsers show it by themselves.
  await expect(card).toBeVisible({ timeout: 20_000 });
  await expect(applicant.getByText('You have an application waiting for review')).toBeVisible({ timeout: 20_000 });

  // 2. The coordinator approves it in another browser.
  await openAs(reviewer, coordinator, `/coordinator/tutor-applications/${applicationId}`);
  await reviewer.getByRole('button', { name: 'Approve', exact: true }).click();
  await reviewer.getByRole('button', { name: 'Yes, approve' }).click();
  await expect(reviewer.getByText(/^Application approved\./)).toBeVisible();

  // The queue drops it and the applicant sees the decision, without a reload. The new tutor
  // role arrives too (account.access_changed): the Teaching link appears.
  await expect(card).toHaveCount(0, { timeout: 20_000 });
  await expect(applicant.getByText('This subject is now on your tutor profile.')).toBeVisible({ timeout: 30_000 });
  await expect(applicant.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Teaching' })).toBeVisible({ timeout: 30_000 });
  expect(await notReloaded(queue)).toBe(true);
  expect(await notReloaded(applicant)).toBe(true);

  await applicantContext.close();
  await queueContext.close();
  await reviewerContext.close();
});

test('problem reports reach only their own faculty’s coordinator, live', async ({ browser }) => {
  test.setTimeout(150_000);
  const own = testCatalog();
  const other = await anotherFaculty();
  const ownStudent = await verifiedStudent('live-report-own');
  const otherStudent = await verifiedStudentOf(other, 'live-report-other');

  const ownContext = await browser.newContext();
  const otherContext = await browser.newContext();
  const ownInbox = await ownContext.newPage();
  const otherInbox = await otherContext.newPage();
  const ownFrames = recordEvents(ownInbox);
  const otherFrames = recordEvents(otherInbox);

  await openAs(ownInbox, { email: own.coordinatorEmail, password: own.coordinatorPassword }, '/coordinator/issues');
  await openAs(otherInbox, { email: other.coordinatorEmail, password: other.coordinatorPassword }, '/coordinator/issues');
  await expect(otherInbox.getByText('No open reports. New problem reports from students and tutors appear here.')).toBeVisible();
  // Both sockets are open, so the checks below can't pass just because nothing was listening.
  await Promise.all([waitUntilLive(ownInbox), waitUntilLive(otherInbox)]);
  await markPage(ownInbox);
  await markPage(otherInbox);

  // 1. A report in the other faculty: only that faculty's inbox shows it.
  const otherReport = await reportProblem(otherStudent, 'Live: report of the other faculty.');
  await expect(otherInbox.getByRole('link', { name: `Report ${reportReference(otherReport)}` })).toBeVisible({ timeout: 20_000 });

  // 2. A report in this run's faculty: only this inbox shows it.
  const ownReport = await reportProblem(ownStudent, 'Live: report of our faculty.');
  await expect(ownInbox.getByRole('link', { name: `Report ${reportReference(ownReport)}` })).toBeVisible({ timeout: 20_000 });

  // Neither coordinator showed, or even received, the other faculty's case ...
  await expect(ownInbox.getByRole('link', { name: `Report ${reportReference(otherReport)}` })).toHaveCount(0);
  await expect(otherInbox.getByRole('link', { name: `Report ${reportReference(ownReport)}` })).toHaveCount(0);
  expect(ownFrames.some((frame) => frame.includes(otherReport))).toBe(false);
  expect(otherFrames.some((frame) => frame.includes(ownReport))).toBe(false);
  // ... while each received the event for its own faculty's case.
  expect(ownFrames.some((frame) => frame.includes(ownReport))).toBe(true);
  expect(otherFrames.some((frame) => frame.includes(otherReport))).toBe(true);
  expect(await notReloaded(ownInbox)).toBe(true);
  expect(await notReloaded(otherInbox)).toBe(true);

  await ownContext.close();
  await otherContext.close();
});
