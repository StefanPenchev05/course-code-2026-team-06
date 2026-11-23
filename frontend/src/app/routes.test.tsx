import { act, render, screen } from '@testing-library/react';
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
    // A failed login is not an ended session.
    expect(screen.queryByText('Your session ended')).not.toBeInTheDocument();
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
    // Logging out on purpose shows no "session ended" notice.
    expect(screen.queryByText('Your session ended')).not.toBeInTheDocument();
  });
});

describe('when the server ends the session', () => {
  function returnToTab() {
    act(() => {
      document.dispatchEvent(new Event('visibilitychange'));
    });
  }

  it('explains an expired session and returns to the same page after logging in again', async () => {
    let session = true;
    mockApi({
      'GET /api/v1/auth/me': () => (session ? json(200, verifiedStudent) : json(401, { error: 'invalid credentials or session' })),
      'POST /api/v1/auth/login': () => {
        session = true;
        return json(200, verifiedStudent);
      },
    });
    const router = renderAt('/sessions');
    const user = userEvent.setup();
    expect(await screen.findByRole('heading', { name: 'My sessions' })).toBeInTheDocument();

    session = false; // e.g. the session timed out on the server
    returnToTab();

    expect(await screen.findByText('Your session ended')).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');

    await user.type(screen.getByLabelText('University email'), 'ana@uni.lu');
    await user.type(screen.getByLabelText('Password'), 'correct horse battery');
    await user.click(screen.getByRole('button', { name: 'Log in' }));

    expect(await screen.findByRole('heading', { name: 'My sessions' })).toBeInTheDocument();
  });

  it('explains a suspended or closed account found during use', async () => {
    let blocked = false;
    mockApi({
      'GET /api/v1/auth/me': () => (blocked ? json(403, { error: 'permission denied' }) : json(200, verifiedStudent)),
    });
    renderAt('/');
    expect(await screen.findByRole('heading', { name: 'Welcome, Ana Student' })).toBeInTheDocument();

    blocked = true; // a coordinator suspended the account
    returnToTab();

    expect(await screen.findByText("This account can't be used right now")).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Log in' })).toBeInTheDocument();
  });

  it('re-checks the account when any request is forbidden', async () => {
    let blocked = false;
    mockApi({
      'GET /api/v1/auth/me': () => (blocked ? json(403, { error: 'permission denied' }) : json(200, verifiedStudent)),
      'GET /api/v1/subjects': () => json(403, { error: 'permission denied' }),
    });
    renderAt('/tutors');
    const user = userEvent.setup();
    const subject = await screen.findByLabelText('Subject');

    blocked = true;
    await user.type(subject, 'algo');

    expect(await screen.findByText("This account can't be used right now")).toBeInTheDocument();
  });

  it('explains a blocked account on page load instead of "can\'t reach Tutti"', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(403, { error: 'permission denied' }) });
    renderAt('/');

    expect(await screen.findByText("This account can't be used right now")).toBeInTheDocument();
    expect(screen.queryByText("We can't reach Tutti right now")).not.toBeInTheDocument();
  });

  it('does not show a notice for an ordinary logged-out visit', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }) });
    renderAt('/sessions');

    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(screen.queryByText('Your session ended')).not.toBeInTheDocument();
  });
});
