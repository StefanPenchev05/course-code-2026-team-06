import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { createBrowserRouter, RouterProvider } from 'react-router';
import { RealtimeProvider } from './app/RealtimeProvider';
import { routes } from './app/routes';
import { AuthProvider } from './features/auth/AuthProvider';
import './styles.css';

const router = createBrowserRouter(routes);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider>
      <RealtimeProvider>
        <RouterProvider router={router} />
      </RealtimeProvider>
    </AuthProvider>
  </StrictMode>,
);
