import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AuthProvider } from '../features/auth/AuthProvider';
import { json, mockApi, verifiedStudent } from '../test/mockApi';
import { routes } from './routes';

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

describe('routing and authentication', () => {
  it('sends logged-out visitors to the login page', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }) });
    const router = renderAt('/sessions');

    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');
  });

  it('restores an existing session on page load', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/');

    expect(await screen.findByRole('heading', { name: 'Welcome, Ana Student' })).toBeInTheDocument();
  });

  it('logs in and returns to the page the user wanted', async () => {
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }),
      'POST /api/v1/auth/login': () => json(200, verifiedStudent),
    });
    const router = renderAt('/sessions');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('University email'), 'ana@uni.lu');
    await user.type(screen.getByLabelText('Password'), 'correct horse battery');
    await user.click(screen.getByRole('button', { name: 'Log in' }));

    expect(await screen.findByRole('heading', { name: 'My sessions' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/sessions');
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ email: 'ana@uni.lu', password: 'correct horse battery' });
  });

  it('shows a clear message for wrong credentials', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }),
      'POST /api/v1/auth/login': () => json(401, { error: 'authentication required' }),
    });
    renderAt('/login');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('University email'), 'ana@uni.lu');
    await user.type(screen.getByLabelText('Password'), 'wrong password!');
    await user.click(screen.getByRole('button', { name: 'Log in' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('Email or password is incorrect.');
  });

  it('explains a refused login without claiming the password is wrong', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }),
      'POST /api/v1/auth/login': () => json(403, { error: 'forbidden' }),
    });
    renderAt('/login');
    const user = userEvent.setup();

    await user.type(await screen.findByLabelText('University email'), 'ana@uni.lu');
    await user.type(screen.getByLabelText('Password'), 'correct horse battery');
    await user.click(screen.getByRole('button', { name: 'Log in' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(/can't log in right now/);
    expect(alert).not.toHaveTextContent(/incorrect/);
  });

  it('validates the form before calling the API', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(401, {}) });
    renderAt('/login');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Log in' }));

    expect(screen.getByLabelText('University email')).toHaveAttribute('aria-invalid', 'true');
    expect(calls.some((c) => c.method === 'POST')).toBe(false);
  });

  it('hides coordinator pages from students', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt('/coordinator');

    expect(await screen.findByRole('heading', { name: "You don't have access to this page" })).toBeInTheDocument();
  });

  it('tells a pending student their registration is awaiting approval', async () => {
    mockApi({
      'GET /api/v1/auth/me': () =>
        json(200, { ...verifiedStudent, verified_student: false, verification_status: 'PENDING' }),
    });
    renderAt('/');

    expect(await screen.findByText('Your registration is waiting for approval')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Find a tutor' })).not.toBeInTheDocument();
  });

  it('logs out and returns to the login page', async () => {
    mockApi({
      'GET /api/v1/auth/me': () => json(200, verifiedStudent),
      'POST /api/v1/auth/logout': () => json(204),
    });
    const router = renderAt('/');
    const user = userEvent.setup();

    await user.click(await screen.findByRole('button', { name: 'Log out' }));

    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');
  });
});
