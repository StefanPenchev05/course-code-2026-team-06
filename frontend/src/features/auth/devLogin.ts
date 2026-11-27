/**
 * The email/password form for LOCAL TEST ACCOUNTS (the legacy POST /auth/login
 * and /auth/register), kept only for development: the local demo without
 * Keycloak (docs/local-demo.md) and the Playwright journeys still log in this
 * way until #66/#67 move accounts to SSO and retire passwords.
 *
 * Release builds never show it. Every call site writes
 * `import.meta.env.DEV && devPasswordLogin()`, and `vite build` replaces
 * import.meta.env.DEV with false, so the form is not even in the bundle.
 *
 * To see the release screens while developing, start Vite with
 * VITE_DEV_PASSWORD_LOGIN=false.
 */
export function devPasswordLogin(): boolean {
  return import.meta.env.VITE_DEV_PASSWORD_LOGIN !== 'false';
}
