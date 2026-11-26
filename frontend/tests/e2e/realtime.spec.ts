import { expect, test, type Page, type WebSocketRoute } from '@playwright/test';
import { approveRegistration, newStudent, registerViaApi } from './backend';
import { logIn } from './ui';

// Issue #90: the shared realtime connection against the REAL Go API, Redis and the /ws
// gateway. A coordinator's decision (real API, as #51's screen does it) reaches a student's
// open page without a reload, and after a disconnect the page catches up by refetching.

/** Marks the page so a reload (which would lose the mark) can be detected. */
async function markPage(page: Page) {
  await page.evaluate(() => {
    (window as unknown as { __noReload: boolean }).__noReload = true;
  });
}

async function wasNotReloaded(page: Page) {
  return page.evaluate(() => (window as unknown as { __noReload?: boolean }).__noReload === true);
}

test('an approval reaches the open page live, without a reload', async ({ page }) => {
  const student = newStudent('live');
  await registerViaApi(student);
  const sockets: string[] = [];
  let open = 0;
  let maxOpen = 0;
  page.on('websocket', (socket) => {
    // Only the app's gateway; Vite's development server has its own hot-reload socket.
    if (!new URL(socket.url()).pathname.endsWith('/ws')) return;
    sockets.push(socket.url());
    maxOpen = Math.max(maxOpen, ++open);
    socket.on('close', () => open--);
  });

  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByText('Your registration is waiting for approval')).toBeVisible();
  await expect.poll(() => sockets.length).toBe(1);
  await markPage(page);

  await approveRegistration(student.email);

  // No reload, no click: the page updates by itself.
  await expect(page.getByText('Your registration is waiting for approval')).toHaveCount(0, { timeout: 25_000 });
  await expect(page.getByRole('link', { name: 'Find a tutor' }).first()).toBeVisible();
  expect(await wasNotReloaded(page)).toBe(true);
  // Never two connections at once. (The server closes the socket when access changes; the app
  // then opens one new connection with the new access.)
  expect(maxOpen).toBe(1);
  expect(sockets.length).toBeLessThanOrEqual(2);
});

test('after a disconnect the page shows it is paused, then catches up on what it missed', async ({ page }) => {
  test.setTimeout(90_000);
  const student = newStudent('resync');
  await registerViaApi(student);

  // Pass the socket through to the real gateway, but let the test cut it and refuse reconnects.
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

  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByText('Your registration is waiting for approval')).toBeVisible();
  await expect.poll(() => current !== null).toBe(true);
  await markPage(page);

  // Cut the connection and keep refusing reconnects.
  blocked = true;
  await current!.close({ code: 1001, reason: '' });
  await expect(page.getByRole('status').filter({ hasText: 'Live updates are paused' })).toBeVisible({ timeout: 15_000 });

  // The approval happens while disconnected, so its event is never delivered (no replay).
  // The app still catches up: each refused reconnect re-checks the session over HTTP, and a
  // successful reconnect refetches again.
  await approveRegistration(student.email);

  // Network back: the app reconnects by itself, without a reload.
  blocked = false;
  await expect(page.getByText('Your registration is waiting for approval')).toHaveCount(0, { timeout: 45_000 });
  await expect(page.getByRole('status').filter({ hasText: 'Live updates are paused' })).toHaveCount(0);
  expect(await wasNotReloaded(page)).toBe(true);
});
