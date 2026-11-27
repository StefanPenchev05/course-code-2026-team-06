import { act, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider, type InitialEntry } from 'react-router';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { routes } from '../app/routes';
import { AuthProvider } from '../features/auth/AuthProvider';
import { browser, rememberAttempt } from '../features/auth/sso';
import { json, mockApi, verifiedStudent } from '../test/mockApi';

// Component tests with a mocked API (TEST DOUBLE). The real API is exercised by tests/e2e/sso.spec.ts.

const signedOut = { 'GET /api/v1/auth/me': () => json(401, { error: 'authentication required' }) };
/** What the browser reports for the API's 302 to the university when the redirect isn't followed. */
const redirectToUniversity = () => ({ type: 'opaqueredirect', status: 0 }) as Response;

function renderAt(entries: InitialEntry[], initialIndex = entries.length - 1) {
  const router = createMemoryRouter(routes, { initialEntries: entries, initialIndex });
  const view = render(
    <AuthProvider>
      <RouterProvider router={router} />
    </AuthProvider>,
  );
  return { router, ...view };
}

/** The screens of a release build: no development test-account form. */
function releaseBuild() {
  vi.stubEnv('VITE_DEV_PASSWORD_LOGIN', 'false');
}

function signInButton() {
  return screen.findByRole('button', { name: 'Sign in with University of Luxembourg' });
}

function backFromTheUniversityPage() {
  act(() => {
    window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true }));
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  sessionStorage.clear();
});

describe('login page in a release build', () => {
  it('offers only the university sign-in: no password, no email, no registration', async () => {
    releaseBuild();
    mockApi(signedOut);
    const { container } = renderAt(['/login']);

    expect(await signInButton()).toBeEnabled();
    expect(container.querySelector('input[type="password"]')).toBeNull();
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Register' })).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'Development test accounts' })).not.toBeInTheDocument();
    expect(screen.getByText(/university password only on the university's own page/)).toBeInTheDocument();
  });

  it('sends /register to the login page', async () => {
    releaseBuild();
    mockApi(signedOut);
    const { router } = renderAt(['/register']);

    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');
  });
});

describe('signing in with the university account', () => {
  it('goes through the fixed Tutti address and ignores addresses in the URL', async () => {
    const { fetchMock } = mockApi({ ...signedOut, 'GET /api/v1/auth/oidc/login': redirectToUniversity });
    const goTo = vi.spyOn(browser, 'goTo').mockImplementation(() => {});
    // A crafted link, arriving from a protected page.
    renderAt([{ pathname: '/login', search: '?next=https://evil.example&redirect_uri=https://evil.example', state: { from: { pathname: '/sessions', search: '' } } }]);
    const user = userEvent.setup();

    await user.click(await signInButton());

    await vi.waitFor(() => expect(goTo).toHaveBeenCalledOnce());
    expect(goTo).toHaveBeenCalledWith('/api/v1/auth/oidc/login');
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/oidc/login', expect.objectContaining({ redirect: 'manual' }));
    expect(JSON.parse(sessionStorage.getItem('tutti.universitySignIn') ?? '{}')).toMatchObject({ returnTo: '/sessions' });
    expect(screen.getByRole('button', { name: 'Sign in with University of Luxembourg' })).toHaveAttribute('aria-busy', 'true');
  });

  it('works with the keyboard', async () => {
    mockApi({ ...signedOut, 'GET /api/v1/auth/oidc/login': redirectToUniversity });
    const goTo = vi.spyOn(browser, 'goTo').mockImplementation(() => {});
    renderAt(['/login']);
    const user = userEvent.setup();

    (await signInButton()).focus();
    await user.keyboard('{Enter}');

    await vi.waitFor(() => expect(goTo).toHaveBeenCalledOnce());
  });

  it.each([
    [404, "University sign-in isn't available here"],
    [429, 'Too many sign-in attempts'],
    [503, "Signing in isn't possible right now"],
  ] as const)('stays in the app and explains HTTP %i', async (status, title) => {
    mockApi({ ...signedOut, 'GET /api/v1/auth/oidc/login': () => json(status, { error: 'x' }) });
    const goTo = vi.spyOn(browser, 'goTo').mockImplementation(() => {});
    renderAt(['/login']);
    const user = userEvent.setup();

    await user.click(await signInButton());

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(title);
    // Keyboard and screen-reader users are taken to the message.
    expect(document.activeElement).toContainElement(alert);
    expect(goTo).not.toHaveBeenCalled();
    expect(sessionStorage.getItem('tutti.universitySignIn')).toBeNull();
    expect(screen.getByRole('button', { name: 'Sign in with University of Luxembourg' })).toBeEnabled();
  });

  it('explains when Tutti cannot be reached at all', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) =>
        String(input).includes('/oidc/login') ? Promise.reject(new TypeError('Failed to fetch')) : json(401, {}),
      ),
    );
    renderAt(['/login']);
    const user = userEvent.setup();

    await user.click(await signInButton());

    expect(await screen.findByRole('alert')).toHaveTextContent("We can't reach Tutti right now");
  });

  it('explains an attempt that came back without a session, once', async () => {
    mockApi(signedOut);
    rememberAttempt('/sessions');
    const first = renderAt(['/login']);

    expect(await screen.findByText("Signing in with your university account didn't finish")).toBeInTheDocument();
    expect(sessionStorage.getItem('tutti.universitySignIn')).toBeNull();

    first.unmount();
    renderAt(['/login']);
    expect(await signInButton()).toBeInTheDocument();
    expect(screen.queryByText("Signing in with your university account didn't finish")).not.toBeInTheDocument();
  });

  it('resets the page when Back restores it from the browser cache', async () => {
    mockApi({ ...signedOut, 'GET /api/v1/auth/oidc/login': redirectToUniversity });
    const goTo = vi.spyOn(browser, 'goTo').mockImplementation(() => {});
    renderAt(['/login']);
    const user = userEvent.setup();
    await user.click(await signInButton());
    await vi.waitFor(() => expect(goTo).toHaveBeenCalledOnce());
    expect(screen.getByRole('button', { name: 'Sign in with University of Luxembourg' })).toBeDisabled();

    backFromTheUniversityPage();

    expect(screen.getByRole('button', { name: 'Sign in with University of Luxembourg' })).toBeEnabled();
    expect(screen.getByText("Signing in with your university account didn't finish")).toBeInTheDocument();
  });
});

describe('after the university sign-in', () => {
  it('opens the page the user wanted, replacing the landing page in the history', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    rememberAttempt('/sessions');
    // The API always redirects to "/" (OIDC_POST_LOGIN_PATH).
    const { router } = renderAt(['/']);

    expect(await screen.findByRole('heading', { name: 'My sessions' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/sessions');
    expect(router.state.historyAction).toBe('REPLACE');
    expect(sessionStorage.getItem('tutti.universitySignIn')).toBeNull();
  });

  it('stays on the home page when the remembered page is not an app page', async () => {
    mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    sessionStorage.setItem('tutti.universitySignIn', JSON.stringify({ returnTo: '//evil.example', startedAt: Date.now() }));
    const { router } = renderAt(['/']);

    expect(await screen.findByRole('heading', { name: 'Welcome, Ana Student' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/');
  });

  it('re-checks the session when Back restores a page from the browser cache', async () => {
    const { calls } = mockApi({ 'GET /api/v1/auth/me': () => json(200, verifiedStudent) });
    renderAt(['/']);
    expect(await screen.findByRole('heading', { name: 'Welcome, Ana Student' })).toBeInTheDocument();
    const before = calls.filter((c) => c.url === '/api/v1/auth/me').length;

    backFromTheUniversityPage();

    await vi.waitFor(() => expect(calls.filter((c) => c.url === '/api/v1/auth/me').length).toBe(before + 1));
  });
});

describe('logging out', () => {
  it('ends the Tutti session, explains the university session, and Back does not reopen protected pages', async () => {
    let session = true;
    const { calls } = mockApi({
      'GET /api/v1/auth/me': () => (session ? json(200, verifiedStudent) : json(401, { error: 'authentication required' })),
      'POST /api/v1/auth/logout': () => {
        session = false;
        return json(204);
      },
    });
    const { router } = renderAt(['/', '/sessions']);
    const user = userEvent.setup();
    expect(await screen.findByRole('heading', { name: 'My sessions' })).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Log out' }));

    expect(await screen.findByText("You're logged out of Tutti")).toBeInTheDocument();
    expect(screen.getByText(/doesn't sign you out of your University of Luxembourg account/)).toBeInTheDocument();
    expect(calls.some((c) => c.method === 'POST' && c.url === '/api/v1/auth/logout')).toBe(true);

    await act(async () => {
      await router.navigate(-1);
    });

    expect(await screen.findByRole('heading', { name: 'Log in' })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe('/login');
    expect(screen.queryByRole('heading', { name: 'Welcome, Ana Student' })).not.toBeInTheDocument();
  });
});
