import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { json, mockApi, verifiedStudent } from '../test/mockApi';

// Component tests with a mocked API (test double). Real-backend journeys are in tests/e2e.

const SUBJECT = { id: '00000000-0000-0000-0000-000000000221', code: 'ALG-1', name: 'Algorithms' };
const OFFERING = {
  id: 'off-1',
  subject_id: SUBJECT.id,
  subject_name: 'Algorithms',
  description: 'Exam preparation and exercises.',
  active: true,
};
const TUTOR_ID = '00000000-0000-0000-0000-0000000000aa';
// The API never sends grades in public views. The mock adds one anyway to prove the UI ignores unknown fields.
const MATCH = {
  id: TUTOR_ID,
  name: 'Tom Tutor',
  description: 'Third-year computing student.',
  active: true,
  average_rating: 4.5,
  rating_count: 8,
  offering: OFFERING,
  grade: '19.75',
};
const PROFILE = { ...MATCH, offering: undefined, offerings: [OFFERING], grade: '19.75' };

const me = { 'GET /api/v1/auth/me': () => json(200, verifiedStudent) };
const catalog = {
  'GET /api/v1/subjects': () => json(200, { data: [SUBJECT], pagination: { limit: 10, offset: 0, has_more: false } }),
  [`GET /api/v1/subjects/${SUBJECT.id}`]: () => json(200, { data: SUBJECT }),
  'GET /api/v1/faculties': () => json(200, { data: [{ id: 'f1', name: 'Demo Engineering' }] }),
};

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  );
  return router;
}

afterEach(() => vi.unstubAllGlobals());

describe('TutorSearchPage', () => {
  it('finds tutors for a chosen subject and links to their profile', async () => {
    const { calls } = mockApi({
      ...me,
      ...catalog,
      'GET /api/v1/tutors': () => json(200, { data: [MATCH], pagination: { limit: 20, offset: 0, has_more: false } }),
    });
    const router = renderAt('/tutors');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('Subject'), 'algo');
    await user.click(await screen.findByRole('button', { name: /Algorithms/ }));

    const card = (await screen.findByRole('link', { name: 'Tom Tutor' })).closest('li')!;
    expect(within(card).getByText('Algorithms · 4.5 ★ (8 ratings)')).toBeInTheDocument();
    expect(within(card).getByText('Exam preparation and exercises.')).toBeInTheDocument();
    expect(screen.queryByText(/19\.75/)).not.toBeInTheDocument();
    expect(router.state.location.search).toBe(`?subject_id=${SUBJECT.id}`);
    expect(calls.some((c) => c.url.includes(`/api/v1/tutors?subject_id=${SUBJECT.id}`))).toBe(true);
  });

  it('sends the faculty and degree filters to the API', async () => {
    const { calls } = mockApi({
      ...me,
      ...catalog,
      'GET /api/v1/tutors': () => json(200, { data: [MATCH], pagination: { limit: 20, offset: 0, has_more: false } }),
    });
    renderAt(`/tutors?subject_id=${SUBJECT.id}`);
    const user = userEvent.setup();

    expect(await screen.findByText('Algorithms')).toBeInTheDocument();
    await user.selectOptions(await screen.findByLabelText("Tutor's faculty"), 'f1');
    await user.selectOptions(screen.getByLabelText("Tutor's degree"), 'MASTER');

    await screen.findByRole('link', { name: 'Tom Tutor' });
    const last = calls.filter((c) => c.url.startsWith('/api/v1/tutors?')).at(-1)!;
    expect(new URL(last.url, 'http://x').searchParams.get('faculty_id')).toBe('f1');
    expect(new URL(last.url, 'http://x').searchParams.get('level')).toBe('MASTER');
  });

  it('explains when the subject is not in the student program', async () => {
    mockApi({
      ...me,
      ...catalog,
      'GET /api/v1/tutors': () => json(422, { error: "invalid input: subject is not in the student's current program" }),
    });
    renderAt(`/tutors?subject_id=${SUBJECT.id}`);

    expect(await screen.findByRole('alert')).toHaveTextContent("This subject isn't part of your study program");
  });

  it('says so when nobody teaches the subject', async () => {
    mockApi({
      ...me,
      ...catalog,
      'GET /api/v1/tutors': () => json(200, { data: [], pagination: { limit: 20, offset: 0, has_more: false } }),
    });
    renderAt(`/tutors?subject_id=${SUBJECT.id}`);

    expect(await screen.findByText('No tutors found')).toBeInTheDocument();
  });

  it('loads more tutors on request', async () => {
    const second = { ...MATCH, id: 'other', name: 'Tina Tutor' };
    mockApi({
      ...me,
      ...catalog,
      'GET /api/v1/tutors': ({ url }) =>
        url.includes('offset=1')
          ? json(200, { data: [second], pagination: { limit: 20, offset: 1, has_more: false } })
          : json(200, { data: [MATCH], pagination: { limit: 20, offset: 0, has_more: true } }),
    });
    renderAt(`/tutors?subject_id=${SUBJECT.id}`);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Show more tutors' }));

    expect(await screen.findByRole('link', { name: 'Tina Tutor' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: '2 tutors' })).toBeInTheDocument();
  });
});

describe('TutorProfilePage', () => {
  it('shows the offering, aggregate rating and free times, but no grade', async () => {
    const { calls } = mockApi({
      ...me,
      [`GET /api/v1/tutors/${TUTOR_ID}`]: () => json(200, { data: PROFILE }),
      [`GET /api/v1/tutors/${TUTOR_ID}/availability`]: () =>
        json(200, {
          data: [
            {
              id: 'slot-1',
              tutor_id: TUTOR_ID,
              starts_at: '2030-01-07T09:00:00Z',
              ends_at: '2030-01-07T10:30:00Z',
              allowed_mode: 'ONLINE',
              is_recurring: true,
            },
          ],
          pagination: { limit: 100, offset: 0, has_more: false },
        }),
    });
    renderAt(`/tutors/${TUTOR_ID}?subject_id=${SUBJECT.id}`);

    expect(await screen.findByRole('heading', { name: 'Tom Tutor' })).toBeInTheDocument();
    expect(screen.getByText('4.5 ★ (8 ratings)')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Algorithms' })).toBeInTheDocument();
    expect(await screen.findByText('Online')).toBeInTheDocument();
    expect(screen.queryByText(/19\.75/)).not.toBeInTheDocument();
    expect(screen.queryByText(/grade/i)).not.toBeInTheDocument();
    const availability = calls.find((c) => c.url.includes('/availability'))!;
    const query = new URL(availability.url, 'http://x').searchParams;
    expect(new Date(query.get('to')!).getTime() - new Date(query.get('from')!).getTime()).toBe(14 * 24 * 60 * 60 * 1000);
  });

  it('explains when the tutor is not available to this student', async () => {
    mockApi({ ...me, [`GET /api/v1/tutors/${TUTOR_ID}`]: () => json(404, { error: 'not found' }) });
    renderAt(`/tutors/${TUTOR_ID}`);

    expect(await screen.findByRole('heading', { name: 'Tutor not available' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '← Back to search' })).toHaveAttribute('href', '/tutors');
  });

  it('is locked for students who are not approved yet', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, { ...verifiedStudent, verified_student: false, verification_status: 'PENDING' }) });
    renderAt(`/tutors/${TUTOR_ID}`);

    expect(await screen.findByText('This becomes available once a coordinator approves your registration.')).toBeInTheDocument();
  });
});
