import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { TutorSessionCard } from '../features/bookings/TutorSessionCard';
import type { Session } from '../features/bookings/types';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests with a mocked API (test double). The real-backend journey is in tests/e2e.

const tutor = { ...verifiedStudent, roles: ['student', 'tutor'] };
const HOUR = 3_600_000;
const QUEUE = '/api/v1/tutors/me/session-requests';
const page = <T,>(data: T[]) => json(200, { data, pagination: { limit: 50, offset: 0, has_more: false } });

function session(overrides: Record<string, unknown> = {}) {
  const start = Date.now() + 48 * HOUR;
  return {
    id: 'ses-1',
    student_id: 'stu-1',
    tutor_id: tutor.id,
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
    status: 'REQUESTED',
    requested_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 24 * HOUR).toISOString(),
    is_late_cancellation: false,
    viewer_role: 'TUTOR',
    ...overrides,
  };
}

function mockQueues(requested: unknown[], accepted: unknown[], extra: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, tutor),
    [`GET ${QUEUE}`]: ({ url }) => (url.includes('status=ACCEPTED') ? page(accepted) : page(requested)),
    ...extra,
  });
}

function renderPage() {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: ['/tutor'] })} />
    </AuthProvider>,
  );
}

const requestsSection = () => screen.getByRole('region', { name: /^Requests waiting for you/ });
const confirmedSection = () => screen.getByRole('region', { name: /^Confirmed sessions/ });

afterEach(() => vi.unstubAllGlobals());

describe('TeachingPage', () => {
  it('lists requests with the student description and answer deadline', async () => {
    mockQueues([session()], []);
    renderPage();

    expect(await screen.findByRole('heading', { name: 'Requests waiting for you (1)' })).toBeInTheDocument();
    expect(within(requestsSection()).getByText('Exercise sheet 4.')).toBeInTheDocument();
    expect(within(requestsSection()).getByText(/^Answer by /)).toBeInTheDocument();
    expect(within(requestsSection()).getByText('Waiting for your answer')).toBeInTheDocument();
  });

  it('accepts an online request only with a valid meeting link, then moves it to confirmed', async () => {
    const { calls } = mockQueues([session()], [], {
      [`POST ${QUEUE}/ses-1/accept`]: ({ body }) => json(200, { data: session({ status: 'ACCEPTED', ...(body as Record<string, unknown>) }) }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Accept Algorithms/ }));
    await user.type(screen.getByLabelText('Meeting link'), 'not a link');
    await user.click(screen.getByRole('button', { name: 'Accept the session' }));
    expect(screen.getByText(/Enter a meeting link that starts with https:\/\//)).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.clear(screen.getByLabelText('Meeting link'));
    await user.type(screen.getByLabelText('Meeting link'), 'https://teams.example/meet/1');
    await user.click(screen.getByRole('button', { name: 'Accept the session' }));

    expect(await screen.findByRole('heading', { name: 'Confirmed sessions (1)' })).toBeInTheDocument();
    expect(within(confirmedSection()).getByRole('link', { name: 'https://teams.example/meet/1' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Requests waiting for you (0)' })).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ meeting_link: 'https://teams.example/meet/1' });
  });

  it('asks for a room for an on-site request', async () => {
    const { calls } = mockQueues([session({ mode: 'ONSITE' })], [], {
      [`POST ${QUEUE}/ses-1/accept`]: ({ body }) => json(200, { data: session({ mode: 'ONSITE', status: 'ACCEPTED', ...(body as Record<string, unknown>) }) }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Accept Algorithms/ }));
    await user.type(screen.getByLabelText('Room or place'), 'Room B-203');
    await user.click(screen.getByRole('button', { name: 'Accept the session' }));

    expect(await within(confirmedSection()).findByText('Room: Room B-203')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ meeting_room: 'Room B-203' });
  });

  it('requires a reason to decline', async () => {
    const { calls } = mockQueues([session()], [], {
      [`POST ${QUEUE}/ses-1/decline`]: () => json(200, { data: session({ status: 'DECLINED', decline_reason: 'Ill.' }) }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Decline Algorithms/ }));
    await user.click(screen.getByRole('button', { name: 'Decline the session' }));
    expect(screen.getByText('Give a reason; the student sees it.')).toBeInTheDocument();

    await user.type(screen.getByLabelText('Reason for declining'), 'Ill.');
    await user.click(screen.getByRole('button', { name: 'Decline the session' }));

    expect(await screen.findByText('Declined. The student was notified with your reason.')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ reason: 'Ill.' });
  });

  it('explains an expired request', async () => {
    mockQueues([session()], [], {
      [`POST ${QUEUE}/ses-1/decline`]: () => json(409, { error: 'the session request has expired' }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Decline Algorithms/ }));
    await user.type(screen.getByLabelText('Reason for declining'), 'Busy.');
    await user.click(screen.getByRole('button', { name: 'Decline the session' }));

    expect(await within(requestsSection()).findByRole('alert')).toHaveTextContent('This request has expired');
  });

  it('offers completion only after the session has ended', async () => {
    const past = Date.now() - 3 * HOUR;
    const ended = session({ id: 'ses-ended', status: 'ACCEPTED', starts_at: new Date(past).toISOString(), ends_at: new Date(past + HOUR).toISOString(), meeting_link: 'https://meet.example/a' });
    const upcoming = session({ id: 'ses-upcoming', subject_name: 'Databases', status: 'ACCEPTED', meeting_link: 'https://meet.example/b' });
    const { calls } = mockQueues([], [ended, upcoming], {
      'POST /api/v1/tutors/me/sessions/ses-ended/complete': () => json(200, { data: { ...ended, status: 'COMPLETED' } }),
    });
    renderPage();
    const user = userEvent.setup();

    const upcomingCard = (await screen.findByRole('heading', { name: 'Databases' })).closest('li')!;
    expect(within(upcomingCard).queryByRole('button', { name: /completed/i })).not.toBeInTheDocument();
    expect(within(upcomingCard).getByText(/You can mark it as completed after it ends/)).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /^Mark Algorithms .* as completed/ }));
    await user.click(screen.getByRole('button', { name: 'Yes, it took place' }));

    expect(await screen.findByText('Marked as completed. The student can now rate the session.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Confirmed sessions (1)' })).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST' && c.url.endsWith('/ses-ended/complete'))).toBe(true);
  });

  it('offers no actions on sessions in a final state', () => {
    for (const status of ['DECLINED', 'EXPIRED', 'CANCELLED', 'COMPLETED', 'NO_SHOW']) {
      const { unmount } = render(
        <ul>
          <TutorSessionCard session={session({ status }) as unknown as Session} onChanged={() => undefined} />
        </ul>,
      );
      expect(screen.queryAllByRole('button')).toHaveLength(0);
      unmount();
    }
  });

  it('is only for tutors', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderPage();

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
  });
});
