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

Except for the historical completion fixture described below, business setup goes through the real
API. `backend.ts` has helpers that do, as API calls, what
other issues' screens will do (for example the coordinator approving a registration or a tutor
application, or a tutor publishing free time), so a journey can start from "an approved tutor with
free time" without depending on screens that don't exist yet.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `E2E_DATABASE_URL` | (required) | The database the API uses, for provisioning test data |
| `E2E_BASE_URL` | `http://localhost:5173` | Where the frontend runs; must equal the API's `PUBLIC_ORIGIN` |
| `E2E_EMAIL_DOMAIN` | `example.test` | A domain listed in the API's `UNIVERSITY_EMAIL_DOMAINS` |
| `E2E_OIDC_ISSUER` | (empty) | Only if the API runs with `OIDC_ENABLED=true`: its issuer, e.g. `http://localhost:8180/realms/tutti-demo` (#68) |

The journeys log in through the **development test-account form** on `/login` (legacy password
login). Release builds don't have that form; they only offer "Sign in with University of
Luxembourg" (#68, see `frontend/README.md` section 10).

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

- An approved tutor with no availability opens My availability and **creates a weekly time in the
  browser**, choosing this run's semester from the real `GET /semesters`; adding an overlapping
  weekly time gets the real 409. The tutor sees it in its local time and zone, adds a one-off time,
  gets the real overlap error, edits the weekly time, skips one weekly date and restores it. A
  student then books the one-off time (real `POST /sessions`), and removing it is refused with the
  "a student has a booking" explanation. After the student cancels via the API, the browser removes
  the one-off time and deletes the weekly rule. Reloads and authenticated API reads verify that both
  deletions persisted.

## Journeys covered (issue #32)

- Two approved tutors are free at the same time. A verified student finds the first one through
  search and books in three steps from the profile (choose a free time, fill in purpose, length and
  description, send). My sessions shows it as "Waiting for the tutor" with "You are the student";
  the tutor's own API view has the tutor role. Booking the second tutor at the same time is refused
  by the database's overlap rule (real 409) with a clear message. Cancelling requires a reason;
  afterwards the session leaves the active list and shows as cancelled with the reason.

## Journeys covered (issue #33)

- Two students request sessions with one tutor. On the Teaching page the tutor declines one (a
  reason is required) and accepts the other (an online session needs a valid meeting link); the
  accepted one moves to "Confirmed sessions". Before the session ends, "Mark as completed" is not
  offered and the real server also refuses completion (409); answering the declined request again
  is refused (409). The students' real API views show ACCEPTED with the link and DECLINED with the
  reason, and the accepted student sees the link in My sessions.
- `session-lifecycle.spec.ts` adds an ended-session completion journey. The tutor confirms that
  it took place, it leaves the teaching queue, and both participants see Completed after a fresh
  read. An authenticated API read verifies the completing tutor; SQL reads verify status history
  and released participant reservations.
- Onsite acceptance requires a room. A separate student browser sees the accepted room; the tutor
  cancels from My sessions with a reason, both browsers see the cancellation, and the database
  confirms reservation release.
- While a tutor's accept form is open, the student cancels through the real API. Submitting the
  stale form gets a real 409, shows the conflict, and preserves CANCELLED after reload.
- A verified student without tutor approval cannot open either tutor screen; the real tutor-only
  endpoints (including semesters) return 403. Reading or cancelling an unrelated booking returns 404, leaves its status unchanged, and it never appears
  in that student's session list.

## Historical completion setup

Booking requires at least three hours' notice and a booked schedule is immutable. Waiting for
hours or overriding the browser clock would not test the server's completion rule. Instead,
`history-fixture.ts` inserts one accepted session that ended an hour ago, using the participants,
approved offering, policy, faculty and semester of a real API-created booking. It inserts the
corresponding status history in the same transaction, with all database constraints and triggers
still enabled. The future reference request is declined through the real API. **Only the subsequent
completion is claimed as a browser-to-backend mutation**, not the seeded historical acceptance.
`database.ts` centralizes psql access and quotes fixture values with psql variables.

## Repeatability and boundaries

Every journey provisions its own users, tutors and availability; visible names are unique as well
as emails and student numbers, so repeated discovery tests cannot match an earlier tutor. Each
run provisions its own catalog/coordinator. No journey needs another test to run first. For example:

```sh
E2E_DATABASE_URL="$DATABASE_URL" npm run test:e2e -- --repeat-each=2
```

No requests are intercepted or fulfilled by a fake API. API helpers deliberately bypass only
screens outside the journey's scope. Existing unit/component mocks remain in the separate suite.
The current implemented screens are account entry/verification, tutor discovery/profile, My
sessions, tutor availability and Teaching; all have real-backend journeys. Tutor applications and
coordinator routes remain placeholder pages, and weekly-rule creation has no UI yet. These tests
do not claim those features, feedback submission, email delivery, OIDC, or WebSocket recovery are
covered. The unrelated-user check uses the same faculty; cross-faculty authorization remains the
backend integration suite's responsibility. Chromium desktop is the configured browser; mobile
and other browser engines are not exercised here.

## Journeys covered (issue #90, live updates)

- A pending student has the home page open; the coordinator approves the registration through
  the real API; the page switches to the approved view **by itself**, without a reload (checked),
  and with never more than one gateway connection open.
- The test cuts the WebSocket (Playwright passes it through to the real gateway, then closes it
  and refuses reconnects). The "Live updates are paused" notice appears; the approval happens
  while disconnected; when the connection is allowed again the page catches up without a reload.
- The API needs Redis and the realtime settings from `.env.example` (`REALTIME_NAMESPACE`, …).

## Journeys covered (issue #91, live booking)

- A tutor and a student in **two separate browsers**. The student books; the request appears on
  the tutor's Teaching page by itself, with a "New: …" announcement. The tutor accepts; the
  student's My sessions card turns "Confirmed" with the meeting link. While the tutor is typing a
  decline reason, another student's request arrives and the typed text is kept; the first student
  then cancels and the request leaves the tutor's list with "now Cancelled". Neither page reloads.
- Not covered in the browser: **expiry**. Requests expire after at least an hour (the worker's
  real clock), longer than a test run. Expiry sends the same `booking.changed` event, which the
  component tests cover.

## Journeys covered (issue #92, live availability)

- A student has a tutor's profile open (it registers a real availability watch) while the tutor uses
  My availability in another browser. A one-off time the tutor adds appears for the student by
  itself. The student opens the booking form and types; another student books that time first:
  the form says "This time is no longer free" (never who took it), keeps the typed text and can't be
  sent, and the time leaves the list. When that booking is cancelled the time is free again. When
  the tutor removes it, it disappears for the student; the tutor's own "Removed." stays. No reloads.
  
## Journeys covered (issue #68, university sign-in)

`sso.spec.ts`:

- A crafted `/login?next=…&redirect_uri=…` link: the button calls only the fixed
  `/api/v1/auth/oidc/login`, without any query. With OIDC switched off (the default), the page says
  so and stays in the app. With `E2E_OIDC_ISSUER` set, the browser goes to that issuer's
  authorization page with `response_type=code`, S256 PKCE and the callback on this origin.
- Log in → open another page → log out: the page explains that the university account stays signed
  in, the old cookie gets a real 401 from `/auth/me`, and Back shows the login page, not the
  protected page.

Not covered: a complete Keycloak login. That needs an account connected to a synthetic Keycloak user
(trusted setup, see `frontend/README.md` section 10) and is covered on the API side by
`backend/tests/integration/test_oidc_keycloak.py`.

## Accessibility and mobile checks (issue #59)

Run them alone with `npm run test:a11y` (same setup as above).

- `accessibility.spec.ts` creates real data (a tutor with free time, a booking, a problem report, a
  pending tutor application and a pending registration), then opens **every screen** as the right
  user: logged out, student, tutor and coordinator, including detail pages. Each screen is checked at
  **1280px and 375px (phone)** with [axe](https://github.com/dequelabs/axe-core) against WCAG 2.1 A
  and AA, for sideways scrolling, and for exactly one `<h1>`. The report lists every problem at once
  (it is attached to the test result) and names any screen that had no data to open.
- `keyboard.spec.ts` logs in and books a session using **only Tab, typing and Enter**: the skip
  link comes first and is visible, every focused element shows a visible focus outline, focus moves
  into the booking form and then to the confirmation, and booking takes **three steps after the
  tutor profile opens** (NFR6).

What automated checks can't prove: axe finds roughly a third to a half of accessibility problems.
A short manual pass with a real screen reader (VoiceOver on macOS/iOS, NVDA on Windows) is still
worth doing before release.

### Tested browsers

| Browser engine | Sizes | Status |
|---|---|---|
| Chromium (Chrome, Edge) | 1280px desktop, 375px phone (emulated) | Automated in every run |
| Firefox | | Not automated (needs `npx playwright install firefox`) |
| WebKit (Safari, iPhone) | | Not automated (needs `npx playwright install webkit`) |

Failed runs keep a screenshot and trace in `test-results/` (`npx playwright show-trace <file>`).

## Validation handoff (2026-11-26)

- Task: extend real-backend coverage for implemented screens; branch `main`. Changes are tests,
  fixture helpers and this documentation only, with no backend business changes or migrations.
- Inspected: account/tutor/availability/booking pages and routes, existing browser helpers and
  journeys, scheduling HTTP/application code, session/history/reservation constraints, and
  AGENTS.md frontend §§14/17.
- Changed files: `session-lifecycle.spec.ts` (four new journeys), `availability.spec.ts` (release
  and deletion), `backend.ts` (unique visible names, optional onsite mode), `database.ts` and
  `history-fixture.ts` (controlled SQL setup/read assertions), `global-setup.ts` (shared SQL helper),
  `tutor-sessions.spec.ts` (coverage comment), and this README.
- Environment: actual Go API, fresh disposable PostgreSQL 17 and Redis, all 18 migrations,
  Vite and Playwright Chromium. Existing development services were left alone. Port 8080 was
  occupied, so this run used a temporary untracked Vite proxy configuration pointing at the
  disposable API's alternate loopback port; the application and test configuration were unchanged.
- Commands from `frontend/`: `npm run test:e2e -- --repeat-each=2 --trace on` (28/28 passed),
  `npm test` (101/101 passed), `npm run typecheck`, `npm run lint`, `npm run build` (all passed).
  After adding the unauthorized cancellation assertion, the four new journeys were also rerun
  twice standalone (8/8 passed); typecheck and lint passed again.
  Browser traces were saved outside the repository. Trace inspection found no page exceptions
  or received 5xx responses; browser console resource errors were expected 401/403/409/422 cases.
- Integration observation: the API logs some scheduling GET requests as `request failed` / 500
  around navigation/reloads (availability, own sessions, tutor requests). The recorded browser
  responses contain no 5xx and all workflows passed. Request cancellation is a possible cause,
  but these tests do not prove it. Follow up on cancellation/error logging separately; no backend
  error mapping or business rule was changed to silence it.
- Remaining scope: weekly-rule creation still needs a frontend control despite the semester API
  now existing; coordinator and tutor-application pages are placeholders. Consult this coverage
  inventory and `session-lifecycle.spec.ts` first when adding those journeys. No fake API is used
  by E2E; existing fake API unit tests remain intact. Browser plugin was unavailable, so the
  repository's Playwright runner was used. No claim of mobile, cross-engine or visual regression
  testing is made. Backend Go unit/race suites were not rerun because only frontend test files
  changed; the real running backend handled every E2E request. No commits, pushes, issue updates
  or deployment were performed.
