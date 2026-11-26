import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RealtimeProvider } from '../app/RealtimeProvider';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { event, FakeWebSocket } from '../test/fakeWebSocket';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests for live booking updates (#91) with a fake WebSocket and a mocked
// HTTP API (test doubles). The two-browser real-backend journey is in tests/e2e/live-booking.spec.ts.

const HOUR = 3_600_000;
const tutor = { ...verifiedStudent, roles: ['student', 'tutor'] };
const QUEUE = '/api/v1/tutors/me/session-requests';
const page = <T,>(data: T[]) => json(200, { data, pagination: { limit: 50, offset: 0, has_more: false } });

function session(id: string, subject: string, overrides: Record<string, unknown> = {}) {
  const start = Date.now() + 48 * HOUR;
  return {
    id,
    student_id: 'stu-1',
    tutor_id: tutor.id,
    subject_id: 'sub-1',
    subject_name: subject,
    offering_id: 'off-1',
    semester_id: 'sem-1',
    starts_at: new Date(start).toISOString(),
    ends_at: new Date(start + HOUR).toISOString(),
    duration_minutes: 60,
    mode: 'ONLINE',
    purpose: 'HOMEWORK',
    description: `${subject} help.`,
    status: 'REQUESTED',
    requested_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 24 * HOUR).toISOString(),
    is_late_cancellation: false,
    viewer_role: 'TUTOR',
    ...overrides,
  };
}

function renderAt(path: string) {
  render(
    <AuthProvider>
      <RealtimeProvider>
        <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
      </RealtimeProvider>
    </AuthProvider>,
  );
}

async function connected(): Promise<FakeWebSocket> {
  await waitFor(() => expect(FakeWebSocket.instances.filter((s) => s.readyState !== 3)).toHaveLength(1));
  const ws = FakeWebSocket.instances.filter((s) => s.readyState !== 3)[0];
  act(() => ws.open());
  return ws;
}

/** Lets the hook's debounce run and the refetch settle. */
const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 400)));

beforeEach(() => {
  FakeWebSocket.reset();
  vi.stubGlobal('WebSocket', FakeWebSocket);
});
afterEach(() => vi.unstubAllGlobals());

describe('Teaching page, live', () => {
  it('shows a new request without a reload and keeps a half-written answer', async () => {
    let requested = [session('ses-a', 'Algorithms')];
    mockApi({
      'GET /api/v1/auth/me': () => json(200, tutor),
      [`GET ${QUEUE}`]: ({ url }: MockCall) => (url.includes('status=ACCEPTED') ? page([]) : page(requested)),
    });
    renderAt('/tutor');
    const user = userEvent.setup();
    const ws = await connected();
    await settle();

    // The tutor starts declining A ...
    const cardA = (await screen.findByRole('heading', { name: 'Algorithms' })).closest('li')!;
    await user.click(within(cardA).getByRole('button', { name: /^Decline / }));
    await user.type(within(cardA).getByLabelText('Reason for declining'), 'Half-written');

    // ... while a student requests B.
    requested = [...requested, session('ses-b', 'Databases')];
    act(() => ws.receive(event('booking.changed')));
    await settle();

    expect(await screen.findByRole('heading', { name: 'Requests waiting for you (2)' })).toBeInTheDocument();
    expect(screen.getByText(/^New: Databases on /)).toBeInTheDocument();
    // The answer in progress is untouched.
    expect(within(cardA).getByLabelText('Reason for declining')).toHaveValue('Half-written');
  });

  it('removes a request the student cancelled and says why', async () => {
    let requested = [session('ses-a', 'Algorithms')];
    mockApi({
      'GET /api/v1/auth/me': () => json(200, tutor),
      [`GET ${QUEUE}`]: ({ url }: MockCall) => (url.includes('status=ACCEPTED') ? page([]) : page(requested)),
      'GET /api/v1/sessions/ses-a': () => json(200, { data: session('ses-a', 'Algorithms', { status: 'CANCELLED' }) }),
    });
    renderAt('/tutor');
    const ws = await connected();
    await screen.findByRole('heading', { name: 'Algorithms' });

    requested = [];
    act(() => ws.receive(event('booking.changed')));
    await settle();

    expect(await screen.findByRole('heading', { name: 'Requests waiting for you (0)' })).toBeInTheDocument();
    expect(screen.getByText(/^Algorithms on .*: now Cancelled\.$/)).toBeInTheDocument();
  });

  it('refreshes after a conflict, so the action disappears', async () => {
    let requested = [session('ses-a', 'Algorithms')];
    mockApi({
      'GET /api/v1/auth/me': () => json(200, tutor),
      [`GET ${QUEUE}`]: ({ url }: MockCall) => (url.includes('status=ACCEPTED') ? page([]) : page(requested)),
      [`POST ${QUEUE}/ses-a/decline`]: () => {
        requested = []; // the student had cancelled a moment earlier
        return json(409, { error: 'session cannot make this transition' });
      },
      'GET /api/v1/sessions/ses-a': () => json(200, { data: session('ses-a', 'Algorithms', { status: 'CANCELLED' }) }),
    });
    renderAt('/tutor');
    const user = userEvent.setup();
    await connected();

    const card = (await screen.findByRole('heading', { name: 'Algorithms' })).closest('li')!;
    await user.click(within(card).getByRole('button', { name: /^Decline / }));
    await user.type(within(card).getByLabelText('Reason for declining'), 'Busy.');
    await user.click(within(card).getByRole('button', { name: 'Decline the session' }));

    expect(await screen.findByRole('heading', { name: 'Requests waiting for you (0)' })).toBeInTheDocument();
    expect(screen.getByText(/now Cancelled/)).toBeInTheDocument();
  });
});

describe('My sessions, live', () => {
  it('shows the tutor accepting, once, even if the event arrives twice', async () => {
    let status = 'REQUESTED';
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'GET /api/v1/sessions/me': ({ url }: MockCall) => {
        const s = session('ses-a', 'Algorithms', { status, viewer_role: 'STUDENT', meeting_link: status === 'ACCEPTED' ? 'https://meet.example/x' : undefined });
        return page(url.includes(`status=${status}`) ? [s] : []);
      },
    });
    renderAt('/sessions');
    const ws = await connected();
    const card = (await screen.findByRole('heading', { name: 'Algorithms' })).closest('li')!;
    expect(within(card).getByText('Waiting for the tutor')).toBeInTheDocument();
    await settle();
    const before = calls.filter((c) => c.url.startsWith('/api/v1/sessions/me')).length;

    status = 'ACCEPTED';
    const accepted = event('booking.changed');
    act(() => {
      ws.receive(accepted);
      ws.receive(accepted); // repeated delivery
      ws.receive(event('booking.changed')); // and a second event in the same burst
    });
    await settle();

    expect(screen.getAllByRole('heading', { name: 'Algorithms' })).toHaveLength(1);
    const updated = screen.getByRole('heading', { name: 'Algorithms' }).closest('li')!;
    expect(within(updated).getByText('Confirmed')).toBeInTheDocument();
    expect(within(updated).getByRole('link', { name: 'https://meet.example/x' })).toBeInTheDocument();
    expect(screen.getByText(/^Algorithms on .*: now Confirmed\.$/)).toBeInTheDocument();
    // One refetch (two calls: requested + accepted) for the whole burst.
    expect(calls.filter((c) => c.url.startsWith('/api/v1/sessions/me')).length - before).toBe(2);
  });

  it('ignores events of other kinds', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'GET /api/v1/sessions/me': () => page([]),
    });
    renderAt('/sessions');
    const ws = await connected();
    await settle();
    const before = calls.length;
    act(() => ws.receive(event('coordination.changed')));
    await settle();
    expect(calls.length).toBe(before);
  });
});
