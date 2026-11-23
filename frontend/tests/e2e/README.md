# Browser journey tests (Playwright)

These tests click through the app in a real Chromium browser against the **real** Go API,
PostgreSQL and Redis. Nothing is mocked, so a passing run is real frontend-to-backend evidence
(AGENTS.md, frontend §14). The fast unit/component tests with a mocked API are separate (`npm test`).

## What you need

- The backend's tools: Go, PostgreSQL with `psql`, Redis and the `migrate` CLI
  (see the root `README.md` and `docs/identity.md`).
- Playwright's browser, once per machine: `npx playwright install chromium`.
- A **disposable** database. Each run adds a test faculty, program, coordinator and students
  to it, and they stay there afterwards.

## Run

1. Start PostgreSQL and Redis, and apply the migrations to the test database.
2. Start the API from the repository root with these settings (only for this process; don't change `.env`):

   ```sh
   set -a; . ./.env; set +a
   PUBLIC_ORIGIN=http://localhost:5173 UNIVERSITY_EMAIL_DOMAINS=example.test \
     DATABASE_URL="$DATABASE_URL" go -C backend run ./cmd/api
   ```

3. In `frontend/`, point the tests at the same database and run them. Playwright starts Vite itself
   (or reuses one already running on port 5173):

   ```sh
   E2E_DATABASE_URL="$DATABASE_URL" npm run test:e2e
   ```

If the API isn't running, the origin is wrong, or the database can't be reached, the run stops
before the first test and says what to fix.

## How the test data is made

Students are created through the real sign-up page, with a new email and student number every run.

There is no API for creating faculties, programs or coordinators: the university provisions them.
So `global-setup.ts` inserts one faculty, one program and that faculty's coordinator with `psql`,
the same way `backend/tests/integration/identity_test.go` does. It then activates the coordinator
through the real `POST /auth/activate-coordinator`. The schema allows one coordinator per faculty,
so each run gets its own faculty and never touches real ones.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `E2E_DATABASE_URL` | (required) | The database the API uses, for provisioning test data |
| `E2E_BASE_URL` | `http://localhost:5173` | Where the frontend runs; must equal the API's `PUBLIC_ORIGIN` |
| `E2E_EMAIL_DOMAIN` | `example.test` | A domain listed in the API's `UNIVERSITY_EMAIL_DOMAINS` |

## Journeys covered (issue #28)

- Sign up → logged in → pending notice → student pages locked → reload keeps the session →
  log out → protected page redirects to login → log in → back on that page.
- Sign-up errors from the server: an email outside the allowed domains (422) and an existing
  account (409), with the typed input kept.
- Login with a wrong password.
- A coordinator rejects a registration with a reason (through the API) → the student sees the
  reason, corrects the details and resubmits → pending again.

Failed runs keep a screenshot and trace in `test-results/` (`npx playwright show-trace <file>`).
