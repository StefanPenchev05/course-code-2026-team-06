# Browser journey tests (Playwright)

These tests click through the app in a real Chromium browser against the **real** Go API,
PostgreSQL and Redis. Nothing is mocked, so a passing run is real frontend-to-backend evidence
(AGENTS.md, frontend §14). The fast unit/component tests with a mocked API are separate (`npm test`).

## What you need

- The backend's tools: Go, PostgreSQL with `psql`, Redis and the `migrate` CLI
  (see the root `README.md` and `docs/identity.md`).
- Playwright's browser, once per machine: `npx playwright install chromium`.
- A **disposable** database. Each run adds a test faculty, program, subject, semester,
  coordinator, students, tutors, free times and a session request to it, and they stay there afterwards.

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

Students are created through the real sign-up page or the real `POST /auth/register`, with a new
email and student number every run.

There is no API for creating faculties, programs, subjects, semesters or coordinators: the
university provisions them. So `global-setup.ts` inserts one faculty with one program and subject,
a semester, and that faculty's coordinator with `psql`, the same way
`backend/tests/integration/identity_test.go` does. It then activates the coordinator through the
real `POST /auth/activate-coordinator`. The schema allows one coordinator per faculty, so each run
gets its own faculty and never touches real ones.

Everything else goes through the real API. `backend.ts` has helpers that do, as API calls, what
other issues' screens will do (for example the coordinator approving a registration or a tutor
application, or a tutor publishing free time), so a journey can start from "an approved tutor with
free time" without depending on screens that don't exist yet.

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
- The server forgets a logged-in session while the browser keeps its cookie (what a timeout looks
  like) → the next request gets a real 401 → "Your session ended" → logging in again returns to
  the page the student was opening.
- The home-faculty coordinator suspends a logged-in student (real `POST /coordinator/users/{id}/suspend`)
  → the next request gets a real 403 → "This account can't be used right now"; logging in is refused.

## Journeys covered (issue #29)

- An approved tutor with free time and a student whose application is *not* approved both exist.
  A verified student searches the subject: only the approved tutor is listed, with the subject and
  rating; the degree filter narrows and restores the list; the profile shows the subject, rating
  count and the free time in the student's time zone (fixed to Europe/Luxembourg in the test);
  the private grade from the application appears nowhere on the page.

## Journeys covered (issue #31)

- An approved tutor has one weekly time, created through the real API in setup (the page can't create
  weekly times yet: the API has no list of semesters to choose from). In the browser the tutor sees
  it in its local time and zone, adds a one-off time, gets the real overlap error, edits the weekly
  time, skips one weekly date and restores it. A student then books the one-off time (real
  `POST /sessions`), and removing it is refused with the "a student has a booking" explanation.

## Journeys covered (issue #32)

- Two approved tutors are free at the same time. A verified student finds the first one through
  search and books in three steps from the profile (choose a free time, fill in purpose, length and
  description, send). My sessions shows it as "Waiting for the tutor" with "You are the student";
  the tutor's own API view has the tutor role. Booking the second tutor at the same time is refused
  by the database's overlap rule (real 409) with a clear message. Cancelling requires a reason;
  afterwards the session leaves the active list and shows as cancelled with the reason.

Failed runs keep a screenshot and trace in `test-results/` (`npx playwright show-trace <file>`).
