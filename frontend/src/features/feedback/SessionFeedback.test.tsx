import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { AuthProvider } from '../auth/AuthProvider';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// They follow the student's feedback journey on My sessions (#34, FR9/UC05).

const HOUR = 3_600_000;
const FEEDBACK = '/api/v1/sessions/ses-1/feedback';
const page = <T,>(data: T[]) => json(200, { data, pagination: { limit: 50, offset: 0, has_more: false } });

function session(overrides: Record<string, unknown> = {}) {
  const start = Date.now() - 48 * HOUR;
  return {
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
    status: 'COMPLETED',
    requested_at: new Date(start - 72 * HOUR).toISOString(),
    expires_at: new Date(start - 48 * HOUR).toISOString(),
    is_late_cancellation: false,
    completed_at: new Date(start + HOUR).toISOString(),
    viewer_role: 'STUDENT',
    ...overrides,
  };
}

function entry(overrides: Record<string, unknown> = {}) {
  return {
    session_id: 'ses-1',
    session_status: 'COMPLETED',
    subject_id: 'sub-1',
    subject_name: 'Algorithms',
    starts_at: session().starts_at,
    rating: 4,
    comment: 'Clear explanations.',
    created_at: new Date().toISOString(),
    ...overrides,
  };
}

function mockSessions(sessions: unknown[], extra: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, verifiedStudent),
    'GET /api/v1/sessions/me': () => page(sessions),
    ...extra,
  });
}

function renderAllSessions() {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: ['/sessions?show=all'] })} />
    </AuthProvider>,
  );
}

const card = async (subject = 'Algorithms') => (await screen.findByRole('heading', { name: subject })).closest('li')!;

afterEach(() => vi.unstubAllGlobals());

describe('Rating a completed session', () => {
  it('lets the student rate their completed session once, with an optional comment', async () => {
    const { calls } = mockSessions([session()], {
      [`GET ${FEEDBACK}`]: () => json(404, { error: 'feedback resource not found' }),
      [`POST ${FEEDBACK}`]: ({ body }) => json(201, { data: entry(body as Record<string, unknown>) }),
    });
    renderAllSessions();
    const user = userEvent.setup();

    const algorithms = await card();
    expect(await within(algorithms).findByText('You haven’t rated this session yet.')).toBeInTheDocument();
    await user.click(within(algorithms).getByRole('button', { name: 'Rate this session: Algorithms' }));

    // Sending without a rating is stopped in the browser and focus moves to the rating.
    await user.click(within(algorithms).getByRole('button', { name: 'Send feedback' }));
    expect(within(algorithms).getByText('Choose a rating from 1 to 5.')).toBeInTheDocument();
    expect(within(algorithms).getByRole('radio', { name: '1 – Poor' })).toHaveFocus();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.click(within(algorithms).getByRole('radio', { name: '4 – Very good' }));
    await user.type(within(algorithms).getByLabelText('Comment (optional)'), '  Clear explanations.  ');
    await user.click(within(algorithms).getByRole('button', { name: 'Send feedback' }));

    expect(await within(algorithms).findByText('Thank you! Your feedback was sent to the tutor.')).toBeInTheDocument();
    expect(within(algorithms).getByText(/Your rating/)).toHaveTextContent('Your rating: ★★★★☆ 4 out of 5');
    expect(within(algorithms).getByText('Clear explanations.')).toBeInTheDocument();
    expect(within(algorithms).queryByRole('button', { name: /Rate/ })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.method === 'POST')).toEqual([
      { method: 'POST', url: FEEDBACK, body: { rating: 4, comment: 'Clear explanations.' } },
    ]);
  });

  it('shows the rating already given instead of the form', async () => {
    mockSessions([session()], { [`GET ${FEEDBACK}`]: () => json(200, { data: entry({ rating: 5, comment: undefined }) }) });
    renderAllSessions();

    const algorithms = await card();
    expect(await within(algorithms).findByText(/Your rating/)).toHaveTextContent('Your rating: ★★★★★ 5 out of 5');
    expect(within(algorithms).queryByRole('button')).not.toBeInTheDocument();
  });

  it('shows the tutor the student’s rating, but no rating form', async () => {
    mockSessions([session({ viewer_role: 'TUTOR', student_id: 'stu-2', tutor_id: verifiedStudent.id })], {
      [`GET ${FEEDBACK}`]: () => json(404, { error: 'feedback resource not found' }),
    });
    renderAllSessions();

    const algorithms = await card();
    expect(await within(algorithms).findByText('The student hasn’t rated this session yet.')).toBeInTheDocument();
    expect(within(algorithms).queryByRole('button')).not.toBeInTheDocument();
  });

  it('offers rating only for completed sessions, so no-shows cannot be rated', async () => {
    const sessions = [
      session({ id: 'ses-a', subject_name: 'Analysis', status: 'NO_SHOW' }),
      session({ id: 'ses-b', subject_name: 'Biology', status: 'CANCELLED', cancellation_reason: 'Ill.' }),
      session({ id: 'ses-c', subject_name: 'Chemistry', status: 'ACCEPTED', starts_at: new Date(Date.now() + 48 * HOUR).toISOString() }),
    ];
    const { calls } = mockSessions(sessions);
    renderAllSessions();

    for (const subject of ['Analysis', 'Biology', 'Chemistry']) {
      expect(within(await card(subject)).queryByRole('button', { name: /Rate/ })).not.toBeInTheDocument();
    }
    expect(calls.some((c) => c.url.includes('/feedback'))).toBe(false);
  });

  it('shows the existing rating when the session was already rated elsewhere (409)', async () => {
    let rated = false;
    mockSessions([session()], {
      [`GET ${FEEDBACK}`]: () => (rated ? json(200, { data: entry({ rating: 2, comment: 'Too fast.' }) }) : json(404, { error: 'feedback resource not found' })),
      [`POST ${FEEDBACK}`]: () => {
        rated = true;
        return json(409, { error: 'session already has feedback' });
      },
    });
    renderAllSessions();
    const user = userEvent.setup();

    const algorithms = await card();
    await user.click(await within(algorithms).findByRole('button', { name: 'Rate this session: Algorithms' }));
    await user.click(within(algorithms).getByRole('radio', { name: '5 – Excellent' }));
    await user.click(within(algorithms).getByRole('button', { name: 'Send feedback' }));

    expect(await within(algorithms).findByText('You had already rated this session.')).toBeInTheDocument();
    expect(await within(algorithms).findByText(/Your rating/)).toHaveTextContent('2 out of 5');
    expect(within(algorithms).getByText('Too fast.')).toBeInTheDocument();
  });

  it('explains when the session is no longer completed (corrected to a no-show)', async () => {
    mockSessions([session()], {
      [`GET ${FEEDBACK}`]: () => json(404, { error: 'feedback resource not found' }),
      [`POST ${FEEDBACK}`]: () => json(422, { error: 'only currently completed sessions can receive feedback' }),
    });
    renderAllSessions();
    const user = userEvent.setup();

    const algorithms = await card();
    await user.click(await within(algorithms).findByRole('button', { name: 'Rate this session: Algorithms' }));
    await user.click(within(algorithms).getByRole('radio', { name: '3 – Good' }));
    await user.click(within(algorithms).getByRole('button', { name: 'Send feedback' }));

    const alert = await within(algorithms).findByRole('alert');
    expect(alert).toHaveTextContent('Only completed sessions can be rated. This session may have been marked as a no-show.');
    expect(alert.parentElement).toHaveFocus();
    expect(within(algorithms).getByRole('button', { name: 'Send feedback' })).toBeEnabled();
  });

  it('stops comments longer than the API allows', async () => {
    const { calls } = mockSessions([session()], { [`GET ${FEEDBACK}`]: () => json(404, { error: 'feedback resource not found' }) });
    renderAllSessions();
    const user = userEvent.setup();

    const algorithms = await card();
    await user.click(await within(algorithms).findByRole('button', { name: 'Rate this session: Algorithms' }));
    await user.click(within(algorithms).getByRole('radio', { name: '4 – Very good' }));
    await user.click(within(algorithms).getByLabelText('Comment (optional)'));
    await user.paste('a'.repeat(2001));
    expect(within(algorithms).getByText(/2001\/2000 characters/)).toBeInTheDocument();
    await user.click(within(algorithms).getByRole('button', { name: 'Send feedback' }));

    expect(within(algorithms).getByText('Shorten the comment to 2000 characters or fewer (it has 2001).')).toBeInTheDocument();
    expect(within(algorithms).getByLabelText('Comment (optional)')).toHaveFocus();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('can try loading the feedback again after an error', async () => {
    let failing = true;
    mockSessions([session()], {
      [`GET ${FEEDBACK}`]: () => (failing ? json(500, { error: 'internal server error' }) : json(200, { data: entry() })),
    });
    renderAllSessions();
    const user = userEvent.setup();

    const algorithms = await card();
    expect(await within(algorithms).findByRole('alert')).toHaveTextContent('Feedback could not be loaded');
    failing = false;
    await user.click(within(algorithms).getByRole('button', { name: 'Try again' }));
    expect(await within(algorithms).findByText(/Your rating/)).toHaveTextContent('4 out of 5');
  });
});
