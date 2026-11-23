import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { json, mockApi, verifiedStudent, type MockCall } from '../test/mockApi';

// Component tests with a mocked API (test double). The real-backend journey is in tests/e2e.

const BASE = '/api/v1/tutors/me/availability';
const tutor = { ...verifiedStudent, roles: ['student', 'tutor'] };
const page = <T,>(data: T[]) => json(200, { data, pagination: { limit: 100, offset: 0, has_more: false } });

const ONE_OFF = {
  id: 'w1',
  tutor_id: tutor.id,
  starts_at: '2030-01-07T09:00:00Z',
  ends_at: '2030-01-07T10:30:00Z',
  allowed_mode: 'ONLINE',
  is_recurring: false,
};
const WEEKLY = { ...ONE_OFF, id: 'w2', starts_at: '2030-01-08T13:00:00Z', ends_at: '2030-01-08T16:00:00Z', is_recurring: true };
const RULE = {
  id: 'r1',
  semester_id: 's1',
  iso_weekday: 2,
  // The real API returns seconds.
  local_start_time: '14:00:00',
  local_end_time: '17:00:00',
  time_zone: 'Europe/Luxembourg',
  allowed_mode: 'BOTH',
};

function mockAvailability(extra: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, tutor),
    [`GET ${BASE}`]: () => page([ONE_OFF, WEEKLY]),
    [`GET ${BASE}/rules`]: () => page([RULE]),
    ...extra,
  });
}

function renderPage() {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: ['/tutor/availability'] })} />
    </AuthProvider>,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe('TutorAvailabilityPage', () => {
  it('shows upcoming free times and weekly times with their local zone', async () => {
    mockAvailability();
    renderPage();

    expect(await screen.findByRole('heading', { name: 'My availability' })).toBeInTheDocument();
    expect(await screen.findByText('Every Tuesday, 14:00–17:00 (Europe/Luxembourg) · Online or on site')).toBeInTheDocument();
    expect(await screen.findByText('Online · One-off')).toBeInTheDocument();
    expect(screen.getByText('Online · Weekly')).toBeInTheDocument();
  });

  it('removes a one-off time after confirming, and reloads the list', async () => {
    let removed = false;
    const { calls } = mockAvailability({
      [`GET ${BASE}`]: () => page(removed ? [WEEKLY] : [ONE_OFF, WEEKLY]),
      [`DELETE ${BASE}/slots/w1`]: () => {
        removed = true;
        return json(204);
      },
    });
    renderPage();
    const user = userEvent.setup();

    const oneOff = (await screen.findByText('Online · One-off')).closest('li')!;
    await user.click(within(oneOff).getByRole('button', { name: /^Remove/ }));
    await user.click(within(oneOff).getByRole('button', { name: 'Yes, remove' }));

    expect(await screen.findByText('Removed.')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText('Online · One-off')).not.toBeInTheDocument());
    expect(calls.some((c) => c.method === 'DELETE' && c.url === `${BASE}/slots/w1`)).toBe(true);
  });

  it('explains that a booked time cannot be removed', async () => {
    mockAvailability({
      [`DELETE ${BASE}/slots/w1`]: () => json(409, { error: 'an active session depends on this availability' }),
    });
    renderPage();
    const user = userEvent.setup();

    const oneOff = (await screen.findByText('Online · One-off')).closest('li')!;
    await user.click(within(oneOff).getByRole('button', { name: /^Remove/ }));
    await user.click(within(oneOff).getByRole('button', { name: 'Yes, remove' }));

    expect(await within(oneOff).findByRole('alert')).toHaveTextContent('A student has a booking that depends on this time');
    expect(screen.getByText('Online · One-off')).toBeInTheDocument();
  });

  it('adds a one-off time in the tutor’s time zone', async () => {
    const { calls } = mockAvailability({
      [`POST ${BASE}/slots`]: ({ body }) => json(201, { data: { ...ONE_OFF, ...(body as object) } }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Date'), '2030-02-04');
    const form = screen.getByRole('heading', { name: 'Add a one-off free time' }).closest('section')!;
    await user.type(within(form).getByLabelText('From'), '10:00');
    await user.type(within(form).getByLabelText('Until'), '11:30');
    await user.selectOptions(within(form).getByLabelText('Where'), 'ONSITE');
    await user.click(screen.getByRole('button', { name: 'Add free time' }));

    expect(await screen.findByText('Added. Students can now book this time.')).toBeInTheDocument();
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
      starts_at: new Date('2030-02-04T10:00').toISOString(),
      ends_at: new Date('2030-02-04T11:30').toISOString(),
      allowed_mode: 'ONSITE',
    });
  });

  it('explains an overlapping one-off time and keeps the input', async () => {
    mockAvailability({
      [`POST ${BASE}/slots`]: () => json(409, { error: 'availability overlaps an existing window' }),
    });
    renderPage();
    const user = userEvent.setup();

    const form = (await screen.findByRole('heading', { name: 'Add a one-off free time' })).closest('section')!;
    await user.type(within(form).getByLabelText('Date'), '2030-01-07');
    await user.type(within(form).getByLabelText('From'), '09:30');
    await user.type(within(form).getByLabelText('Until'), '10:00');
    await user.click(screen.getByRole('button', { name: 'Add free time' }));

    expect(await within(form).findByRole('alert')).toHaveTextContent('This overlaps another free time of yours.');
    expect(within(form).getByLabelText('Date')).toHaveValue('2030-01-07');
  });

  it('edits a weekly time, sending only the changed fields', async () => {
    const { calls } = mockAvailability({
      [`PATCH ${BASE}/rules/r1`]: ({ body }) => json(200, { data: { ...RULE, ...(body as object) } }),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Edit Every Tuesday/ }));
    const editor = screen.getByRole('form', { name: 'Edit weekly time' });
    await user.selectOptions(within(editor).getByLabelText('Day'), '3');
    await user.clear(within(editor).getByLabelText('Until'));
    await user.type(within(editor).getByLabelText('Until'), '18:00');
    await user.click(within(editor).getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ iso_weekday: 3, local_end_time: '18:00' }));
  });

  it('restores a skipped date', async () => {
    let skipped = true;
    const { calls } = mockAvailability({
      [`GET ${BASE}/rules/r1/exceptions`]: () => page(skipped ? [{ local_date: '2030-01-15' }] : []),
      [`DELETE ${BASE}/rules/r1/exceptions/2030-01-15`]: () => {
        skipped = false;
        return json(204);
      },
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: /^Skipped dates of Every Tuesday/ }));
    await user.click(await screen.findByRole('button', { name: /^Restore .*2030/ }));

    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url.endsWith('/exceptions/2030-01-15'))).toBe(true));
  });

  it('is only for tutors', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderPage();

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
  });
});
