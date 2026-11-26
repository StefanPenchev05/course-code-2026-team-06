import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { AuthProvider } from '../auth/AuthProvider';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Coordinator registration-verification queue (#51, FR2/UC01).

const FACULTY_ID = '55555555-5555-5555-5555-555555555555';
const ID = '99999999-9999-9999-9999-999999999999';
const DETAIL = `/api/v1/coordinator/verifications/${ID}`;
const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY_ID,
};
const PROGRAMS = [{ id: 'prog-cs', faculty_id: FACULTY_ID, name: 'Bachelor in Computer Science', level: 'BACHELOR' }];

function verification(overrides: Record<string, unknown> = {}) {
  return {
    id: ID,
    student_id: '12121212-1212-1212-1212-121212121212',
    faculty_id: FACULTY_ID,
    name: 'Nina Newcomer',
    email: 'nina.newcomer@student.uni.lu',
    student_number: '0231234567',
    program_id: 'prog-cs',
    status: 'PENDING',
    submitted_at: '2026-11-20T09:30:00Z',
    ...overrides,
  };
}

function mockCoordinator(routes: Record<string, (call: MockCall) => Response>) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, coordinator),
    [`GET /api/v1/public/faculties/${FACULTY_ID}/programs`]: () => json(200, { data: PROGRAMS }),
    ...routes,
  });
}

function renderAt(path: string) {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
    </AuthProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe('Registration queue', () => {
  it('lists pending registrations with the details to check, oldest first', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/verifications': () =>
        json(200, [verification(), verification({ id: 'v-2', name: 'Omar Older', student_number: '0239999999', email: 'omar@student.uni.lu' })]),
    });
    renderAt('/coordinator/verifications');

    expect(await screen.findByRole('heading', { level: 1, name: 'Student registrations' })).toBeInTheDocument();
    const items = within(await screen.findByRole('list', { name: 'Registrations waiting for review' })).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(within(items[0]).getByRole('link', { name: 'Nina Newcomer' })).toHaveAttribute('href', `/coordinator/verifications/${ID}`);
    expect(items[0]).toHaveTextContent('Student ID 0231234567 · nina.newcomer@student.uni.lu');
    expect(await within(items[0]).findByText(/Bachelor in Computer Science · Submitted/)).toBeInTheDocument();
    expect(calls.find((c) => c.url.startsWith('/api/v1/coordinator/verifications'))?.url).toBe('/api/v1/coordinator/verifications?limit=26&offset=0');
  });

  it('shows 25 at a time; the API sends a bare array, so one extra row tells whether there are more', async () => {
    const rows = (from: number, count: number) =>
      Array.from({ length: count }, (_, i) => verification({ id: `v-${from + i}`, name: `Student ${from + i}` }));
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/verifications': ({ url }) => json(200, url.includes('offset=0') ? rows(0, 26) : rows(25, 3)),
    });
    renderAt('/coordinator/verifications');
    const user = userEvent.setup();

    expect(await screen.findByRole('link', { name: 'Student 24' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Student 25' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Show more' }));
    expect(await screen.findByRole('link', { name: 'Student 27' })).toBeInTheDocument();
    expect(within(screen.getByRole('list', { name: 'Registrations waiting for review' })).getAllByRole('listitem')).toHaveLength(28);
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.url.startsWith('/api/v1/coordinator/verifications')).map((c) => c.url)).toEqual([
      '/api/v1/coordinator/verifications?limit=26&offset=0',
      '/api/v1/coordinator/verifications?limit=26&offset=25',
    ]);
  });

  it('says when nothing is waiting', async () => {
    mockCoordinator({ 'GET /api/v1/coordinator/verifications': () => json(200, []) });
    renderAt('/coordinator/verifications');

    expect(await screen.findByText('No registrations are waiting for review.')).toBeInTheDocument();
  });

  it('is only for coordinators', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/coordinator/verifications');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/coordinator/'))).toBe(false);
  });
});

describe('Reviewing one registration', () => {
  it('approves after a confirmation and shows the new status', async () => {
    let status = 'PENDING';
    const { calls } = mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, verification({ status, ...(status === 'APPROVED' ? { reviewed_at: '2026-11-26T10:00:00Z' } : {}) })),
      [`POST ${DETAIL}/approve`]: () => {
        status = 'APPROVED';
        return new Response(null, { status: 204 });
      },
    });
    renderAt(`/coordinator/verifications/${ID}`);
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { level: 1, name: 'Registration of Nina Newcomer' })).toBeInTheDocument();
    expect(screen.getByText('Student ID').nextElementSibling).toHaveTextContent('0231234567');
    expect(screen.getByText('University email').nextElementSibling).toHaveTextContent('nina.newcomer@student.uni.lu');
    expect(await screen.findByText('Bachelor in Computer Science')).toBeInTheDocument();
    expect(screen.getByText('Waiting for review')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Approve' }));
    await user.click(screen.getByRole('button', { name: 'Yes, approve' }));

    expect(await screen.findByText('Registration approved. The student can now use Tutti.')).toBeInTheDocument();
    expect(await screen.findByText('Approved')).toBeInTheDocument();
    expect(screen.getByText('Decided')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reject' })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'POST').map((c) => c.url)).toEqual([`${DETAIL}/approve`]);
  });

  it('rejects with a required reason the student will see', async () => {
    let rejected: string | null = null;
    const { calls } = mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, verification(rejected ? { status: 'REJECTED', rejection_reason: rejected, reviewed_at: '2026-11-26T10:00:00Z' } : {})),
      [`POST ${DETAIL}/reject`]: ({ body }) => {
        rejected = (body as { reason: string }).reason;
        return new Response(null, { status: 204 });
      },
    });
    renderAt(`/coordinator/verifications/${ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Reject' }));
    await user.click(screen.getByRole('button', { name: 'Reject registration' }));
    expect(screen.getByText('Give a reason; the student sees it and can correct their details.')).toBeInTheDocument();
    expect(screen.getByLabelText('Reason for rejecting')).toHaveFocus();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.type(screen.getByLabelText('Reason for rejecting'), '  The student ID does not match our records.  ');
    await user.click(screen.getByRole('button', { name: 'Reject registration' }));

    expect(await screen.findByText(/Registration rejected\./)).toBeInTheDocument();
    expect(await screen.findByText('Rejected')).toBeInTheDocument();
    expect(screen.getByText('Rejection reason').nextElementSibling).toHaveTextContent('The student ID does not match our records.');
    expect(calls.filter((c) => c.method === 'POST')).toEqual([
      { method: 'POST', url: `${DETAIL}/reject`, body: { reason: 'The student ID does not match our records.' } },
    ]);
  });

  it('shows the current state when another coordinator decided first (409)', async () => {
    let decided = false;
    mockCoordinator({
      [`GET ${DETAIL}`]: () => json(200, verification(decided ? { status: 'REJECTED', rejection_reason: 'Not enrolled.' } : {})),
      [`POST ${DETAIL}/approve`]: () => {
        decided = true;
        return json(409, { error: 'conflicting operation' });
      },
    });
    renderAt(`/coordinator/verifications/${ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Approve' }));
    await user.click(screen.getByRole('button', { name: 'Yes, approve' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('This verification was already decided');
    expect(alert.parentElement).toHaveFocus();
    expect(await screen.findByText('Not enrolled.')).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Decision' })).not.toBeInTheDocument();
  });

  it('keeps an old decision but hides the details of a student who moved to another faculty', async () => {
    mockCoordinator({
      [`GET ${DETAIL}`]: () =>
        json(200, { id: ID, student_id: 'x', faculty_id: FACULTY_ID, status: 'APPROVED', submitted_at: '2026-10-01T08:00:00Z', reviewed_at: '2026-10-02T08:00:00Z' }),
    });
    renderAt(`/coordinator/verifications/${ID}`);

    expect(await screen.findByText(/moved to another faculty/)).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Student registration' })).toBeInTheDocument();
    expect(screen.queryByText('Student ID')).not.toBeInTheDocument();
  });

  it('explains a registration of another faculty', async () => {
    mockCoordinator({ [`GET ${DETAIL}`]: () => json(404, { error: 'resource not found' }) });
    renderAt(`/coordinator/verifications/${ID}`);

    expect(await screen.findByText('This verification doesn’t exist or belongs to another faculty.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '← All registrations' })).toHaveAttribute('href', '/coordinator/verifications');
  });
});
