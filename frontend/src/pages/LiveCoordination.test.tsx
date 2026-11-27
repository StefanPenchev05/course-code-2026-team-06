import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { RealtimeProvider } from '../app/RealtimeProvider';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { event, FakeWebSocket } from '../test/fakeWebSocket';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests for live coordinator, applicant and tutor views (#93) with a fake WebSocket
// and a mocked HTTP API (TEST DOUBLES, not integration evidence). One test per event family
// and per access change; real-backend journeys are #94.

const FACULTY = '55555555-5555-5555-5555-555555555555';
const OTHER_FACULTY = '66666666-6666-6666-6666-666666666666';
const ID = '0a1b2c3d-4e5f-6789-abcd-ef0123456789';
const OTHER_ID = 'fedcba98-7654-4321-8fed-cba987654321';
const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY,
};
const tutor = { ...verifiedStudent, roles: ['student', 'tutor'] };

const page = (data: unknown[], hasMore = false) => json(200, { data, pagination: { limit: 25, offset: 0, has_more: hasMore } });

/** Answers a list endpoint like the API: slices by the limit and offset in the query. */
function paged(items: () => unknown[]) {
  return ({ url }: MockCall) => {
    const query = new URL(url, 'http://x').searchParams;
    const limit = Number(query.get('limit') ?? 25);
    const offset = Number(query.get('offset') ?? 0);
    const all = items();
    return json(200, { data: all.slice(offset, offset + limit), pagination: { limit, offset, has_more: offset + limit < all.length } });
  };
}

const gets = (calls: MockCall[], path: string) => calls.filter((c) => c.method === 'GET' && c.url.split('?')[0] === path).length;

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
  await settle();
  return ws;
}

/** Lets the debounce run and the refetch settle. */
const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 400)));

async function send(ws: FakeWebSocket, type: string, entityId?: string) {
  act(() => ws.receive(event(type, entityId ? { entityId } : {})));
  await settle();
}

beforeEach(() => {
  FakeWebSocket.reset();
  vi.stubGlobal('WebSocket', FakeWebSocket);
});
afterEach(() => vi.unstubAllGlobals());

describe('tutor applications (tutor_application.changed)', () => {
  const application = (overrides: Record<string, unknown> = {}) => ({
    id: ID,
    student_id: 'aaaabbbb-1111-2222-3333-444455556666',
    subject_id: 'sub-algo',
    subject_name: 'Algorithms',
    status: 'PENDING',
    submitted_at: '2026-11-20T09:30:00Z',
    ...overrides,
  });

  it('removes an application decided elsewhere from the coordinator queue', async () => {
    let queue = [application()];
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), 'GET /api/v1/coordinator/tutor-applications': () => page(queue) });
    renderAt('/coordinator/tutor-applications');
    expect(await screen.findByRole('link', { name: 'Algorithms' })).toBeInTheDocument();
    const ws = await connected();

    queue = []; // approved in another browser
    await send(ws, 'tutor_application.changed', ID);

    expect(screen.getByText('No applications are waiting for review.')).toBeInTheDocument();
    expect(screen.getByText('Updated: 1 application is no longer in this list.')).toBeInTheDocument();
  });

  it('shows the decision to the applicant and offers the form again', async () => {
    let mine = [application()];
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent), 'GET /api/v1/tutor-applications/me': () => page(mine) });
    renderAt('/tutor-applications');
    expect(await screen.findByText('You have an application waiting for review')).toBeInTheDocument();
    const ws = await connected();

    mine = [application({ status: 'APPROVED', reviewed_at: '2026-11-21T10:00:00Z' })];
    await send(ws, 'tutor_application.changed', ID);

    expect(screen.getByText('This subject is now on your tutor profile.')).toBeInTheDocument();
    expect(screen.queryByText('You have an application waiting for review')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Subject')).toBeInTheDocument();
  });

  it('keeps a half-written rejection reason, then shows a decision made elsewhere', async () => {
    const DETAIL = `/api/v1/coordinator/tutor-applications/${ID}`;
    let detail: Record<string, unknown> = { ...application(), grade: '17.50', description: 'I loved this course.' };
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), [`GET ${DETAIL}`]: () => json(200, { data: detail }) });
    renderAt(`/coordinator/tutor-applications/${ID}`);
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Reject' }));
    await user.type(screen.getByLabelText('Reason for rejecting'), 'Half-written');
    const ws = await connected();
    const before = gets(calls, DETAIL);

    await send(ws, 'tutor_application.changed', OTHER_ID); // another application: no refetch
    expect(gets(calls, DETAIL)).toBe(before);

    await send(ws, 'tutor_application.changed', ID); // this one, still pending
    expect(gets(calls, DETAIL)).toBe(before + 1);
    expect(screen.getByLabelText('Reason for rejecting')).toHaveValue('Half-written');

    detail = { ...detail, status: 'APPROVED', reviewed_at: '2026-11-21T10:00:00Z' };
    await send(ws, 'tutor_application.changed', ID);

    expect(screen.getByText('Decided in the meantime')).toBeInTheDocument();
    expect(screen.queryByLabelText('Reason for rejecting')).not.toBeInTheDocument();
  });
});

describe('coordinator cases (coordination.changed)', () => {
  const issue = (n: number, overrides: Record<string, unknown> = {}) => ({
    id: `${String(n).padStart(8, '0')}-0000-4000-8000-000000000000`,
    reporter_user_id: 'aaaabbbb-1111-2222-3333-444455556666',
    faculty_id: FACULTY,
    description: `Problem number ${n}.`,
    status: 'OPEN',
    created_at: '2026-11-20T09:30:00Z',
    ...overrides,
  });

  it('adds a new problem report and keeps the "Show more" results', async () => {
    let open = Array.from({ length: 30 }, (_, i) => issue(i + 1));
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), 'GET /api/v1/coordinator/issues': paged(() => open) });
    renderAt('/coordinator/issues');
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: 'Show more reports' }));
    const list = () => screen.getByRole('list', { name: 'Open reports' });
    await waitFor(() => expect(within(list()).getAllByRole('link')).toHaveLength(30));
    const ws = await connected();

    open = [...open, issue(31)];
    await send(ws, 'coordination.changed', open[30].id);

    expect(within(list()).getAllByRole('link')).toHaveLength(31);
    expect(screen.getByText('Updated: 1 new report.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Show more reports' })).not.toBeInTheDocument();
  });

  it('keeps a half-typed note on a report, then shows a resolution made elsewhere', async () => {
    const DETAIL = `/api/v1/coordinator/issues/${ID}`;
    let detail: Record<string, unknown> = issue(1, { id: ID });
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), [`GET ${DETAIL}`]: () => json(200, { data: detail }) });
    renderAt(`/coordinator/issues/${ID}`);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText('Resolution note'), 'Half-typed');
    const ws = await connected();

    await send(ws, 'coordination.changed', ID);
    expect(screen.getByLabelText('Resolution note')).toHaveValue('Half-typed');

    detail = { ...detail, status: 'RESOLVED', resolved_at: '2026-11-21T10:00:00Z', resolution_note: 'Sent a new link.' };
    await send(ws, 'coordination.changed', ID);

    expect(screen.getByText('Resolved in the meantime')).toBeInTheDocument();
    expect(screen.getByText('Sent a new link.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Resolution note')).not.toBeInTheDocument();
  });

  it('drops a report that is no longer visible to this coordinator', async () => {
    const DETAIL = `/api/v1/coordinator/issues/${ID}`;
    let visible = true;
    mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinator),
      [`GET ${DETAIL}`]: () => (visible ? json(200, { data: issue(1, { id: ID, description: 'Private details.' }) }) : json(404, { error: 'not found' })),
    });
    renderAt(`/coordinator/issues/${ID}`);
    expect(await screen.findByText('Private details.')).toBeInTheDocument();
    const ws = await connected();

    visible = false; // e.g. moved to another faculty
    await send(ws, 'coordination.changed', ID);

    expect(screen.getByText('This report doesn’t exist or belongs to another faculty.')).toBeInTheDocument();
    expect(screen.queryByText('Private details.')).not.toBeInTheDocument();
  });

  it('removes a no-show report decided elsewhere from the queue', async () => {
    let pending = [
      {
        id: ID,
        session_id: 'ccccdddd-1111-2222-3333-444455556666',
        reporter_user_id: 'aaaabbbb-1111-2222-3333-444455556666',
        absent_user_id: 'eeeeffff-1111-2222-3333-444455556666',
        status: 'PENDING',
        description: 'Nobody joined.',
        reported_at: '2026-11-20T15:30:00Z',
      },
    ];
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), 'GET /api/v1/coordinator/attendance-reports': () => page(pending) });
    renderAt('/coordinator/no-show-reports');
    expect(await screen.findByRole('list', { name: 'No-show reports waiting for a decision' })).toBeInTheDocument();
    const ws = await connected();

    pending = [];
    await send(ws, 'coordination.changed', ID);

    expect(screen.getByText('No no-show reports are waiting for a decision.')).toBeInTheDocument();
    expect(screen.getByText('Updated: 1 no-show report is no longer in this list.')).toBeInTheDocument();
  });

  it('adds a new registration to the verification queue', async () => {
    const registration = (id: string, name: string) => ({
      id,
      student_id: 'aaaabbbb-1111-2222-3333-444455556666',
      faculty_id: FACULTY,
      name,
      email: `${name.split(' ')[0].toLowerCase()}@uni.lu`,
      student_number: '0123456789',
      status: 'PENDING',
      submitted_at: '2026-11-20T09:30:00Z',
    });
    let rows = [registration(ID, 'Ana Student')];
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), 'GET /api/v1/coordinator/verifications': () => json(200, rows) });
    renderAt('/coordinator/verifications');
    expect(await screen.findByRole('link', { name: 'Ana Student' })).toBeInTheDocument();
    const ws = await connected();

    rows = [...rows, registration(OTHER_ID, 'Ben Newcomer')];
    await send(ws, 'coordination.changed', OTHER_ID);

    expect(screen.getByRole('link', { name: 'Ben Newcomer' })).toBeInTheDocument();
    expect(screen.getByText('Updated: 1 new registration.')).toBeInTheDocument();
  });

  it('keeps a typed reason on an account, then shows a suspension made elsewhere', async () => {
    const HISTORY = `/api/v1/coordinator/users/${ID}/audit-history`;
    let suspensions: unknown[] = [];
    mockApi({
      'GET /api/v1/auth/me': () => json(200, coordinator),
      [`GET ${HISTORY}`]: ({ url }: MockCall) => page(new URL(url, 'http://x').searchParams.get('category') === 'SUSPENSION' ? suspensions : []),
    });
    renderAt(`/coordinator/users/${ID}`);
    const user = userEvent.setup();
    await user.type(await screen.findByLabelText('Reason for suspending'), 'Half-typed');
    const ws = await connected();

    await send(ws, 'coordination.changed', OTHER_ID); // e.g. a new flag: the state is the same
    expect(screen.getByLabelText('Reason for suspending')).toHaveValue('Half-typed');

    suspensions = [
      {
        id: 'dec-1',
        user_id: ID,
        faculty_id: FACULTY,
        category: 'SUSPENSION',
        recorded_at: '2026-11-25T10:00:00Z',
        actor_kind: 'USER',
        actor_user_id: coordinator.id,
        was_suspended: false,
        is_suspended: true,
      },
    ];
    await send(ws, 'coordination.changed', OTHER_ID);

    expect(screen.getByText('This account was suspended elsewhere while you had it open, for example in another tab.')).toBeInTheDocument();
    expect(screen.getByLabelText('Reason for reinstating')).toHaveValue('');
  });
});

describe('teaching activity (activity.changed)', () => {
  const HOUR = 3_600_000;
  const activity = (completed: number, ratings: number) => ({
    average_rating: 4.5,
    rating_count: ratings,
    completed_sessions: completed,
    hours_taught: completed,
    upcoming_sessions: [],
    pagination: { limit: 50, offset: 0, has_more: false },
  });
  const entry = (id: string, comment: string) => ({
    session_id: id,
    session_status: 'COMPLETED',
    subject_id: 'sub-1',
    subject_name: 'Algorithms',
    starts_at: new Date(Date.now() - 72 * HOUR).toISOString(),
    rating: 5,
    comment,
    created_at: new Date().toISOString(),
  });
  const value = (term: string) => screen.getByText(term).nextElementSibling;

  it('refreshes the tutor’s totals and feedback, but not for other tutors', async () => {
    let totals = activity(4, 3);
    let received = [entry('ses-1', 'Clear and patient.')];
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, tutor),
      'GET /api/v1/tutors/me/activity': () => json(200, { data: totals }),
      'GET /api/v1/tutors/me/feedback': () => page(received),
    });
    renderAt('/tutor/activity');
    expect(await screen.findByText('Clear and patient.')).toBeInTheDocument();
    const ws = await connected();
    const before = gets(calls, '/api/v1/tutors/me/activity');

    await send(ws, 'activity.changed', OTHER_ID); // another tutor
    expect(gets(calls, '/api/v1/tutors/me/activity')).toBe(before);

    totals = activity(5, 4);
    received = [entry('ses-2', 'Great explanation of heaps.'), ...received];
    await send(ws, 'activity.changed', tutor.id);

    expect(value('Completed sessions')).toHaveTextContent('5');
    expect(value('Ratings received')).toHaveTextContent('4');
    expect(screen.getByText('Great explanation of heaps.')).toBeInTheDocument();
  });

  it('refreshes the coordinator’s faculty totals', async () => {
    const statistics = (completed: number) => ({
      faculty_id: FACULTY,
      approved_tutors: 5,
      active_tutors: 4,
      completed_sessions: completed,
      hours_taught: completed,
      average_rating: 4.6,
      rating_count: 9,
    });
    let current = statistics(12);
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), 'GET /api/v1/coordinator/statistics': () => json(200, { data: current }) });
    renderAt('/coordinator');
    await waitFor(() => expect(value('Completed sessions')).toHaveTextContent('12'));
    const ws = await connected();

    current = statistics(13);
    await send(ws, 'activity.changed', OTHER_ID);

    expect(value('Completed sessions')).toHaveTextContent('13');
  });
});

describe('access changes (account.access_changed)', () => {
  const report = (id: string, description: string) => ({
    id,
    reporter_user_id: 'aaaabbbb-1111-2222-3333-444455556666',
    faculty_id: FACULTY,
    description,
    status: 'OPEN',
    created_at: '2026-11-20T09:30:00Z',
  });

  it('removes protected pages at once when the account is suspended', async () => {
    let me: () => Response = () => json(200, coordinator);
    mockApi({ 'GET /api/v1/auth/me': () => me(), 'GET /api/v1/coordinator/issues': () => page([report(ID, 'Faculty report.')]) });
    renderAt('/coordinator/issues');
    expect(await screen.findByText(/Faculty report\./)).toBeInTheDocument();
    const ws = await connected();

    me = () => json(403, { error: 'permission denied' });
    await send(ws, 'account.access_changed');

    expect(screen.getByText("This account can't be used right now")).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(screen.queryByText(/Faculty report\./)).not.toBeInTheDocument();
  });

  it('hides coordinator pages when the coordinator role is removed', async () => {
    let me: object = coordinator;
    mockApi({ 'GET /api/v1/auth/me': () => json(200, me), 'GET /api/v1/coordinator/issues': () => page([report(ID, 'Faculty report.')]) });
    renderAt('/coordinator/issues');
    expect(await screen.findByText(/Faculty report\./)).toBeInTheDocument();
    const ws = await connected();

    me = verifiedStudent;
    await send(ws, 'account.access_changed');

    expect(screen.getByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(screen.queryByText(/Faculty report\./)).not.toBeInTheDocument();
  });

  it('loads coordinator lists again from scratch when the faculty changes', async () => {
    let me = coordinator;
    mockApi({
      'GET /api/v1/auth/me': () => json(200, me),
      // The server scopes the inbox to the coordinator's current faculty.
      'GET /api/v1/coordinator/issues': () =>
        page(me.coordinator_faculty_id === FACULTY ? [report(ID, 'Report of the old faculty.')] : [report(OTHER_ID, 'Report of the new faculty.')]),
    });
    renderAt('/coordinator/issues');
    expect(await screen.findByText(/Report of the old faculty\./)).toBeInTheDocument();
    const ws = await connected();

    me = { ...coordinator, coordinator_faculty_id: OTHER_FACULTY };
    await send(ws, 'account.access_changed');

    expect(await screen.findByText(/Report of the new faculty\./)).toBeInTheDocument();
    expect(screen.queryByText(/Report of the old faculty\./)).not.toBeInTheDocument();
  });
});
