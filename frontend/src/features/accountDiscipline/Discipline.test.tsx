import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { AuthProvider } from '../auth/AuthProvider';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Flag review, suspension and account history (#56, FR18/FR19, UC14/UC16, NFR12).

const FACULTY_ID = '55555555-5555-5555-5555-555555555555';
const USER_ID = '0a1b2c3d-4e5f-6789-abcd-ef0123456789';
const USER = `/api/v1/coordinator/users/${USER_ID}`;
const HISTORY = `${USER}/audit-history`;
const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY_ID,
};

const page = (data: unknown[]) => json(200, { data, pagination: { limit: 25, offset: 0, has_more: false } });
const base = { user_id: USER_ID, faculty_id: FACULTY_ID };
const suspensionEvent = (suspended: boolean, reason: string) => ({
  ...base,
  id: `s-${suspended}`,
  category: 'SUSPENSION',
  recorded_at: '2026-11-25T10:00:00Z',
  actor_kind: 'USER',
  actor_user_id: coordinator.id,
  was_suspended: !suspended,
  is_suspended: suspended,
  reason,
});
const violations = [
  { ...base, id: 'v-2', category: 'VIOLATION', recorded_at: '2026-11-20T10:00:00Z', actor_kind: 'USER', actor_user_id: coordinator.id, violation_kind: 'NO_SHOW', semester_id: 'abcdabcd-0000-0000-0000-000000000000', session_id: 'ccccdddd-0000-0000-0000-000000000000', occurred_at: '2026-11-18T14:00:00Z' },
  { ...base, id: 'v-1', category: 'VIOLATION', recorded_at: '2026-11-10T10:00:00Z', actor_kind: 'SYSTEM', actor_user_id: null, violation_kind: 'LATE_CANCELLATION', semester_id: 'abcdabcd-0000-0000-0000-000000000000', occurred_at: '2026-11-10T10:00:00Z' },
];
const flags = [
  { ...base, id: 'f-1', category: 'FLAG', recorded_at: '2026-11-20T10:00:01Z', actor_kind: 'SYSTEM', actor_user_id: null, semester_id: 'abcdabcd-0000-0000-0000-000000000000', first_violation_id: '11112222-0000-0000-0000-000000000000', second_violation_id: '33334444-0000-0000-0000-000000000000' },
];

/** History by category; `suspensions` is read at request time so a test can change it. */
function historyRoute(state: { suspensions: unknown[] }) {
  return ({ url }: MockCall) => {
    const category = new URL(url, 'http://x').searchParams.get('category');
    return page(category === 'SUSPENSION' ? state.suspensions : category === 'FLAG' ? flags : violations);
  };
}

function mockCoordinator(routes: Record<string, (call: MockCall) => Response>) {
  return mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), ...routes });
}

function renderAt(path: string) {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
    </AuthProvider>,
  );
}

const posts = (calls: MockCall[]) => calls.filter((c) => c.method === 'POST');

afterEach(() => vi.unstubAllGlobals());

describe('Flagged accounts', () => {
  it('lists flags of the faculty with the violation count and current state', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/flagged-users': () =>
        page([
          { flag_id: 'f-1', user_id: USER_ID, semester_id: 'abcdabcd-0000-0000-0000-000000000000', strike_count: 3, flagged_at: '2026-11-20T10:00:00Z', is_suspended: false },
          { flag_id: 'f-2', user_id: 'ffff0000-0000-0000-0000-000000000000', semester_id: 'abcdabcd-0000-0000-0000-000000000000', strike_count: 2, flagged_at: '2026-11-19T10:00:00Z', is_suspended: true },
        ]),
    });
    renderAt('/coordinator/flags');

    const [first, second] = within(await screen.findByRole('list', { name: 'Flagged accounts' })).getAllByRole('listitem');
    expect(within(first).getByRole('link', { name: 'Account 0A1B2C3D' })).toHaveAttribute('href', `/coordinator/users/${USER_ID}`);
    expect(first).toHaveTextContent('3 violations in semester ABCDABCD');
    expect(within(first).getByText('Not suspended')).toBeInTheDocument();
    expect(within(second).getByText('Suspended')).toBeInTheDocument();
    expect(screen.getByText(/A flag never suspends anyone/)).toBeInTheDocument();
    expect(calls.find((c) => c.url.startsWith('/api/v1/coordinator/flagged-users'))?.url).toBe('/api/v1/coordinator/flagged-users?limit=25&offset=0');
  });

  it('says when no account is flagged', async () => {
    mockCoordinator({ 'GET /api/v1/coordinator/flagged-users': () => page([]) });
    renderAt('/coordinator/flags');

    expect(await screen.findByText('No accounts of your faculty are flagged.')).toBeInTheDocument();
  });

  it('is only for coordinators', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/coordinator/flags');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/coordinator/'))).toBe(false);
  });
});

describe('One account', () => {
  it('shows violations and flags without names or evidence', async () => {
    const { calls } = mockCoordinator({ [`GET ${HISTORY}`]: historyRoute({ suspensions: [] }) });
    renderAt(`/coordinator/users/${USER_ID}`);
    const user = userEvent.setup();

    expect(await screen.findByText('Your faculty has not suspended this account.')).toBeInTheDocument();
    const list = await screen.findByRole('list', { name: 'Violations' });
    const [noShow, late] = within(list).getAllByRole('listitem');
    expect(noShow).toHaveTextContent('Confirmed no-show');
    expect(noShow).toHaveTextContent('Recorded by coordinator ref. 44444444');
    expect(noShow).toHaveTextContent('Session CCCCDDDD');
    expect(late).toHaveTextContent('Late cancellation (less than 24 hours before)');
    expect(late).toHaveTextContent('Recorded automatically');

    await user.selectOptions(screen.getByLabelText('Show'), 'Flags');
    expect(await screen.findByText('Flagged for semester ABCDABCD')).toBeInTheDocument();
    expect(screen.getByText('After violations 11112222 and 33334444.')).toBeInTheDocument();
    const categories = calls.filter((c) => c.url.startsWith(`${HISTORY}?`)).map((c) => new URL(c.url, 'http://x').searchParams.get('category'));
    expect(categories).toEqual(expect.arrayContaining(['SUSPENSION', 'VIOLATION', 'FLAG']));
  });

  it('suspends only with a reason and after a second confirmation', async () => {
    const state = { suspensions: [] as unknown[] };
    const { calls } = mockCoordinator({
      [`GET ${HISTORY}`]: historyRoute(state),
      [`POST ${USER}/suspend`]: ({ body }) => {
        state.suspensions = [suspensionEvent(true, (body as { reason: string }).reason)];
        return json(200, { data: { user_id: USER_ID, decision_id: 'd-1', is_suspended: true, changed_at: '2026-11-26T10:00:00Z', cancelled_sessions: 2 } });
      },
    });
    renderAt(`/coordinator/users/${USER_ID}`);
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { level: 1, name: 'Account 0A1B2C3D' })).toBeInTheDocument();
    await user.click(await screen.findByRole('button', { name: 'Suspend account' }));
    expect(screen.getByText('Give a reason; it is kept in the account’s history.')).toBeInTheDocument();
    expect(screen.getByLabelText('Reason for suspending')).toHaveFocus();

    await user.type(screen.getByLabelText('Reason for suspending'), '  Three confirmed no-shows this semester.  ');
    await user.click(screen.getByRole('button', { name: 'Suspend account' }));
    const question = screen.getByRole('group', { name: 'Suspend this account?' });
    expect(within(question).getByRole('button', { name: 'Yes, suspend' })).toHaveFocus();
    expect(posts(calls)).toHaveLength(0);
    await user.click(within(question).getByRole('button', { name: 'Yes, suspend' }));

    expect(await screen.findByText(/Account suspended\. 2 future sessions were cancelled/)).toBeInTheDocument();
    expect(await screen.findByText(/Last decision by your faculty: suspended on/)).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Reinstate account' })).toBeInTheDocument();
    expect(posts(calls)).toEqual([{ method: 'POST', url: `${USER}/suspend`, body: { reason: 'Three confirmed no-shows this semester.' } }]);
  });

  it('reinstates a suspended account with a reason', async () => {
    const state = { suspensions: [suspensionEvent(true, 'Three confirmed no-shows.')] as unknown[] };
    const { calls } = mockCoordinator({
      [`GET ${HISTORY}`]: historyRoute(state),
      [`POST ${USER}/reinstate`]: ({ body }) => {
        state.suspensions = [suspensionEvent(false, (body as { reason: string }).reason)];
        return json(200, { data: { user_id: USER_ID, decision_id: 'd-2', is_suspended: false, changed_at: '2026-11-26T11:00:00Z', cancelled_sessions: 0 } });
      },
    });
    renderAt(`/coordinator/users/${USER_ID}`);
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Reason for reinstating'), 'Talked to the student; agreed on attendance.');
    await user.click(screen.getByRole('button', { name: 'Reinstate account' }));

    expect(await screen.findByText(/Account reinstated\./)).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Suspend account' })).toBeInTheDocument();
    expect(posts(calls)).toEqual([{ method: 'POST', url: `${USER}/reinstate`, body: { reason: 'Talked to the student; agreed on attendance.' } }]);
  });

  it('explains that only the current home faculty can decide (404)', async () => {
    mockCoordinator({
      [`GET ${HISTORY}`]: historyRoute({ suspensions: [] }),
      [`POST ${USER}/suspend`]: () => json(404, { error: 'not found' }),
    });
    renderAt(`/coordinator/users/${USER_ID}`);
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Reason for suspending'), 'Repeated violations.');
    await user.click(screen.getByRole('button', { name: 'Suspend account' }));
    await user.click(screen.getByRole('button', { name: 'Yes, suspend' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('current home faculty');
    expect(alert.parentElement).toHaveFocus();
    expect(screen.getByLabelText('Reason for suspending')).toHaveValue('Repeated violations.');
  });

  it('reloads when the state changed meanwhile (409)', async () => {
    const state = { suspensions: [] as unknown[] };
    mockCoordinator({
      [`GET ${HISTORY}`]: historyRoute(state),
      [`POST ${USER}/suspend`]: () => {
        state.suspensions = [suspensionEvent(true, 'Suspended by a colleague.')];
        return json(409, { error: 'conflict' });
      },
    });
    renderAt(`/coordinator/users/${USER_ID}`);
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Reason for suspending'), 'Repeated violations.');
    await user.click(screen.getByRole('button', { name: 'Suspend account' }));
    await user.click(screen.getByRole('button', { name: 'Yes, suspend' }));

    expect(await screen.findByText(/The account’s state changed meanwhile/)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reinstate account' })).toBeInTheDocument());
  });
});
