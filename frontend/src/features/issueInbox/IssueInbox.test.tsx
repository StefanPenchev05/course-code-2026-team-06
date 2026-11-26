import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { AuthProvider } from '../auth/AuthProvider';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Coordinator problem-report inbox and resolution (#53, FR17/UC13).

const FACULTY_ID = '55555555-5555-5555-5555-555555555555';
const ID = '0a1b2c3d-4e5f-6789-abcd-ef0123456789';
const DETAIL = `/api/v1/coordinator/issues/${ID}`;
const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY_ID,
};

function issue(overrides: Record<string, unknown> = {}) {
  return {
    id: ID,
    reporter_user_id: 'aaaabbbb-1111-2222-3333-444455556666',
    faculty_id: FACULTY_ID,
    session_id: 'ccccdddd-1111-2222-3333-444455556666',
    description: 'The meeting link did not work.\nThe tutor waited 20 minutes.',
    status: 'OPEN',
    created_at: '2026-11-20T09:30:00Z',
    ...overrides,
  };
}

const page = (data: unknown[], hasMore = false) => json(200, { data, pagination: { limit: 25, offset: 0, has_more: hasMore } });

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

const inboxQueries = (calls: MockCall[]) =>
  calls.filter((c) => c.url.startsWith('/api/v1/coordinator/issues?')).map((c) => new URL(c.url, 'http://x').searchParams.toString());

afterEach(() => vi.unstubAllGlobals());

describe('Problem report inbox', () => {
  it('lists open reports oldest first with a short reference, topic and first line', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/issues': () =>
        page([issue(), issue({ id: 'f0f0f0f0-0000-0000-0000-000000000000', session_id: undefined, description: 'General question about tutoring.' })]),
    });
    renderAt('/coordinator/issues');

    const list = await screen.findByRole('list', { name: 'Open reports' });
    const [first, second] = within(list).getAllByRole('listitem');
    expect(within(first).getByRole('link', { name: 'Report 0A1B2C3D' })).toHaveAttribute('href', `/coordinator/issues/${ID}`);
    expect(first).toHaveTextContent('About a session · Sent');
    expect(within(first).getByText('The meeting link did not work.…')).toBeInTheDocument();
    expect(second).toHaveTextContent('General · Sent');
    expect(screen.getByText(/No-show reports are separate/)).toBeInTheDocument();
    expect(inboxQueries(calls)).toEqual(['status=OPEN&limit=25&offset=0']);
  });

  it('switches to resolved reports and keeps the filter in the URL', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/issues': ({ url }) =>
        url.includes('status=RESOLVED')
          ? page([issue({ status: 'RESOLVED', resolved_at: '2026-11-21T10:00:00Z', resolution_note: 'Sent a new link.' })])
          : page([]),
    });
    const router = renderAt('/coordinator/issues');
    const user = userEvent.setup();

    expect(await screen.findByText(/No open reports/)).toBeInTheDocument();
    await user.selectOptions(screen.getByLabelText('Show reports'), 'Resolved');

    const list = await screen.findByRole('list', { name: 'Resolved reports' });
    expect(within(list).getByText('Resolved')).toBeInTheDocument();
    expect(list).toHaveTextContent('· Resolved');
    expect(router.state.location.search).toBe('?status=resolved');
    await waitFor(() => expect(inboxQueries(calls).at(-1)).toBe('status=RESOLVED&limit=25&offset=0'));
  });

  it('is only for coordinators', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/coordinator/issues');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/coordinator/'))).toBe(false);
  });
});

describe('One problem report', () => {
  it('resolves an open report once, with a required note', async () => {
    let stored: string | null = null;
    const { calls } = mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, { data: issue() }),
      [`POST ${DETAIL}/resolve`]: ({ body }) => {
        stored = (body as { resolution_note: string }).resolution_note;
        return json(200, {
          data: issue({ status: 'RESOLVED', resolved_at: '2026-11-26T10:00:00Z', handled_by_coordinator_id: coordinator.id, resolution_note: stored }),
        });
      },
    });
    renderAt(`/coordinator/issues/${ID}`);
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { level: 1, name: 'Report 0A1B2C3D' })).toBeInTheDocument();
    expect(screen.getByText(/The tutor waited 20 minutes\./)).toBeInTheDocument();
    expect(screen.getByText('About').nextElementSibling).toHaveTextContent('A session (ref. CCCCDDDD)');
    expect(screen.getByText(/doesn’t change any session, attendance record or account/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Resolve report' }));
    expect(screen.getByText('Write what you did or decided; it is stored with the report.')).toBeInTheDocument();
    expect(screen.getByLabelText('Resolution note')).toHaveFocus();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.type(screen.getByLabelText('Resolution note'), '  Sent the student a new meeting link.  ');
    await user.click(screen.getByRole('button', { name: 'Resolve report' }));

    expect(await screen.findByText('Report resolved. Your note is stored with it.')).toBeInTheDocument();
    expect(screen.getByText('Resolution note', { selector: 'h2' })).toBeInTheDocument();
    expect(screen.getByText('Sent the student a new meeting link.')).toBeInTheDocument();
    expect(screen.getByText('Status').nextElementSibling).toHaveTextContent('Resolved');
    expect(screen.queryByRole('button', { name: 'Resolve report' })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'POST')).toEqual([
      { method: 'POST', url: `${DETAIL}/resolve`, body: { resolution_note: 'Sent the student a new meeting link.' } },
    ]);
  });

  it('shows the stored resolution when another coordinator resolved it first (409)', async () => {
    let resolvedElsewhere = false;
    mockCoordinator({
      [`GET ${DETAIL}`]: () =>
        json(200, { data: resolvedElsewhere ? issue({ status: 'RESOLVED', resolved_at: '2026-11-26T09:00:00Z', resolution_note: 'Handled by phone.' }) : issue() }),
      [`POST ${DETAIL}/resolve`]: () => {
        resolvedElsewhere = true;
        return json(409, { error: 'issue report conflicts with current data' });
      },
    });
    renderAt(`/coordinator/issues/${ID}`);
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Resolution note'), 'My note.');
    await user.click(screen.getByRole('button', { name: 'Resolve report' }));

    expect(await screen.findByText(/This report was already resolved/)).toBeInTheDocument();
    expect(await screen.findByText('Handled by phone.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Resolution note')).not.toBeInTheDocument();
  });

  it('explains a report of another faculty', async () => {
    mockCoordinator({ [`GET ${DETAIL}`]: () => json(404, { error: 'issue resource not found' }) });
    renderAt(`/coordinator/issues/${ID}`);

    expect(await screen.findByText('This report doesn’t exist or belongs to another faculty.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '← All problem reports' })).toHaveAttribute('href', '/coordinator/issues');
  });
});
