import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../../app/routes';
import { AuthProvider } from '../auth/AuthProvider';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// They cover the tutor's activity page (#34, FR14/UC10, and the activity part of #55).

const tutor = { ...verifiedStudent, roles: ['student', 'tutor'] };
const HOUR = 3_600_000;
const ACTIVITY = '/api/v1/tutors/me/activity';
const RECEIVED = '/api/v1/tutors/me/feedback';

function activity(overrides: Record<string, unknown> = {}) {
  const start = Date.now() + 24 * HOUR;
  return {
    average_rating: 4.33,
    rating_count: 3,
    completed_sessions: 4,
    hours_taught: 3.75,
    upcoming_sessions: [
      {
        id: 'ses-next',
        subject_id: 'sub-2',
        subject_name: 'Databases',
        starts_at: new Date(start).toISOString(),
        ends_at: new Date(start + HOUR).toISOString(),
        mode: 'ONLINE',
        meeting_link: 'https://meet.example/db',
      },
    ],
    pagination: { limit: 50, offset: 0, has_more: false },
    ...overrides,
  };
}

function entry(id: string, rating: number, comment?: string) {
  return {
    session_id: id,
    session_status: 'COMPLETED',
    subject_id: 'sub-1',
    subject_name: 'Algorithms',
    starts_at: new Date(Date.now() - 72 * HOUR).toISOString(),
    rating,
    comment,
    created_at: new Date().toISOString(),
  };
}

const feedbackPage = (data: unknown[], hasMore = false) => json(200, { data, pagination: { limit: 25, offset: 0, has_more: hasMore } });

function mockActivity(routes: Record<string, (call: MockCall) => Response>, user: unknown = tutor) {
  return mockApi({ 'GET /api/v1/auth/me': () => json(200, user), ...routes });
}

function renderPage() {
  render(
    <AuthProvider>
      <RouterProvider router={createMemoryRouter(routes, { initialEntries: ['/tutor/activity'] })} />
    </AuthProvider>,
  );
}

/** The value next to a label in the totals. */
const total = (label: string) => screen.getByText(label).nextElementSibling;

afterEach(() => vi.unstubAllGlobals());

describe('My activity', () => {
  it('shows completed sessions, hours, rating, upcoming sessions and feedback', async () => {
    const { calls } = mockActivity({
      [`GET ${ACTIVITY}`]: () => json(200, { data: activity() }),
      [`GET ${RECEIVED}`]: () => feedbackPage([entry('ses-1', 5, 'Very patient.\nGreat examples.'), entry('ses-2', 4)]),
    });
    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'My activity' })).toBeInTheDocument();
    expect(await screen.findByText('Completed sessions')).toBeInTheDocument();
    expect(total('Completed sessions')).toHaveTextContent('4');
    expect(total('Hours taught')).toHaveTextContent(new Intl.NumberFormat().format(3.75)); // 3.75, or 3,75 in some locales
    expect(total('Average rating')).toHaveTextContent('4.3 / 5');
    expect(total('Ratings received')).toHaveTextContent('3');
    expect(screen.getByText(/no-shows are left out/)).toBeInTheDocument();

    const upcoming = screen.getByRole('region', { name: 'Upcoming sessions' });
    expect(within(upcoming).getByRole('heading', { name: 'Databases' })).toBeInTheDocument();
    expect(within(upcoming).getByRole('link', { name: 'https://meet.example/db' })).toHaveAttribute('href', 'https://meet.example/db');

    const feedback = screen.getByRole('region', { name: 'Feedback from students' });
    const items = within(feedback).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('5 out of 5');
    expect(items[0]).toHaveTextContent('Very patient. Great examples.');
    expect(items[1]).toHaveTextContent('4 out of 5');
    expect(items[1]).toHaveTextContent('No comment.');
    expect(within(feedback).queryByRole('button')).not.toBeInTheDocument();

    // Only the keys the API accepts.
    const activityCall = calls.find((c) => c.url.startsWith(ACTIVITY))!;
    expect(new URL(activityCall.url, 'http://x').searchParams.toString()).toBe('limit=50&offset=0');
  });

  it('explains empty and unrated states', async () => {
    mockActivity({
      [`GET ${ACTIVITY}`]: () =>
        json(200, { data: activity({ average_rating: null, rating_count: 0, completed_sessions: 0, hours_taught: 0, upcoming_sessions: [] }) }),
      [`GET ${RECEIVED}`]: () => feedbackPage([]),
    });
    renderPage();

    expect(await screen.findByText('No ratings yet')).toBeInTheDocument();
    expect(total('Completed sessions')).toHaveTextContent('0');
    expect(total('Hours taught')).toHaveTextContent('0');
    expect(screen.getByText(/No confirmed sessions are coming up/)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'free times' })).toHaveAttribute('href', '/tutor/availability');
    expect(screen.getByText(/No feedback yet/)).toBeInTheDocument();
  });

  it('loads more feedback without repeating entries', async () => {
    const { calls } = mockActivity({
      [`GET ${ACTIVITY}`]: () => json(200, { data: activity() }),
      [`GET ${RECEIVED}`]: ({ url }) =>
        url.includes('offset=0')
          ? feedbackPage([entry('ses-1', 5, 'First page.')], true)
          : feedbackPage([entry('ses-1', 5, 'First page.'), entry('ses-9', 3, 'Second page.')]),
    });
    renderPage();
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Show more feedback' }));

    const feedback = screen.getByRole('region', { name: 'Feedback from students' });
    expect(await within(feedback).findByText('Second page.')).toBeInTheDocument();
    expect(within(feedback).getAllByRole('listitem')).toHaveLength(2);
    expect(within(feedback).queryByRole('button', { name: 'Show more feedback' })).not.toBeInTheDocument();
    expect(calls.filter((c) => c.url.startsWith(RECEIVED)).map((c) => c.url)).toEqual([
      `${RECEIVED}?limit=25&offset=0`,
      `${RECEIVED}?limit=25&offset=1`,
    ]);
  });

  it('says when not all upcoming sessions are shown', async () => {
    mockActivity({
      [`GET ${ACTIVITY}`]: () => json(200, { data: activity({ pagination: { limit: 50, offset: 0, has_more: true } }) }),
      [`GET ${RECEIVED}`]: () => feedbackPage([]),
    });
    renderPage();

    const upcoming = await screen.findByRole('region', { name: 'Upcoming sessions' });
    expect(within(upcoming).getByText(/Only the next 1 are shown/)).toBeInTheDocument();
    expect(within(upcoming).getByRole('link', { name: 'Teaching' })).toHaveAttribute('href', '/tutor');
  });

  it('shows an error with a way to try again', async () => {
    let failing = true;
    mockActivity({
      [`GET ${ACTIVITY}`]: () => (failing ? json(500, { error: 'internal server error' }) : json(200, { data: activity() })),
      [`GET ${RECEIVED}`]: () => feedbackPage([]),
    });
    renderPage();
    const user = userEvent.setup();

    expect(await screen.findByRole('alert')).toHaveTextContent('Your activity could not be loaded');
    failing = false;
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Completed sessions')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('is only for tutors', async () => {
    const { calls } = mockActivity({}, verifiedStudent);
    renderPage();

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith(ACTIVITY))).toBe(false);
  });
});
