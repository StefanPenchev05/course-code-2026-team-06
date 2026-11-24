import { defineConfig, devices } from '@playwright/test';

/**
 * Browser journey tests (npm run test:e2e). They drive a real browser against
 * the REAL Go API: nothing is mocked. See tests/e2e/README.md for the setup
 * (running API, migrated PostgreSQL with the catalog fixture, Redis).
 */
const baseURL = process.env.E2E_BASE_URL ?? 'http://localhost:5173';

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/e2e/global-setup.ts',
  // Journeys share one backend and database; run them one at a time.
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  // Starts Vite unless it is already running. The Go API must be started separately.
  webServer: {
    command: 'npm run dev',
    url: baseURL,
    reuseExistingServer: true,
    timeout: 60_000,
  },
});
