import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { AuthProvider } from '../auth/AuthProvider';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Coordinator no-show adjudication (#54, FR17/FR18, UC13).

const FACULTY_ID = '55555555-5555-5555-5555-555555555555';
const ID = '0a1b2c3d-4e5f-6789-abcd-ef0123456789';
const DETAIL = `/api/v1/coordinator/attendance-reports/${ID}`;
const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY_ID,
};

function report(overrides: Record<string, unknown> = {}) {
  return {
    id: ID,
    session_id: 'ccccdddd-1111-2222-3333-444455556666',
    reporter_user_id: 'aaaabbbb-1111-2222-3333-444455556666',
    absent_user_id: 'eeeeffff-1111-2222-3333-444455556666',
    status: 'PENDING',
    description: 'I waited 30 minutes in the online meeting.\nNobody joined.',
    reported_at: '2026-11-20T15:30:00Z',
    ...overrides,
  };
}

const page = (data: unknown[]) => json(200, { data, pagination: { limit: 25, offset: 0, has_more: false } });

function mockCoordinator(routes: Record<string, (call: MockCall) => Response>) {
  return mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinator), ...routes });
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  );
  return router;
}

const listQueries = (calls: MockCall[]) =>
  calls
    .filter((c) => c.url.startsWith('/api/v1/coordinator/attendance-reports?'))
    .map((c) => new URL(c.url, 'http://x').searchParams.toString());
const posts = (calls: MockCall[]) => calls.filter((c) => c.method === 'POST');

afterEach(() => vi.unstubAllGlobals());

describe('No-show report list', () => {
  it('lists reports waiting for a decision, oldest first', async () => {
    const { calls } = mockCoordinator({ 'GET /api/v1/coordinator/attendance-reports': () => page([report()]) });
    renderAt('/coordinator/no-show-reports');

    const list = await screen.findByRole('list', { name: 'No-show reports waiting for a decision' });
    const [item] = within(list).getAllByRole('listitem');
    expect(within(item).getByRole('link', { name: 'No-show report 0A1B2C3D' })).toHaveAttribute('href', `/coordinator/no-show-reports/${ID}`);
    expect(item).toHaveTextContent('Session CCCCDDDD · Reported');
    expect(within(item).getByText('I waited 30 minutes in the online meeting.…')).toBeInTheDocument();
    expect(screen.getByText(/General problem reports are separate/)).toBeInTheDocument();
    expect(listQueries(calls)).toEqual(['status=PENDING&limit=25&offset=0']);
  });

  it('switches to confirmed reports and keeps the filter in the URL', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/attendance-reports': ({ url }) =>
        url.includes('status=CONFIRMED') ? page([report({ status: 'CONFIRMED', reviewed_at: '2026-11-21T10:00:00Z' })]) : page([]),
    });
    const router = renderAt('/coordinator/no-show-reports');
    const user = userEvent.setup();

    expect(await screen.findByText('No no-show reports are waiting for a decision.')).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText('Show reports'), 'Confirmed');

    const list = await screen.findByRole('list', { name: 'Confirmed no-show reports' });
    expect(within(list).getByText('Confirmed')).toBeInTheDocument();
    expect(router.state.location.search).toBe('?status=confirmed');
    await waitFor(() => expect(listQueries(calls).at(-1)).toBe('status=CONFIRMED&limit=25&offset=0'));
  });

  it('is only for coordinators', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/coordinator/no-show-reports');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/coordinator/'))).toBe(false);
  });
});

describe('Deciding one report', () => {
  it('confirms only with a note and after a second confirmation', async () => {
    const { calls } = mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, { data: report() }),
      [`POST ${DETAIL}/confirm`]: ({ body }) =>
        json(200, { data: report({ status: 'CONFIRMED', reviewed_at: '2026-11-26T10:00:00Z', resolution_note: (body as { resolution_note: string }).resolution_note }) }),
    });
    renderAt(`/coordinator/no-show-reports/${ID}`);
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { level: 1, name: 'No-show report 0A1B2C3D' })).toBeInTheDocument();
    expect(screen.getByText('Said to be absent').nextElementSibling).toHaveTextContent('Participant ref. EEEEFFFF');
    expect(screen.getByText(/This is an allegation, not a fact/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Confirm no-show' }));
    expect(screen.getByText('Explain your decision; it is stored with the report.')).toBeInTheDocument();
    expect(screen.getByLabelText('Decision note')).toHaveFocus();

    await user.type(screen.getByLabelText('Decision note'), '  The tutor sent a screenshot of the empty meeting.  ');
    await user.click(screen.getByRole('button', { name: 'Confirm no-show' }));
    const question = screen.getByRole('group', { name: 'Confirm the no-show?' });
    expect(within(question).getByRole('button', { name: 'Yes, confirm no-show' })).toHaveFocus();
    await user.click(within(question).getByRole('button', { name: 'Go back' }));
    expect(posts(calls)).toHaveLength(0);

    await user.click(screen.getByRole('button', { name: 'Confirm no-show' }));
    await user.click(screen.getByRole('button', { name: 'Yes, confirm no-show' }));

    expect(await screen.findByText('No-show confirmed. A violation was recorded for the absent participant.')).toBeInTheDocument();
    expect(screen.getByText('Status').nextElementSibling).toHaveTextContent('Confirmed');
    expect(screen.getByText('The tutor sent a screenshot of the empty meeting.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject report' })).not.toBeInTheDocument();
    expect(posts(calls)).toEqual([
      { method: 'POST', url: `${DETAIL}/confirm`, body: { resolution_note: 'The tutor sent a screenshot of the empty meeting.' } },
    ]);
  });

  it('rejects with a note and leaves the session as it is', async () => {
    const { calls } = mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, { data: report() }),
      [`POST ${DETAIL}/reject`]: ({ body }) =>
        json(200, { data: report({ status: 'REJECTED', reviewed_at: '2026-11-26T10:00:00Z', resolution_note: (body as { resolution_note: string }).resolution_note }) }),
    });
    renderAt(`/coordinator/no-show-reports/${ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Reject report' }));
    expect(screen.getByText('Explain your decision; it is stored with the report.')).toBeInTheDocument();
    expect(posts(calls)).toHaveLength(0);

    await user.type(screen.getByLabelText('Decision note'), 'The student joined late but did attend.');
    await user.click(screen.getByRole('button', { name: 'Reject report' }));

    expect(await screen.findByText('Report rejected. The session’s outcome stays as it was.')).toBeInTheDocument();
    expect(screen.getByText('Status').nextElementSibling).toHaveTextContent('Rejected');
    expect(posts(calls)).toEqual([{ method: 'POST', url: `${DETAIL}/reject`, body: { resolution_note: 'The student joined late but did attend.' } }]);
  });

  it('shows the current state when the decision is no longer possible (409)', async () => {
    let decided = false;
    mockCoordinator({
      [`GET ${DETAIL}`]: () =>
        json(200, { data: decided ? report({ status: 'REJECTED', reviewed_at: '2026-11-26T09:00:00Z', resolution_note: 'Decided earlier.' }) : report() }),
      [`POST ${DETAIL}/confirm`]: () => {
        decided = true;
        return json(409, { error: 'attendance report already exists, was decided or conflicts with the session outcome' });
      },
    });
    renderAt(`/coordinator/no-show-reports/${ID}`);
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Decision note'), 'Confirming.');
    await user.click(screen.getByRole('button', { name: 'Confirm no-show' }));
    await user.click(screen.getByRole('button', { name: 'Yes, confirm no-show' }));

    expect(await screen.findByText(/This report was already decided/)).toBeInTheDocument();
    expect(await screen.findByText('Decided earlier.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Decision note')).not.toBeInTheDocument();
  });

  it('explains a report about another faculty’s session', async () => {
    mockCoordinator({ [`GET ${DETAIL}`]: () => json(404, { error: 'attendance resource not found' }) });
    renderAt(`/coordinator/no-show-reports/${ID}`);

    expect(await screen.findByText('This report doesn’t exist or concerns a session of another faculty.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '← All no-show reports' })).toHaveAttribute('href', '/coordinator/no-show-reports');
  });
});
