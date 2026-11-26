import { render } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { routes } from '../../app/routes';
import { AuthProvider } from '../auth/AuthProvider';

// TEST DOUBLE helpers for the #35 tests (see src/test/mockApi.ts). Not used by the app.

export const FACULTY_ID = '55555555-5555-5555-5555-555555555555';
export const TUTOR_ID = '77777777-7777-7777-7777-777777777777';

export const coordinator = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: FACULTY_ID,
};

/** A list response in the API's { data, pagination } shape. */
export function page<T>(data: T[], hasMore = false, limit = 25) {
  return { data, pagination: { limit, offset: 0, has_more: hasMore } };
}

export function tutorSummary(overrides: Record<string, unknown> = {}) {
  return {
    id: TUTOR_ID,
    name: 'Tom Tutor',
    program_id: 'prog-cs',
    program_name: 'Bachelor in Computer Science',
    faculty_id: FACULTY_ID,
    approved_at: '2026-10-01T09:00:00Z',
    active: true,
    completed_sessions: 4,
    hours_taught: 6,
    average_rating: 4.67,
    rating_count: 3,
    ...overrides,
  };
}

/** Renders the whole app (real routes, guards and AuthProvider) at `path`. Call mockApi first. */
export function renderApp(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  );
  return router;
}
