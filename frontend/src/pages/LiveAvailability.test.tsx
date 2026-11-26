import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RealtimeProvider } from '../app/RealtimeProvider';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { event, FakeWebSocket } from '../test/fakeWebSocket';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests for live availability and discovery (#92) with a fake WebSocket and a mocked
// HTTP API (test doubles). The two-browser real-backend journey is tests/e2e/live-availability.spec.ts.

const HOUR = 3_600_000;
const TUTOR = '11111111-1111-4111-8111-111111111111';
const OTHER = '22222222-2222-4222-8222-222222222222';
const OFFERING = { id: 'off-1', subject_id: 'sub-1', subject_name: 'Algorithms', description: '', active: true };
const PROFILE = { id: TUTOR, name: 'Tom Tutor', description: '', active: true, average_rating: null, rating_count: 0, offerings: [OFFERING] };
const start = Math.ceil((Date.now() + 3 * 24 * HOUR) / HOUR) * HOUR;
const slot = (id: string, from: number, hours: number) => ({
  id,
  tutor_id: TUTOR,
  starts_at: new Date(from).toISOString(),
  ends_at: new Date(from + hours * HOUR).toISOString(),
  allowed_mode: 'ONLINE',
  is_recurring: false,
});
const page = <T,>(data: T[]) => json(200, { data, pagination: { limit: 100, offset: 0, has_more: false } });

function renderAt(path: string) {
  render(
    <AuthProvider>
      <RealtimeProvider>
        <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
      </RealtimeProvider>
    </AuthProvider>,
  );
}

const openSocket = () => FakeWebSocket.instances.filter((s) => s.readyState !== 3)[0];
async function connected(): Promise<FakeWebSocket> {
  await waitFor(() => expect(openSocket()).toBeDefined());
  const ws = openSocket();
  act(() => ws.open());
  return ws;
}
const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 650)));

beforeEach(() => {
  FakeWebSocket.reset();
  vi.stubGlobal('WebSocket', FakeWebSocket);
});
afterEach(() => vi.unstubAllGlobals());

function mockProfile(slots: () => unknown[], extra: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, verifiedStudent),
    [`GET /api/v1/tutors/${TUTOR}`]: () => json(200, { data: PROFILE }),
    [`GET /api/v1/tutors/${TUTOR}/availability`]: () => page(slots()),
    'POST /api/v1/realtime/availability-watches': () => json(200, { expires_at: new Date(Date.now() + 600_000).toISOString() }),
    ...extra,
  });
}

describe('tutor profile, live', () => {
  it('watches the tutor and updates free times when they are taken and released', async () => {
    let slots = [slot('a', start, 1), slot('b', start + 24 * HOUR, 1)];
    const { calls } = mockProfile(() => slots);
    renderAt(`/tutors/${TUTOR}`);
    const ws = await connected();
    expect(await screen.findAllByRole('button', { name: /^Book / })).toHaveLength(2);
    await waitFor(() =>
      expect(calls.filter((c) => c.url === '/api/v1/realtime/availability-watches').map((c) => c.body)).toContainEqual({ tutor_id: TUTOR }),
    );

    // Someone books "b": it disappears.
    slots = [slot('a', start, 1)];
    act(() => ws.receive(event('availability.changed', { entityId: TUTOR })));
    await settle();
    expect(screen.getAllByRole('button', { name: /^Book / })).toHaveLength(1);

    // The booking is cancelled (reservation released): "b" is free again.
    slots = [slot('a', start, 1), slot('b', start + 24 * HOUR, 1)];
    act(() => ws.receive(event('availability.changed', { entityId: TUTOR })));
    await settle();
    expect(screen.getAllByRole('button', { name: /^Book / })).toHaveLength(2);
  });

  it('ignores changes of other tutors', async () => {
    const { calls } = mockProfile(() => [slot('a', start, 1)]);
    renderAt(`/tutors/${TUTOR}`);
    const ws = await connected();
    await screen.findAllByRole('button', { name: /^Book / });
    await settle();
    const before = calls.filter((c) => c.url.includes('/availability?')).length;
    act(() => ws.receive(event('availability.changed', { entityId: OTHER })));
    await settle();
    expect(calls.filter((c) => c.url.includes('/availability?')).length).toBe(before);
  });

  it('keeps an open booking form when its time disappears, explains it, and recovers', async () => {
    let slots = [slot('a', start, 2), slot('b', start + 24 * HOUR, 1)];
    mockProfile(() => slots);
    renderAt(`/tutors/${TUTOR}`);
    const user = userEvent.setup();
    const ws = await connected();

    await user.click((await screen.findAllByRole('button', { name: /^Book / }))[0]);
    await user.type(screen.getByLabelText('What do you need help with?'), 'Kept text');

    // "a" is no longer free (booked elsewhere, or removed by the tutor).
    slots = [slot('b', start + 24 * HOUR, 1)];
    act(() => ws.receive(event('availability.changed', { entityId: TUTOR })));
    await settle();

    const form = screen.getByRole('region', { name: /^Book Tom Tutor/ });
    expect(within(form).getByText('This time is no longer free')).toBeInTheDocument();
    expect(within(form).queryByText(/booked by|reserved/i)).not.toBeInTheDocument(); // never claims who took it
    expect(within(form).getByRole('button', { name: 'Send request' })).toBeDisabled();
    expect(within(form).getByLabelText('What do you need help with?')).toHaveValue('Kept text');

    // Choosing another current free time keeps the text and allows sending again.
    await user.click(screen.getByRole('button', { name: /^Book / }));
    const again = screen.getByRole('region', { name: /^Book Tom Tutor/ });
    expect(within(again).queryByText('This time is no longer free')).not.toBeInTheDocument();
    expect(within(again).getByLabelText('What do you need help with?')).toHaveValue('Kept text');
    expect(within(again).getByRole('button', { name: 'Send request' })).toBeEnabled();
  });

  it('narrows the start times when only part of the chosen time was taken', async () => {
    let slots = [slot('a', start, 3)];
    mockProfile(() => slots);
    renderAt(`/tutors/${TUTOR}`);
    const user = userEvent.setup();
    const ws = await connected();

    await user.click(await screen.findByRole('button', { name: /^Book / }));
    expect(within(screen.getByLabelText('Start')).getAllByRole('option')).toHaveLength(5); // 60 min in 3 h, every 30 min

    slots = [slot('a', start, 1)]; // the last two hours were booked
    act(() => ws.receive(event('availability.changed', { entityId: TUTOR })));
    await settle();

    expect(within(screen.getByLabelText('Start')).getAllByRole('option')).toHaveLength(1);
    expect(screen.queryByText('This time is no longer free')).not.toBeInTheDocument();
  });

  it('registers the watch again and refetches after a reconnection', async () => {
    const { calls } = mockProfile(() => [slot('a', start, 1)]);
    renderAt(`/tutors/${TUTOR}`);
    const ws = await connected();
    await screen.findAllByRole('button', { name: /^Book / });
    await settle();
    const watches = () => calls.filter((c) => c.url === '/api/v1/realtime/availability-watches').length;
    const loads = () => calls.filter((c) => c.url.includes('/availability?')).length;
    const [watchesBefore, loadsBefore] = [watches(), loads()];

    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    act(() => ws.drop(1006));
    act(() => vi.advanceTimersByTime(30_000));
    vi.useRealTimers();
    act(() => openSocket().open());
    await settle();

    expect(watches()).toBeGreaterThan(watchesBefore);
    expect(loads()).toBeGreaterThan(loadsBefore);
  });
});

describe('tutor search, live', () => {
  it('updates a visible tutor’s rating', async () => {
    let rating = { average_rating: null as number | null, rating_count: 0 };
    mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'GET /api/v1/subjects/sub-1': () => json(200, { data: { id: 'sub-1', code: 'ALG', name: 'Algorithms' } }),
      'GET /api/v1/faculties': () => json(200, { data: [] }),
      'GET /api/v1/tutors': () =>
        json(200, { data: [{ ...PROFILE, ...rating, offering: OFFERING }], pagination: { limit: 20, offset: 0, has_more: false } }),
      'POST /api/v1/realtime/availability-watches': () => json(200, { expires_at: new Date().toISOString() }),
    });
    renderAt('/tutors?subject_id=sub-1');
    const ws = await connected();
    expect(await screen.findByText('Algorithms · No ratings yet')).toBeInTheDocument();

    rating = { average_rating: 5, rating_count: 1 };
    act(() => ws.receive(event('activity.changed', { entityId: TUTOR })));
    await settle();
    expect(screen.getByText('Algorithms · 5.0 ★ (1 rating)')).toBeInTheDocument();
  });
});

describe('tutor availability page, live', () => {
  const tutorUser = { ...verifiedStudent, id: TUTOR, roles: ['student', 'tutor'] };
  const BASE = '/api/v1/tutors/me/availability';

  it('shows a change made elsewhere and keeps the tutor’s own confirmation', async () => {
    let windows = [slot('a', start, 1), slot('b', start + 24 * HOUR, 1)];
    mockApi({
      'GET /api/v1/auth/me': () => json(200, tutorUser),
      [`GET ${BASE}`]: () => page(windows),
      [`GET ${BASE}/rules`]: () => page([]),
      'GET /api/v1/semesters': () => json(200, { data: [] }),
      [`DELETE ${BASE}/slots/a`]: () => {
        windows = windows.filter((w) => w.id !== 'a');
        return json(204);
      },
    });
    renderAt('/tutor/availability');
    const user = userEvent.setup();
    const ws = await connected();
    await screen.findAllByText('Online · One-off');

    // The tutor removes "a" ...
    await user.click(screen.getAllByRole('button', { name: /^Remove/ })[0]);
    await user.click(screen.getByRole('button', { name: 'Yes, remove' }));
    expect(await screen.findByText('Removed.')).toBeInTheDocument();

    // ... the server echoes it, and a change from another tab arrives: the list updates, the notice stays.
    windows = [...windows, slot('c', start + 48 * HOUR, 1)];
    act(() => ws.receive(event('availability.changed', { entityId: TUTOR })));
    await settle();
    expect(screen.getAllByText('Online · One-off')).toHaveLength(2);
    expect(screen.getByText('Removed.')).toBeInTheDocument();
  });
});
