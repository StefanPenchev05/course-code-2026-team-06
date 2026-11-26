import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { StrictMode, useState } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthProvider } from '../features/auth/AuthProvider';
import { useLiveRefresh } from '../shared/realtime';
import { event, FakeWebSocket } from '../test/fakeWebSocket';
import { json, mockApi, verifiedStudent } from '../test/mockApi';
import { RealtimeProvider } from './RealtimeProvider';
import { routes } from './routes';

// Component tests with a fake WebSocket and mocked HTTP API (test doubles).
// The real gateway is exercised in tests/e2e/realtime.spec.ts.

const pending = { ...verifiedStudent, verified_student: false, verification_status: 'PENDING' };
const TUTOR_A = '11111111-1111-4111-8111-111111111111';
const TUTOR_B = '22222222-2222-4222-8222-222222222222';

function renderApp(path = '/') {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <StrictMode>
      <AuthProvider>
        <RealtimeProvider>
          <RouterProvider router={router} />
        </RealtimeProvider>
      </AuthProvider>
    </StrictMode>,
  );
  return router;
}

/** Sockets the app has open right now. */
const openSockets = () => FakeWebSocket.instances.filter((s) => s.readyState !== 3);

/** The provider starts its socket one tick after login (so StrictMode never opens two). */
async function socket(): Promise<FakeWebSocket> {
  await waitFor(() => expect(openSockets()).toHaveLength(1));
  return openSockets()[0];
}

beforeEach(() => {
  FakeWebSocket.reset();
  vi.stubGlobal('WebSocket', FakeWebSocket);
});
afterEach(() => vi.unstubAllGlobals());

describe('RealtimeProvider', () => {
  it('connects once for a logged-in user and disconnects for good on logout', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent), 'POST /api/v1/auth/logout': () => json(204) });
    renderApp();
    const user = userEvent.setup();

    await screen.findByRole('heading', { name: 'Welcome, Ana Student' });
    expect((await socket()).url).toBe('ws://localhost:3000/ws');
    // StrictMode mounted twice, but only one socket was ever created.
    expect(FakeWebSocket.instances).toHaveLength(1);

    await user.click(screen.getByRole('button', { name: 'Log out' }));
    await screen.findByRole('heading', { name: 'Log in' });
    expect(openSockets()).toHaveLength(0);
    const created = FakeWebSocket.instances.length;
    await act(() => new Promise((resolve) => setTimeout(resolve, 50)));
    expect(FakeWebSocket.instances).toHaveLength(created); // no reconnect after logout
  });

  it('does not connect while logged out', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }) });
    renderApp('/login');

    await screen.findByRole('heading', { name: 'Log in' });
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it('shows an approval live when the account access changes, without a reload', async () => {
    let me: object = pending;
    mockApi({ 'GET /api/v1/auth/me': () => json(200, me) });
    renderApp();

    expect(await screen.findByText('Your registration is waiting for approval')).toBeInTheDocument();
    const ws = await socket();
    act(() => ws.open());

    me = verifiedStudent; // a coordinator approved the registration
    act(() => ws.receive(event('account.access_changed')));

    // The student features appear (menu and home page) and the pending notice goes away.
    expect((await screen.findAllByRole('link', { name: 'Find a tutor' })).length).toBeGreaterThan(0);
    expect(screen.queryByText('Your registration is waiting for approval')).not.toBeInTheDocument();
  });

  it('re-checks the session when the connection is refused, and stops if logged out', async () => {
    let loggedIn = true;
    mockApi({
      'GET /api/v1/auth/me': () => (loggedIn ? json(200, verifiedStudent) : json(401, { error: 'invalid credentials or session' })),
    });
    renderApp();
    await screen.findByRole('heading', { name: 'Welcome, Ana Student' });
    const ws = await socket();

    loggedIn = false; // the session expired on the server
    act(() => ws.drop()); // handshake refused before opening

    expect(await screen.findByText('Your session ended')).toBeInTheDocument();
    expect(openSockets()).toHaveLength(0);
    const created = FakeWebSocket.instances.length;
    await act(() => new Promise((resolve) => setTimeout(resolve, 1200)));
    expect(FakeWebSocket.instances).toHaveLength(created); // no reconnect loop
  });
});

describe('useLiveRefresh', () => {
  function Probe({ entityId }: { entityId?: string }) {
    const [reloads, setReloads] = useState(0);
    useLiveRefresh(['availability.changed'], () => setReloads((n) => n + 1), { entityId, debounceMs: 20 });
    return <p>reloads: {reloads}</p>;
  }

  function renderProbe(entityId?: string) {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    render(
      <AuthProvider>
        <RealtimeProvider>
          <Probe entityId={entityId} />
        </RealtimeProvider>
      </AuthProvider>,
    );
  }

  const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 60)));

  it('reloads once for a burst of matching events and ignores other types', async () => {
    renderProbe();
    await socket();
    act(() => openSockets()[0].open()); // first connection: one resync reload
    await settle();
    expect(screen.getByText('reloads: 1')).toBeInTheDocument();

    act(() => {
      for (let i = 0; i < 3; i++) openSockets()[0].receive(event('availability.changed'));
      openSockets()[0].receive(event('booking.changed'));
    });
    await settle();
    expect(screen.getByText('reloads: 2')).toBeInTheDocument();
  });

  it('reloads after a reconnection and for unknown event types', async () => {
    renderProbe();
    await socket();
    act(() => openSockets()[0].open());
    await settle();
    expect(screen.getByText('reloads: 1')).toBeInTheDocument();

    act(() => openSockets()[0].receive(event('something.new')));
    await settle();
    expect(screen.getByText('reloads: 2')).toBeInTheDocument();

    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    act(() => openSockets()[0].drop(1006));
    act(() => vi.advanceTimersByTime(30_000));
    vi.useRealTimers();
    act(() => openSockets()[0].open()); // reconnected: resync
    await settle();
    expect(screen.getByText('reloads: 3')).toBeInTheDocument();
  });

  it('only reacts to its own entity', async () => {
    renderProbe(TUTOR_A);
    await socket();
    act(() => openSockets()[0].open());
    await settle();
    act(() => openSockets()[0].receive(event('availability.changed', { entityId: TUTOR_B })));
    await settle();
    expect(screen.getByText('reloads: 1')).toBeInTheDocument();
    act(() => openSockets()[0].receive(event('availability.changed', { entityId: TUTOR_A })));
    await settle();
    expect(screen.getByText('reloads: 2')).toBeInTheDocument();
  });
});
