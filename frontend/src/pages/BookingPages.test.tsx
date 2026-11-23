import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests with a mocked API (test double). The real-backend journey is in tests/e2e.

const TUTOR_ID = '00000000-0000-0000-0000-0000000000aa';
const OFFERING = { id: 'off-1', subject_id: 'sub-1', subject_name: 'Algorithms', description: '', active: true };
const PROFILE = { id: TUTOR_ID, name: 'Tom Tutor', description: '', active: true, average_rating: null, rating_count: 0, offerings: [OFFERING] };
const DAY_MS = 24 * 60 * 60 * 1000;
// A free time three days from now, on a full hour, 2 hours long.
const slotStart = new Date(Math.ceil((Date.now() + 3 * DAY_MS) / 3_600_000) * 3_600_000);
const SLOT = {
  id: 'slot-1',
  tutor_id: TUTOR_ID,
  starts_at: slotStart.toISOString(),
  ends_at: new Date(slotStart.getTime() + 2 * 3_600_000).toISOString(),
  allowed_mode: 'BOTH',
  is_recurring: false,
};
const page = <T,>(data: T[], hasMore = false) => json(200, { data, pagination: { limit: 50, offset: 0, has_more: hasMore } });

function session(overrides: Record<string, unknown> = {}) {
  return {
    id: 'ses-1',
    student_id: verifiedStudent.id,
    tutor_id: TUTOR_ID,
    subject_id: 'sub-1',
    subject_name: 'Algorithms',
    offering_id: 'off-1',
    semester_id: 'sem-1',
    starts_at: SLOT.starts_at,
    ends_at: new Date(slotStart.getTime() + 3_600_000).toISOString(),
    duration_minutes: 60,
    mode: 'ONLINE',
    purpose: 'EXAM_PREPARATION',
    description: 'Dynamic programming exercises.',
    status: 'REQUESTED',
    requested_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 2 * DAY_MS).toISOString(),
    is_late_cancellation: false,
    viewer_role: 'STUDENT',
    ...overrides,
  };
}

function renderAt(path: string) {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: [path] })} />
    </AuthProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe('booking from a tutor profile', () => {
  it('books in three steps: choose a free time, fill in the form, send', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      [`GET /api/v1/tutors/${TUTOR_ID}`]: () => json(200, { data: PROFILE }),
      [`GET /api/v1/tutors/${TUTOR_ID}/availability`]: () => page([SLOT]),
      'POST /api/v1/sessions': ({ body }: MockCall) => json(201, { data: session(body as Record<string, unknown>) }),
    });
    renderAt(`/tutors/${TUTOR_ID}?subject_id=sub-1`);
    const user = userEvent.setup();

    // 1. choose a free time
    await user.click(await screen.findByRole('button', { name: /^Book / }));
    // 2. fill in the form
    const form = screen.getByRole('region', { name: /^Book Tom Tutor on/ });
    expect(within(form).getByRole('heading', { name: /^Book Tom Tutor/ })).toHaveFocus();
    await user.selectOptions(within(form).getByLabelText('Length'), '90');
    await user.selectOptions(within(form).getByLabelText('Where'), 'ONSITE');
    await user.type(within(form).getByLabelText('What do you need help with?'), 'Dynamic programming exercises.');
    // 3. send
    await user.click(within(form).getByRole('button', { name: 'Send request' }));

    const confirmation = (await screen.findByText('Request sent')).closest('[tabindex]') as HTMLElement;
    expect(within(confirmation).getByRole('link', { name: 'My sessions' })).toHaveAttribute('href', '/sessions');
    // The confirmation is above the form, so focus moves to it.
    expect(confirmation).toHaveFocus();
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
      offering_id: 'off-1',
      availability_slot_id: 'slot-1',
      starts_at: SLOT.starts_at,
      duration_minutes: 90,
      mode: 'ONSITE',
      purpose: 'EXAM_PREPARATION',
      description: 'Dynamic programming exercises.',
    });
  });

  it('only offers 30 minutes for an introduction', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      [`GET /api/v1/tutors/${TUTOR_ID}`]: () => json(200, { data: PROFILE }),
      [`GET /api/v1/tutors/${TUTOR_ID}/availability`]: () => page([SLOT]),
    });
    renderAt(`/tutors/${TUTOR_ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Book / }));
    await user.selectOptions(screen.getByLabelText('Purpose'), 'INTRODUCTION');

    const length = screen.getByLabelText('Length');
    expect(within(length).getAllByRole('option').map((o) => o.textContent)).toEqual(['30 minutes']);
  });

  it('requires a description and explains an overlap from the server', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      [`GET /api/v1/tutors/${TUTOR_ID}`]: () => json(200, { data: PROFILE }),
      [`GET /api/v1/tutors/${TUTOR_ID}/availability`]: () => page([SLOT]),
      'POST /api/v1/sessions': () => json(409, { error: 'a participant already has a reservation in this interval' }),
    });
    renderAt(`/tutors/${TUTOR_ID}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Book / }));
    await user.click(screen.getByRole('button', { name: 'Send request' }));
    expect(screen.getByText('Tell the tutor briefly what you need help with.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.type(screen.getByLabelText('What do you need help with?'), 'Graphs.');
    await user.click(screen.getByRole('button', { name: 'Send request' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('You or the tutor already have a session at this time.');
  });
});

describe('MySessionsPage', () => {
  it('shows status and the viewer’s role', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'GET /api/v1/sessions/me': ({ url }) =>
        url.includes('status=ACCEPTED')
          ? page([session({ id: 'ses-2', status: 'ACCEPTED', viewer_role: 'TUTOR', subject_name: 'Databases', meeting_link: 'https://meet.example/x' })])
          : page([session()]),
    });
    renderAt('/sessions');

    const requested = (await screen.findByRole('heading', { name: 'Algorithms' })).closest('li')!;
    expect(within(requested).getByText('Waiting for the tutor')).toBeInTheDocument();
    expect(within(requested).getByText('You are the student')).toBeInTheDocument();
    const accepted = screen.getByRole('heading', { name: 'Databases' }).closest('li')!;
    expect(within(accepted).getByText('Confirmed')).toBeInTheDocument();
    expect(within(accepted).getByText('You are the tutor')).toBeInTheDocument();
    // A tutor waits on nobody: their own requests read "Waiting for your answer" (checked in TeachingPage tests).
    expect(within(accepted).getByRole('link', { name: 'https://meet.example/x' })).toBeInTheDocument();
  });

  it('requires a reason to cancel, warns when it is late, and updates the list', async () => {
    const soon = new Date(Date.now() + 5 * 3_600_000);
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'GET /api/v1/sessions/me': ({ url }) =>
        url.includes('status=REQUESTED') ? page([session({ starts_at: soon.toISOString() })]) : page([]),
      'POST /api/v1/sessions/ses-1/cancel': ({ body }: MockCall) =>
        json(200, { data: session({ status: 'CANCELLED', is_late_cancellation: true, ...(body as object) }) }),
    });
    renderAt('/sessions');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Cancel Algorithms/ }));
    expect(screen.getByText('This is a late cancellation')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Cancel the session' }));
    expect(screen.getByText('Give a reason; the other person sees it.')).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST')).toBe(false);

    await user.type(screen.getByLabelText('Reason for cancelling'), 'I am ill.');
    await user.click(screen.getByRole('button', { name: 'Cancel the session' }));

    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Algorithms' })).not.toBeInTheDocument());
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ reason: 'I am ill.' });
  });

  it('points to tutor search when there is nothing booked', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent), 'GET /api/v1/sessions/me': () => page([]) });
    renderAt('/sessions');

    const empty = await screen.findByText(/You have no requested or confirmed sessions/);
    expect(within(empty).getByRole('link', { name: 'Find a tutor' })).toHaveAttribute('href', '/tutors');
  });
});
