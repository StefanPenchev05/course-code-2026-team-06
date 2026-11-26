import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { AuthProvider } from '../auth/AuthProvider';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Student problem reports (#52, FR10/UC06).

const HOUR = 3_600_000;
const REPORT_ID = '0a1b2c3d-4e5f-6789-abcd-ef0123456789';
const start = Date.now() - 48 * HOUR;
const session = {
  id: 'ses-1',
  student_id: verifiedStudent.id,
  tutor_id: 'tutor-1',
  subject_id: 'sub-1',
  subject_name: 'Algorithms',
  offering_id: 'off-1',
  semester_id: 'sem-1',
  starts_at: new Date(start).toISOString(),
  ends_at: new Date(start + HOUR).toISOString(),
  duration_minutes: 60,
  mode: 'ONLINE',
  purpose: 'HOMEWORK',
  description: 'Exercise sheet 4.',
  status: 'ACCEPTED',
  requested_at: new Date(start - 72 * HOUR).toISOString(),
  expires_at: new Date(start - 48 * HOUR).toISOString(),
  is_late_cancellation: false,
  viewer_role: 'STUDENT',
};

function created(body: unknown) {
  const input = body as { description: string; session_id?: string };
  return json(201, {
    data: { id: REPORT_ID, reporter_user_id: verifiedStudent.id, faculty_id: 'fac-1', ...input, status: 'OPEN', created_at: new Date().toISOString() },
  });
}

function mockStudent(routes: Record<string, (call: MockCall) => Response>) {
  return mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent), ...routes });
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

describe('Report a problem', () => {
  it('sends a general report to the student’s faculty, with a required description', async () => {
    const { calls } = mockStudent({ 'POST /api/v1/issues': ({ body }) => created(body) });
    renderAt('/report');
    const user = userEvent.setup();

    expect(await screen.findByRole('heading', { level: 1, name: 'Report a problem' })).toBeInTheDocument();
    expect(screen.getByText(/This is not a no-show report/)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Send report' }));
    expect(screen.getByText('Describe the problem.')).toBeInTheDocument();
    expect(screen.getByLabelText('What happened?')).toHaveFocus();
    expect(posts(calls)).toHaveLength(0);

    await user.type(screen.getByLabelText('What happened?'), '  The tutor did not answer my messages.  ');
    await user.click(screen.getByRole('button', { name: 'Send report' }));

    expect(await screen.findByText('Report sent')).toBeInTheDocument();
    // Submission feedback, not a claim that the problem was dealt with.
    expect(screen.getByText(/in the inbox of the coordinators of your faculty/)).toHaveTextContent(
      'doesn’t resolve anything yet: a coordinator still has to review it',
    );
    expect(screen.getByText('0A1B2C3D')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Send report' })).not.toBeInTheDocument();
    expect(posts(calls)).toEqual([{ method: 'POST', url: '/api/v1/issues', body: { description: 'The tutor did not answer my messages.' } }]);
  });

  it('links the report to a session when opened from that session', async () => {
    const { calls } = mockStudent({
      'GET /api/v1/sessions/ses-1': () => json(200, { data: session }),
      'POST /api/v1/issues': ({ body }) => created(body),
    });
    renderAt('/report?session=ses-1');
    const user = userEvent.setup();

    expect(await screen.findByText(/About your/)).toHaveTextContent('About your Algorithms session on');
    expect(screen.getByRole('link', { name: 'Report something else instead' })).toHaveAttribute('href', '/report');
    await user.type(screen.getByLabelText('What happened?'), 'The meeting link did not work.');
    await user.click(screen.getByRole('button', { name: 'Send report' }));

    expect(await screen.findByText(/in the inbox of the coordinators of the tutor’s faculty/)).toBeInTheDocument();
    expect(posts(calls)[0].body).toEqual({ description: 'The meeting link did not work.', session_id: 'ses-1' });
  });

  it('offers a general report when the session is not the student’s', async () => {
    const { calls } = mockStudent({ 'GET /api/v1/sessions/ses-x': () => json(404, { error: 'session not found' }) });
    renderAt('/report?session=ses-x');

    expect(await screen.findByText('This session can’t be linked to a report')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Write a general report instead' })).toHaveAttribute('href', '/report');
    expect(screen.queryByRole('button', { name: 'Send report' })).not.toBeInTheDocument();
    expect(posts(calls)).toHaveLength(0);
  });

  it('explains when the account may not send reports', async () => {
    mockStudent({ 'POST /api/v1/issues': () => json(403, { error: 'forbidden' }) });
    renderAt('/report');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('What happened?'), 'Something went wrong.');
    await user.click(screen.getByRole('button', { name: 'Send report' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('can’t send reports right now');
    expect(alert.parentElement).toHaveFocus();
    expect(screen.getByLabelText('What happened?')).toHaveValue('Something went wrong.');
  });

  it('shows the server’s validation message and keeps the text', async () => {
    mockStudent({ 'POST /api/v1/issues': () => json(422, { error: 'invalid input: description must contain 1 to 2000 bytes' }) });
    renderAt('/report');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('What happened?'), 'Too long for the server.');
    await user.click(screen.getByRole('button', { name: 'Send report' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Description must contain 1 to 2000 bytes');
    expect(screen.getByLabelText('What happened?')).toHaveValue('Too long for the server.');
    expect(screen.queryByText('Report sent')).not.toBeInTheDocument();
  });

  it('is not available to students whose registration is not approved', async () => {
    const pending = { ...verifiedStudent, verified_student: false, verification_status: 'PENDING' };
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, pending) });
    renderAt('/report');
    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(screen.getByText('This becomes available once a coordinator approves your registration.')).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/issues') || c.url.startsWith('/api/v1/sessions'))).toBe(false);
  });

  it('is not available to coordinator-only accounts', async () => {
    const coordinatorOnly = { id: 'coord-1', name: 'Carla', email: 'carla@uni.lu', roles: ['coordinator'], verified_student: false, coordinator_faculty_id: 'fac-1' };
    mockApi({ 'GET /api/v1/auth/me': () => json(200, coordinatorOnly) });
    renderAt('/report');
    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(screen.queryByLabelText('What happened?')).not.toBeInTheDocument();
  });

  it('is reachable from each session in My sessions', async () => {
    mockStudent({ 'GET /api/v1/sessions/me': () => json(200, { data: [session], pagination: { limit: 50, offset: 0, has_more: false } }) });
    renderAt('/sessions?show=all');

    const card = (await screen.findByRole('heading', { name: 'Algorithms' })).closest('li')!;
    expect(within(card).getByRole('link', { name: /^Report a problem with Algorithms on/ })).toHaveAttribute('href', '/report?session=ses-1');
  });
});
