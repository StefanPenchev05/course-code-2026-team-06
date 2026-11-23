import { render } from '@testing-library/react';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { routes } from '../../app/routes';
import { AuthProvider } from '../auth/AuthProvider';

// TEST DOUBLE helpers for the #30 tests (see src/test/mockApi.ts). Not used by the app.

export const PROGRAM_ID = '88888888-8888-8888-8888-888888888888';

export const coordinatorUser = {
  id: '44444444-4444-4444-4444-444444444444',
  name: 'Carla Coordinator',
  email: 'carla.coordinator@uni.lu',
  roles: ['coordinator'],
  verified_student: false,
  coordinator_faculty_id: '55555555-5555-5555-5555-555555555555',
};

/** A list response in the API's { data, pagination } shape. */
export function page<T>(data: T[], hasMore = false) {
  return { data, pagination: { limit: 25, offset: 0, has_more: hasMore } };
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
