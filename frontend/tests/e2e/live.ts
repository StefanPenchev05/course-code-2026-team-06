import { expect, request, type Page, type WebSocketRoute } from '@playwright/test';
import { approvedTutorWithFreeTime, loggedIn, newStudent, type Student } from './backend';
import { E2E, type TestCatalog } from './env';
import { createTestCatalog } from './global-setup';
import { logIn } from './ui';

/**
 * Helpers for the live-update journeys (#90–#94) against the REAL Go API, Redis and /ws gateway.
 * They never sleep: every wait is a visible-state or connection-state assertion.
 */

/** Marks the page so a reload (which would lose the mark) can be detected. */
export async function markPage(page: Page) {
  await page.evaluate(() => {
    (window as unknown as { __noReload: boolean }).__noReload = true;
  });
}

export const notReloaded = (page: Page) => page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload === true);

/** Logs in through the development test-account form, then opens `path`. */
export async function openAs(page: Page, who: { email: string; password: string }, path: string) {
  await page.goto('/login');
  await logIn(page, who.email, who.password);
  await expect(page.getByRole('button', { name: 'Log out' })).toBeVisible();
  await page.goto(path);
}

/**
 * Waits until the page's live connection is open (RealtimeProvider sets <html data-live>).
 * Changes made after this reach the page as events, not only through the refetch that
 * follows a (re)connection.
 */
export async function waitUntilLive(page: Page) {
  await expect(page.locator('html[data-live="open"]')).toBeAttached({ timeout: 20_000 });
}

/** Every text frame the page receives from the gateway, to prove what it did NOT receive. */
export function recordEvents(page: Page): string[] {
  const frames: string[] = [];
  page.on('websocket', (socket) => {
    // Only the app's gateway; Vite's development server has its own hot-reload socket.
    if (!new URL(socket.url()).pathname.endsWith('/ws')) return;
    socket.on('framereceived', (frame) => frames.push(String(frame.payload)));
  });
  return frames;
}

/**
 * Passes the page's gateway socket through to the real server, but lets the test cut it and
 * refuse reconnects (what a dropped network looks like to the app), then allow them again.
 */
export async function cuttableGateway(page: Page) {
  let blocked = false;
  let current: WebSocketRoute | null = null;
  await page.routeWebSocket(/\/ws$/, (ws) => {
    if (blocked) {
      void ws.close({ code: 1001, reason: '' });
      return;
    }
    current = ws;
    ws.connectToServer();
  });
  return {
    async cut() {
      blocked = true;
      await expect.poll(() => current !== null).toBe(true);
      await current!.close({ code: 1001, reason: '' });
    },
    restore() {
      blocked = false;
    },
  };
}

/** An approved tutor with a four-hour free time three days ahead, and the IDs a booking needs. */
export async function tutorWithFreeTime(prefix: string) {
  const HOUR = 60 * 60 * 1000;
  const startsAt = new Date(Date.now() + 3 * 24 * HOUR);
  startsAt.setUTCMinutes(0, 0, 0);
  const tutor = await approvedTutorWithFreeTime(prefix, `${prefix} tutor.`, 18, { startsAt, endsAt: new Date(startsAt.getTime() + 4 * HOUR) });
  const api = await loggedIn(tutor.email, tutor.password);
  try {
    const tutorId = ((await (await api.get('/api/v1/auth/me')).json()) as { id: string }).id;
    const slotId = ((await (await api.get('/api/v1/tutors/me/availability')).json()) as { data: { id: string }[] }).data[0].id;
    return { tutor, tutorId, slotId, startsAt, hour: (n: number) => new Date(startsAt.getTime() + n * HOUR).toISOString() };
  } finally {
    await api.dispose();
  }
}

/** The user ID of an account (GET /auth/me). */
export async function userIdOf(who: Student): Promise<string> {
  const api = await loggedIn(who.email, who.password);
  try {
    return ((await (await api.get('/api/v1/auth/me')).json()) as { id: string }).id;
  } finally {
    await api.dispose();
  }
}

/** A second faculty with its own program, subject, semester and coordinator, like global-setup.ts makes. */
export async function anotherFaculty(): Promise<TestCatalog> {
  const api = await request.newContext({ baseURL: E2E.baseURL, extraHTTPHeaders: { Origin: E2E.baseURL } });
  try {
    return await createTestCatalog(api);
  } finally {
    await api.dispose();
  }
}

/** A student of `catalog`'s program, registered and approved by that faculty's coordinator (real API). */
export async function verifiedStudentOf(catalog: TestCatalog, prefix: string): Promise<Student> {
  const student = newStudent(prefix);
  const anonymous = await request.newContext({ baseURL: E2E.baseURL, extraHTTPHeaders: { Origin: E2E.baseURL } });
  try {
    const registered = await anonymous.post('/api/v1/auth/register', {
      data: { name: student.name, email: student.email, student_number: student.studentNumber, program_id: catalog.programId, password: student.password },
    });
    expect(registered.status(), `registration of ${student.email}`).toBe(201);
  } finally {
    await anonymous.dispose();
  }
  const coordinator = await loggedIn(catalog.coordinatorEmail, catalog.coordinatorPassword);
  try {
    const queue = (await (await coordinator.get('/api/v1/coordinator/verifications')).json()) as { id: string; email: string }[];
    const pending = queue.find((item) => item.email === student.email);
    expect(pending, `registration of ${student.email} is in that faculty's queue`).toBeDefined();
    expect((await coordinator.post(`/api/v1/coordinator/verifications/${pending!.id}/approve`)).status()).toBe(204);
  } finally {
    await coordinator.dispose();
  }
  return student;
}

/** A general problem report (real POST /issues, as the report form sends it). Returns its ID. */
export async function reportProblem(who: Student, description: string): Promise<string> {
  const api = await loggedIn(who.email, who.password);
  try {
    const sent = await api.post('/api/v1/issues', { data: { description } });
    expect(sent.status(), `problem report: ${await sent.text()}`).toBe(201);
    return ((await sent.json()) as { data: { id: string } }).data.id;
  } finally {
    await api.dispose();
  }
}

/** The short reference the coordinator inbox shows for a report (#53). */
export const reportReference = (id: string) => id.replace(/-/g, '').slice(0, 8).toUpperCase();
