import type { RouteObject } from 'react-router';
import { ComingSoonPage } from '../pages/ComingSoonPage';
import { HomePage } from '../pages/HomePage';
import { LoginPage } from '../pages/LoginPage';
import { NotFoundPage } from '../pages/NotFoundPage';
import { RegisterPage } from '../pages/RegisterPage';
import { TutorProfilePage } from '../pages/TutorProfilePage';
import { TutorSearchPage } from '../pages/TutorSearchPage';
import { RouteErrorPage } from '../pages/RouteErrorPage';
import { AppLayout } from './AppLayout';
import { PublicOnly, RequireAuth, RequireRole } from './guards';

/**
 * All screens of the app. Replace a ComingSoonPage with the real page when
 * its issue is implemented; the issue number is shown on the placeholder.
 */
export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppLayout />,
    errorElement: <RouteErrorPage />,
    children: [
      {
        element: <PublicOnly />,
        children: [
          { path: 'login', element: <LoginPage /> },
          { path: 'register', element: <RegisterPage /> },
        ],
      },
      {
        element: <RequireAuth />,
        children: [
          { index: true, element: <HomePage /> },
          {
            element: <RequireRole verifiedStudent />,
            children: [
              { path: 'tutors', element: <TutorSearchPage /> },
              { path: 'tutors/:id', element: <TutorProfilePage /> },
              { path: 'sessions', element: <ComingSoonPage title="My sessions" issue={32} /> },
              { path: 'tutor-applications', element: <ComingSoonPage title="Become a tutor" issue={30} /> },
            ],
          },
          {
            element: <RequireRole role="tutor" />,
            children: [{ path: 'tutor/*', element: <ComingSoonPage title="Teaching" issue={33} /> }],
          },
          {
            element: <RequireRole role="coordinator" />,
            children: [{ path: 'coordinator/*', element: <ComingSoonPage title="Coordinator" issue={35} /> }],
          },
        ],
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
];
