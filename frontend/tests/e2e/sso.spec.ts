import { expect, test } from '@playwright/test';
import { verifiedStudent } from './backend';
import { E2E } from './env';
import { logIn, logOut } from './ui';

// Issue #68: the university sign-in and logout against the REAL Go API.
// By default the API runs without OIDC (OIDC_ENABLED unset), so the button must
// explain that and stay in the app. When the API runs with OIDC_ENABLED=true and
// E2E_OIDC_ISSUER is set, the button must leave for exactly that issuer.

const signInButton = { name: 'Sign in with University of Luxembourg' };

test('the university sign-in goes only through the fixed Tutti address', async ({ page }) => {
  // A crafted link: the addresses in it must not be used anywhere.
  await page.goto('/login?next=https://evil.example&redirect_uri=https://evil.example');
  const firstRequest = page.waitForRequest((request) => new URL(request.url()).pathname === '/api/v1/auth/oidc/login');

  await page.getByRole('button', signInButton).click();

  expect(new URL((await firstRequest).url()).search).toBe('');
  if (E2E.oidcIssuer) {
    await page.waitForURL((url) => url.href.startsWith(`${E2E.oidcIssuer}/protocol/openid-connect/auth`));
    const url = new URL(page.url());
    expect(url.searchParams.get('response_type')).toBe('code');
    expect(url.searchParams.get('code_challenge_method')).toBe('S256');
    expect(url.searchParams.get('redirect_uri')).toBe(new URL('/api/v1/auth/oidc/callback', E2E.baseURL).href);
  } else {
    await expect(page.getByRole('alert')).toContainText("University sign-in isn't available here");
    await expect(page).toHaveURL(/\/login\?next=/);
    await expect(page.getByRole('button', signInButton)).toBeEnabled();
  }
});

test('logging out ends the Tutti session, explains the university session, and Back stays logged out', async ({ page }) => {
  const student = await verifiedStudent('sso-logout');
  await page.goto('/login');
  await logIn(page, student.email, student.password);
  await expect(page.getByRole('heading', { name: `Welcome, ${student.name}` })).toBeVisible();
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'My sessions' }).click();
  await expect(page.getByRole('heading', { name: 'My sessions', level: 1 })).toBeVisible();

  await logOut(page);

  await expect(page.getByText("You're logged out of Tutti")).toBeVisible();
  await expect(page.getByText(/doesn't sign you out of your University of Luxembourg account/)).toBeVisible();
  // The API agrees: the old session cookie no longer works.
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(401);

  await page.goBack();
  await expect(page.getByRole('heading', { name: 'Log in', level: 1 })).toBeVisible();
  await expect(page.getByRole('heading', { name: `Welcome, ${student.name}` })).toHaveCount(0);
});
