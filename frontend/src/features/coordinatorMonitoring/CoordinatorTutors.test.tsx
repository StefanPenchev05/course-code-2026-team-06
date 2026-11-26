import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { json, mockApi, verifiedStudent, type MockCall } from '../../test/mockApi';
import { coordinator, FACULTY_ID, page, renderApp, TUTOR_ID, tutorSummary } from './testUtils';

// Component tests with a mocked API (test double, not integration evidence; see src/test/mockApi.ts).
// Coordinator home and tutor list (#35, FR16/UC12).

const SUBJECT = { id: 'sub-algo', code: 'BINFO-ALGO', name: 'Algorithms' };
const PROGRAMS = [
  { id: 'prog-cs', faculty_id: FACULTY_ID, name: 'Bachelor in Computer Science', level: 'BACHELOR' },
  { id: 'prog-math', faculty_id: FACULTY_ID, name: 'Bachelor in Mathematics', level: 'BACHELOR' },
];

function mockCoordinator(routes: Record<string, (call: MockCall) => Response> = {}) {
  return mockApi({
    'GET /api/v1/auth/me': () => json(200, coordinator),
    [`GET /api/v1/public/faculties/${FACULTY_ID}/programs`]: () => json(200, { data: PROGRAMS }),
    'GET /api/v1/subjects': () => json(200, page([SUBJECT], false, 10)),
    [`GET /api/v1/subjects/${SUBJECT.id}`]: () => json(200, { data: SUBJECT }),
    ...routes,
  });
}

const tutorCalls = (calls: MockCall[]) => calls.filter((c) => c.url.startsWith('/api/v1/coordinator/tutors?')).map((c) => new URL(c.url, 'http://x').searchParams);

afterEach(() => vi.unstubAllGlobals());

describe('Coordinator home', () => {
  it('shows the faculty totals and links to the tools', async () => {
    mockCoordinator({
      'GET /api/v1/coordinator/statistics': () =>
        json(200, {
          data: {
            faculty_id: FACULTY_ID,
            approved_tutors: 5,
            active_tutors: 4,
            completed_sessions: 12,
            hours_taught: 15,
            average_rating: 4.62,
            rating_count: 9,
          },
        }),
    });
    renderApp('/coordinator');

    expect(await screen.findByRole('heading', { level: 1, name: 'Coordinator' })).toBeInTheDocument();
    const value = async (label: string) => (await screen.findByText(label)).nextElementSibling;
    expect(await value('Approved tutors')).toHaveTextContent('5');
    expect(await value('Active tutors')).toHaveTextContent('4');
    expect(await value('Completed sessions')).toHaveTextContent('12');
    expect(await value('Hours taught')).toHaveTextContent('15');
    expect(await value('Average rating')).toHaveTextContent('4.6 / 5');
    expect(await value('Ratings')).toHaveTextContent('9');
    expect(screen.getByRole('link', { name: 'Tutors' })).toHaveAttribute('href', '/coordinator/tutors');
    expect(screen.getByRole('link', { name: 'Tutor applications' })).toHaveAttribute('href', '/coordinator/tutor-applications');
  });

  it('can try again when the totals fail to load', async () => {
    let failing = true;
    mockCoordinator({
      'GET /api/v1/coordinator/statistics': () =>
        failing
          ? json(500, { error: 'internal server error' })
          : json(200, { data: { faculty_id: FACULTY_ID, approved_tutors: 0, active_tutors: 0, completed_sessions: 0, hours_taught: 0, average_rating: null, rating_count: 0 } }),
    });
    renderApp('/coordinator');
    const user = userEvent.setup();

    expect(await screen.findByRole('alert')).toHaveTextContent('The faculty totals could not be loaded');
    failing = false;
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('No ratings yet')).toBeInTheDocument();
  });
});

describe('Tutors of the faculty', () => {
  it('lists tutors with their activity and filters by program and subject in the URL', async () => {
    const { calls } = mockCoordinator({
      'GET /api/v1/coordinator/tutors': () =>
        json(200, page([tutorSummary(), tutorSummary({ id: 'tutor-2', name: 'Uma Unbookable', active: false, completed_sessions: 1, hours_taught: 0.75, average_rating: null, rating_count: 0 })])),
    });
    const router = renderApp('/coordinator/tutors');
    const user = userEvent.setup();

    const tom = (await screen.findByRole('link', { name: 'Tom Tutor' })).closest('li')!;
    expect(screen.getByRole('link', { name: 'Tom Tutor' })).toHaveAttribute('href', `/coordinator/tutors/${TUTOR_ID}`);
    expect(within(tom).getByText('Active')).toBeInTheDocument();
    expect(within(tom).getByText('Bachelor in Computer Science')).toBeInTheDocument();
    expect(within(tom).getByText(/4 completed sessions/)).toHaveTextContent('4 completed sessions · 6 h taught · 4.7 / 5 (3 ratings)');
    const uma = screen.getByRole('link', { name: 'Uma Unbookable' }).closest('li')!;
    expect(within(uma).getByText('Not bookable')).toBeInTheDocument();
    expect(within(uma).getByText(/1 completed session ·/)).toHaveTextContent('No ratings yet');
    expect(tutorCalls(calls)[0].toString()).toBe('limit=25&offset=0');

    await screen.findByRole('option', { name: 'Bachelor in Mathematics' }); // programs load next to the list
    await user.selectOptions(screen.getByLabelText('Program'), 'Bachelor in Mathematics');
    await waitFor(() => expect(tutorCalls(calls).at(-1)?.get('program_id')).toBe('prog-math'));

    await user.type(screen.getByLabelText('Subject'), 'algo');
    await user.click(await screen.findByRole('button', { name: /Algorithms/ }));
    await waitFor(() => expect(tutorCalls(calls).at(-1)?.get('subject_id')).toBe(SUBJECT.id));
    expect(tutorCalls(calls).at(-1)?.get('program_id')).toBe('prog-math');
    expect(router.state.location.search).toBe(`?program_id=prog-math&subject_id=${SUBJECT.id}`);

    await user.click(screen.getByRole('button', { name: 'Change subject' }));
    await waitFor(() => expect(router.state.location.search).toBe('?program_id=prog-math'));
  });

  it('restores the filters from the URL', async () => {
    const { calls } = mockCoordinator({ 'GET /api/v1/coordinator/tutors': () => json(200, page([])) });
    renderApp(`/coordinator/tutors?program_id=prog-cs&subject_id=${SUBJECT.id}`);

    expect(await screen.findByText('No tutors match these filters.')).toBeInTheDocument();
    expect(await screen.findByText('Algorithms')).toBeInTheDocument();
    expect(await screen.findByRole('option', { name: 'Bachelor in Computer Science', selected: true })).toBeInTheDocument();
    const query = tutorCalls(calls)[0];
    expect(query.get('program_id')).toBe('prog-cs');
    expect(query.get('subject_id')).toBe(SUBJECT.id);
  });

  it('says when the faculty has no tutors yet', async () => {
    mockCoordinator({ 'GET /api/v1/coordinator/tutors': () => json(200, page([])) });
    renderApp('/coordinator/tutors');

    expect(await screen.findByText('Your faculty has no approved tutors yet.')).toBeInTheDocument();
  });

  it('is only for coordinators', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderApp('/coordinator/tutors');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
    expect(calls.some((c) => c.url.startsWith('/api/v1/coordinator/'))).toBe(false);
  });
});
