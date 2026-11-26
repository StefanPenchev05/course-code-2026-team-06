import type { RouteObject } from 'react-router';
import { HomePage } from '../pages/HomePage';
import { LoginPage } from '../pages/LoginPage';
import { MySessionsPage } from '../pages/MySessionsPage';
import { NotFoundPage } from '../pages/NotFoundPage';
import { RegisterPage } from '../pages/RegisterPage';
import { TeachingPage } from '../pages/TeachingPage';
import { TutorAvailabilityPage } from '../pages/TutorAvailabilityPage';
import { TutorProfilePage } from '../pages/TutorProfilePage';
import { TutorSearchPage } from '../pages/TutorSearchPage';
import { RouteErrorPage } from '../pages/RouteErrorPage';
import { AppLayout } from './AppLayout';
import { PublicOnly, RequireAuth, RequireRole } from './guards';
import { CoordinatorTutorApplicationPage } from '../pages/CoordinatorTutorApplicationPage';
import { CoordinatorTutorApplicationsPage } from '../pages/CoordinatorTutorApplicationsPage';
import { TutorApplicationsPage } from '../pages/TutorApplicationsPage';
import { TutorActivityPage } from '../pages/TutorActivityPage';
import { CoordinatorHomePage } from '../pages/CoordinatorHomePage';
import { CoordinatorTutorPage } from '../pages/CoordinatorTutorPage';
import { CoordinatorTutorsPage } from '../pages/CoordinatorTutorsPage';

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
              { path: 'sessions', element: <MySessionsPage /> },
              { path: 'tutor-applications', element: <TutorApplicationsPage /> },
            ],
          },
          {
            element: <RequireRole role="tutor" />,
            children: [
              { path: 'tutor/availability', element: <TutorAvailabilityPage /> },
              { path: 'tutor', element: <TeachingPage /> },
              { path: 'tutor/activity', element: <TutorActivityPage /> },
            ],
          },
          {
            element: <RequireRole role="coordinator" />,
            children: [
              { path: 'coordinator/tutor-applications', element: <CoordinatorTutorApplicationsPage /> },
              { path: 'coordinator/tutor-applications/:id', element: <CoordinatorTutorApplicationPage /> },
              { path: 'coordinator', element: <CoordinatorHomePage /> },
              { path: 'coordinator/tutors', element: <CoordinatorTutorsPage /> },
              { path: 'coordinator/tutors/:id', element: <CoordinatorTutorPage /> },
            ],
          },
        ],
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
];
