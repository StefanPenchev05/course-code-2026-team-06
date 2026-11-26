import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page, type TestInfo } from '@playwright/test';
import { approvedTutorWithFreeTime, bookWindow, loggedIn, newStudent, registerViaApi, verifiedStudent, type Student } from './backend';
import { testCatalog } from './env';
import { logIn } from './ui';

// Issue #59: accessibility and mobile checks of every screen, against the REAL Go API with real
// data (so lists, forms and detail pages are filled). axe checks WCAG 2.1 A/AA rules; each page
// is also checked at phone width (375px) for sideways scrolling.

const WCAG = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];
const PHONE = { width: 375, height: 812 };
const DESKTOP = { width: 1280, height: 800 };

interface Finding {
  page: string;
  width: number;
  rule: string;
  impact: string;
  help: string;
  targets: string[];
}

/** Checks the current page at desktop and phone width; returns problems instead of failing early. */
async function audit(page: Page, name: string): Promise<Finding[]> {
  const findings: Finding[] = [];
  for (const size of [DESKTOP, PHONE]) {
    await page.setViewportSize(size);
    await expect(page.locator('h1').first()).toBeVisible();
    const result = await new AxeBuilder({ page }).withTags(WCAG).analyze();
    for (const violation of result.violations) {
      findings.push({
        page: name,
        width: size.width,
        rule: violation.id,
        impact: violation.impact ?? 'unknown',
        help: violation.help,
        targets: violation.nodes.slice(0, 3).map((node) => node.target.join(' ')),
      });
    }
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    if (overflow > 0) {
      findings.push({ page: name, width: size.width, rule: 'horizontal-scroll', impact: 'serious', help: `Page is ${overflow}px wider than the screen`, targets: [] });
    }
    const headings = await page.locator('h1').count();
    if (headings !== 1) {
      findings.push({ page: name, width: size.width, rule: 'one-h1', impact: 'moderate', help: `Expected exactly one <h1>, found ${headings}`, targets: [] });
    }
  }
  await page.setViewportSize(DESKTOP);
  return findings;
}

async function visit(page: Page, path: string, name: string, all: Finding[]) {
  await page.goto(path);
  await page.waitForLoadState('networkidle');
  all.push(...(await audit(page, name)));
}

/** Opens the first detail link in the main content that starts with `prefix`, if the list has one. */
async function visitFirstDetail(page: Page, listPath: string, prefix: string, name: string, all: Finding[]) {
  await page.goto(listPath);
  await page.waitForLoadState('networkidle');
  const link = page.locator(`main a[href^="${prefix}"]`).first();
  if ((await link.count()) === 0) {
    all.push({ page: name, width: 0, rule: 'not-checked', impact: 'info', help: `No item to open on ${listPath}`, targets: [] });
    return;
  }
  await link.click();
  await page.waitForLoadState('networkidle');
  all.push(...(await audit(page, name)));
}

async function asUser(page: Page, user: Pick<Student, 'email' | 'password'>) {
  await page.context().clearCookies();
  await page.goto('/login');
  await logIn(page, user.email, user.password);
  await expect(page.getByRole('button', { name: 'Log out' })).toBeVisible();
}

function report(testInfo: TestInfo, findings: Finding[]) {
  const problems = findings.filter((finding) => finding.impact !== 'info');
  const text = problems.length
    ? problems.map((f) => `- [${f.impact}] ${f.page} @${f.width}px: ${f.rule}: ${f.help}${f.targets.length ? ` (${f.targets.join(' | ')})` : ''}`).join('\n')
    : 'No accessibility or mobile problems found.';
  const skipped = findings.filter((finding) => finding.impact === 'info').map((f) => `- ${f.page}: ${f.help}`).join('\n');
  void testInfo.attach('accessibility-report', { body: `${text}\n\nNot checked:\n${skipped || '- none'}`, contentType: 'text/plain' });
  console.log(text);
  if (skipped) console.log(`Not checked:\n${skipped}`);
  return problems;
}

test('every screen passes automated accessibility checks and fits a phone screen', async ({ page }, testInfo) => {
  test.setTimeout(180_000);
  const findings: Finding[] = [];
  const { coordinatorEmail, coordinatorPassword } = testCatalog();

  // Real data so lists and detail pages have content.
  const startsAt = new Date(Date.now() + 3 * 24 * 60 * 60 * 1000);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime('a11y-tutor', 'Accessible explanations.', 18, {
    startsAt,
    endsAt: new Date(startsAt.getTime() + 3 * 60 * 60 * 1000),
  });
  const learner = await verifiedStudent('a11y-learner');
  const tutorApi = await loggedIn(tutor.email, tutor.password);
  const tutorId = ((await (await tutorApi.get('/api/v1/auth/me')).json()) as { id: string }).id;
  const slotId = ((await (await tutorApi.get('/api/v1/tutors/me/availability')).json()) as { data: { id: string }[] }).data[0].id;
  await tutorApi.dispose();
  const sessionId = await bookWindow(learner, tutorId, slotId, startsAt.toISOString(), 'Accessibility test booking.');
  const learnerApi = await loggedIn(learner.email, learner.password);
  expect((await learnerApi.post('/api/v1/issues', { data: { description: 'Accessibility test report.', session_id: sessionId } })).status()).toBe(201);
  await learnerApi.dispose();
  const applicant = await verifiedStudent('a11y-applicant');
  const applicantApi = await loggedIn(applicant.email, applicant.password);
  expect(
    (await applicantApi.post('/api/v1/tutor-applications', { data: { subject_id: testCatalog().subjectId, grade: 17, description: 'Pending.' } })).status(),
  ).toBe(201);
  await applicantApi.dispose();
  await registerViaApi(newStudent('a11y-pending')); // waits in the coordinator's verification queue

  // Logged out.
  await visit(page, '/login', 'Log in', findings);
  await visit(page, '/register', 'Register', findings);
  await visit(page, '/no-such-page', 'Not found', findings);

  // Student.
  await asUser(page, learner);
  await visit(page, '/', 'Home (student)', findings);
  await visit(page, `/tutors?subject_id=${testCatalog().subjectId}`, 'Find a tutor (results)', findings);
  await visit(page, `/tutors/${tutorId}`, 'Tutor profile', findings);
  await page.getByRole('button', { name: /^Book / }).first().click();
  findings.push(...(await audit(page, 'Tutor profile (booking form open)')));
  await visit(page, '/sessions', 'My sessions', findings);
  await visit(page, '/sessions?show=all', 'My sessions (all)', findings);
  await visit(page, '/tutor-applications', 'Become a tutor', findings);
  await visit(page, '/report', 'Report a problem', findings);

  // Tutor.
  await asUser(page, tutor);
  await visit(page, '/tutor', 'Teaching', findings);
  await visit(page, '/tutor/availability', 'My availability', findings);
  await visit(page, '/tutor/activity', 'My activity', findings);

  // Coordinator.
  await asUser(page, { email: coordinatorEmail, password: coordinatorPassword });
  await visit(page, '/coordinator', 'Coordinator home', findings);
  await visit(page, '/coordinator/verifications', 'Verification queue', findings);
  await visitFirstDetail(page, '/coordinator/verifications', '/coordinator/verifications/', 'Verification detail', findings);
  await visit(page, '/coordinator/tutor-applications', 'Tutor applications', findings);
  await visitFirstDetail(page, '/coordinator/tutor-applications', '/coordinator/tutor-applications/', 'Tutor application detail', findings);
  await visit(page, '/coordinator/tutors', 'Tutor monitoring', findings);
  await visitFirstDetail(page, '/coordinator/tutors', '/coordinator/tutors/', 'Tutor monitoring detail', findings);
  await visit(page, '/coordinator/issues', 'Issue inbox', findings);
  await visitFirstDetail(page, '/coordinator/issues', '/coordinator/issues/', 'Issue detail', findings);
  await visit(page, '/coordinator/no-show-reports', 'No-show reports', findings);

  const problems = report(testInfo, findings);
  expect(problems, 'accessibility/mobile problems (see the report above)').toEqual([]);
});
