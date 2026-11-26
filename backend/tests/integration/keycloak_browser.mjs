// Opt-in protocol/browser test, invoked by TestOIDCKeycloakBrowser.
// Uses the repository's installed Playwright; never records tokens or passwords.
import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
const require = createRequire(new URL('../../../frontend/package.json', import.meta.url));
const { chromium, request } = require('@playwright/test');
const config = JSON.parse(process.env.TUTTI_KEYCLOAK_BROWSER_FIXTURE);
const browser = await chromium.launch({ headless: true });
let scenario = 'initialization';
try {
  for (const user of config.users) {
    scenario = user.scenario;
    const context = await browser.newContext();
    try {
      const page = await context.newPage();
      const errors = [];
      page.on('pageerror', () => errors.push('browser exception'));
      if (user.oldSession) {
        await context.addCookies([{ name: 'tutti_session', value: user.oldSession, url: config.origin, httpOnly: true, sameSite: 'Lax' }]);
        assert.equal((await page.request.get(`${config.origin}/api/v1/auth/me`)).status(), 200);
      }
      await page.goto(`${config.origin}/api/v1/auth/oidc/login`);
      await page.getByRole('textbox', { name: /username|email/i }).waitFor();
      const temporary = (await context.cookies(config.origin)).find((cookie) => cookie.name === 'tutti_oidc_tx');
      assert.ok(temporary?.httpOnly && temporary.sameSite === 'Lax');
      await page.getByRole('textbox', { name: /username|email/i }).fill(user.username);
      await page.getByLabel('Password', { exact: true }).fill(config.password);
      await page.getByRole('button', { name: /sign in/i }).click();
      if (user.status === 200) {
        await page.waitForURL(`${config.origin}/api/v1/auth/me`);
        const response = await page.request.get(`${config.origin}/api/v1/auth/me`);
        assert.equal(response.status(), 200);
        const me = await response.json();
        assert.equal(me.id, user.id);
        assert.equal(me.verified_student, user.verified);
        assert.deepEqual(me.roles.sort(), user.roles.sort());
        const appCookie = (await context.cookies(config.origin)).find((cookie) => cookie.name === 'tutti_session');
        assert.ok(appCookie?.httpOnly && appCookie.sameSite === 'Lax' && appCookie.value !== user.oldSession);
        assert.equal((await page.request.get(`${config.origin}/api/v1/semesters`)).status(), user.semesters);
        if (user.oldSession) {
          const old = await request.newContext({ extraHTTPHeaders: { Cookie: `tutti_session=${user.oldSession}` } });
          try { assert.equal((await old.get(`${config.origin}/api/v1/auth/me`)).status(), 401); }
          finally { await old.dispose(); }
        }
        assert.equal((await page.request.post(`${config.origin}/api/v1/auth/logout`, { headers: { Origin: config.origin } })).status(), 204);
        assert.equal((await page.request.get(`${config.origin}/api/v1/auth/me`)).status(), 401);
      } else {
        await page.waitForURL((url) => url.origin === config.origin && url.pathname === '/api/v1/auth/oidc/callback');
        const error = JSON.parse(await page.locator('body').innerText());
        assert.equal(error.error, user.status === 403 ? 'permission denied' : 'OIDC authentication failed');
        assert.equal((await page.request.get(`${config.origin}/api/v1/auth/me`)).status(), 401);
        assert.equal((await context.cookies(config.origin)).some((cookie) => cookie.name === 'tutti_session'), false);
      }
      assert.equal((await context.cookies(config.origin)).some((cookie) => cookie.name === 'tutti_oidc_tx'), false);
      assert.deepEqual(errors, []);
      assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
      console.log(`PASS: Keycloak browser ${user.scenario}`);
    } finally { await context.close(); }
  }
} catch {
  // Playwright call logs can include filled passwords or callback URLs. Keep
  // failures generic; never dump the Error, page HTML, cookies or trace here.
  console.error(`FAIL: Keycloak browser ${scenario}`);
  process.exitCode = 1;
} finally { await browser.close(); }
