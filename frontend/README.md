# Tutti frontend

React + TypeScript web client for the Tutti peer-tutoring platform (Team 6, Software Engineering 2026).

**Owners:** Nicolle Blanco and Anastasiia Shkamerda (Frontend + UX/UI).
**Read this file before working on the frontend.** That goes for people and for AI assistants. It's the shared plan, so update it when ownership or conventions change.

---

## 1. Running it locally

You need Node.js 20.19+ or 22.13+ (CI uses 24) and the backend running (see the root `README.md` and `docs/identity.md`).

### Backend (once)

In the repository root `.env`:

```sh
PUBLIC_ORIGIN=http://localhost:5173          # must be the frontend's address, exactly
UNIVERSITY_EMAIL_DOMAINS=uni.lu,example.test # domains allowed to register (use test ones locally)
```

Then, from the repository root:

```sh
docker compose up -d --wait postgres redis
set -a; . ./.env; set +a
migrate -path backend/migrations -database "$DATABASE_URL" up
psql "$DATABASE_URL" -f backend/tests/integration/catalog_fixture.sql   # demo faculties/programs/subjects
(cd backend && go run ./cmd/api)
```

> **Why `PUBLIC_ORIGIN=http://localhost:5173`?** The API rejects every write (including login) whose
> `Origin` header doesn't exactly match `PUBLIC_ORIGIN`. In development the browser only talks to Vite on
> port 5173, and Vite forwards `/api` to the Go API on 8080. Always open **http://localhost:5173**, not
> `127.0.0.1:5173`. Otherwise login fails with 403.

### Frontend

```sh
cd frontend
npm install
npm run dev        # http://localhost:5173
npm test           # unit + component tests (mocked API, not real-backend evidence)
npm run lint       # code-quality checks (ESLint)
npm run typecheck
npm run build
npm run test:e2e   # browser journeys against the REAL backend; setup in tests/e2e/README.md
npm run test:a11y  # only the accessibility, mobile and keyboard journeys (#59)
```

### A test account

For ready-to-use student, tutor, coordinator and pending-student accounts
without SMTP, see [local demo accounts](../docs/local-demo.md).

In development builds you can also create a test account (release builds only offer the university
sign-in; see section 10). Open http://localhost:5173/register and sign up with an address on a domain listed in
`UNIVERSITY_EMAIL_DOMAINS` (for example `you@example.test`). With the catalog fixture loaded,
choose a "Demo" faculty and its program. You're logged in straight away; until a coordinator
approves the registration you'll see the "waiting for approval" notice, and the student pages
stay locked.

---

## 2. Folder structure

```text
src/
├── main.tsx              # entry point: AuthProvider + router
├── styles.css            # base styles and design tokens (:root variables)
├── app/                  # app-wide wiring
│   ├── routes.tsx        # every URL of the app, in one place
│   ├── guards.tsx        # RequireAuth, PublicOnly, RequireRole
│   └── AppLayout.tsx     # header, navigation, logout
├── pages/                # one component per screen (thin: composes features + shared UI)
├── features/<name>/      # everything for one feature: api.ts, types.ts, components, hooks
│   ├── auth/             # session state (useAuth), university sign-in (sso.ts), logout, resubmission;
│   │                     #   dev-only test-account login/sign-up (devLogin.ts)
│   └── catalog/          # public faculty/program lists, ProgramPicker (import from catalog/index.ts)
├── shared/
│   ├── api/client.ts     # the ONLY place that calls fetch
│   ├── api/errors.ts     # errorMessage(): API error → sentence for the user
│   ├── hooks/            # useFocusOnFailure (move focus to the problem after a failed submit)
│   └── ui/               # Button, TextField, SelectField, Alert, Spinner, PageLoader
└── test/                 # unit-test setup and the fetch mock (mockApi, a TEST DOUBLE)
tests/e2e/                # Playwright browser journeys against the real backend (no mocks)
```

Suggested feature folders as the issues land: `features/tutoring`, `features/scheduling`,
`features/feedback`, `features/coordination`.

---

## 3. Conventions

**Talking to the API**
- Always use `api.get/post/patch/delete` from `shared/api/client.ts`. Paths are relative to `/api/v1`
  (`api.get('/tutors', { query: { subject_id } })`).
- Put a feature's calls in `features/<name>/api.ts` and its response types in `features/<name>/types.ts`.
  Copy the types from the backend handler's JSON tags (`backend/internal/<module>/interfaces/http/`).
- Lists come back as `Page<T>` = `{ data, pagination: { limit, offset, has_more } }`.
- Show errors with `errorMessage(error, { 409: 'This slot was just taken.' })`. Give screen-specific overrides
  for the statuses that matter on that screen.
- A 401 anywhere logs the user out automatically. You don't need to handle it per page.

**Live updates (realtime, #90)**
- The app keeps **one** WebSocket to `/ws` while someone is logged in (`app/RealtimeProvider.tsx`,
  `shared/realtime/`). Messages only say *that* something changed (`booking.changed`,
  `availability.changed`, …); they carry no data. Always refetch over HTTP.
- In a screen, use `useLiveRefresh(['booking.changed'], reload)` from `shared/realtime`. It calls
  `reload` (debounced) when such an event arrives, and after every reconnection, because events
  sent while disconnected are not replayed. Pass `{ entityId }` to react to one tutor or session only.
- Public views of a tutor (profile, search results) must also call `useAvailabilityWatch(tutorIds)`,
  or the server won't send that tutor's `availability.changed` to this user (max. 32 tutors; the
  hook registers, renews every 5 minutes and re-registers after reconnecting).
- Lists built on `usePagedList` get a `refresh()` for live updates (#93): `useLiveRefresh([...], refresh)`
  refetches as many items as are shown (so "Show more" results stay) and swaps them in place. Render
  `<LiveAnnouncement change={change} noun={['report', 'reports']} />` so screen readers hear what changed.
  Detail pages refetch in the background and only replace the data, so forms and typed text stay; a
  403/404 (`noLongerVisible(error)`) drops the record instead of keeping private data on screen.
- Never open your own `WebSocket`, never send messages on it, and never show event fields as data.
  The contract is in `docs/realtime.md` and `docs/realtime-delivery.md`.
- `<html data-live="…">` mirrors the connection state (`open`, `reconnecting`, …). Only the browser
  journeys use it (#94), to wait until a page is listening; don't style or script against it.
- In development Vite forwards `/ws` to the API (see `vite.config.ts`); the API checks the Origin
  exactly like for HTTP writes.

**Permissions and privacy**
- `RequireRole` only hides screens. The server checks every permission, so never rely on the UI for security,
  and never send user IDs, roles or faculty as "who I am" in a request body.
- Never display a tutor's grade, student number or private feedback comments except on the coordinator
  screens the API allows (see `AGENTS.md` §6 and §9). Students see an "approved" badge and aggregate ratings.

**Accessibility and mobile (NFR7, #59)**
- Use `TextField` (it links labels, hints and errors) and `Button` (it handles loading state).
- Errors use `Alert` (role="alert"). Every page has exactly one `<h1>`.
- Check every screen at phone width (375px) and with keyboard only.
- Booking must take at most 3 steps after opening a tutor profile (NFR6).

**Adding a screen**
1. Create `src/pages/MyPage.tsx` (and feature code under `src/features/...`).
2. In `src/app/routes.tsx`, replace the matching `ComingSoonPage` (or add a route inside the right guard).
3. Add a test next to it using `mockApi` (see `src/app/routes.test.tsx` for examples).

**Git**
- Agree with the team on branching. A feature branch and PR per issue (as the backend modules did) keeps
  Nicolle's and Anastasiia's work from colliding. Reference the issue in the PR (`Closes #29`), and don't touch backend files, migrations, `compose.yaml` or CI without telling the backend owners.

---

## 4. Issue plan and ownership

Milestone **M7 – Frontend MVP**. Ownership below is a proposal. Confirm it, then assign yourselves on GitHub.

| Order | Issue | Screen | Owner | Backend ready? |
|---|---|---|---|---|
| 1 | #27 | Routing, API client, auth state, shared UI | Nicolle | ✅ (this foundation) |
| 2 | #28 | Register, login, verification status, resubmit | Nicolle | ✅ |
| 3 | #29 | Tutor search and tutor profile | Nicolle | ✅ |
| 4 | #32 | Book a session, my sessions, cancel | Nicolle | ✅ |
| 5 | #31 | Tutor availability management | Nicolle | ✅ |
| 6 | #33 | Tutor accept / decline / complete | Nicolle | ✅ |
| – | #30 | Tutor application (student + coordinator review) | Anastasiia | ✅ |
| – | #34 / #55 | Feedback form, tutor activity dashboard (overlap: track as one) | Anastasiia | ✅ |
| – | #51 | Coordinator verification queue | Anastasiia | ✅ |
| – | #54 | Coordinator no-show adjudication | Anastasiia | ✅ |
| – | #52 / #53 | Report a problem, coordinator issue inbox | Anastasiia | ✅ (merged to `main`) |
| – | #35 | Remaining coordinator screens (monitoring, policy cancellation) | Anastasiia | ✅ (merged to `main`) |
| – | #56 | Violation flags and suspension | Anastasiia | ✅ (#25, #26, #48, #49 merged to `main`) |
| last | #59 | Accessibility and mobile checks | both | – |
| later | #68 | Uni.lu SSO login | Nicolle | 🟡 frontend done (see section 10); backend failure redirect and Uni.lu approval open |

**SSO note:** the approved requirement is Uni.lu SSO only (FR1/FR23). Release builds now show only the
"Sign in with University of Luxembourg" button (#68). The email/password form for local test accounts
stays in development builds until #66/#67 move accounts to SSO and retire passwords (section 10).

---

## 5. Status of #27 (foundation)

| Acceptance criterion | Where |
|---|---|
| Routing separates public and authenticated areas | `app/routes.tsx`, `app/guards.tsx` |
| API client preserves session cookies and handles same-origin writes | `shared/api/client.ts` |
| Authentication state is restored or refreshed consistently | `features/auth/AuthProvider.tsx` (on load, on tab focus, on any 401) |
| Shared loading, error and form components exist | `shared/ui/` |
| Foundation smoke tests pass | `npm test` |

**CI:** `.github/workflows/frontend.yml` runs lint, typecheck, tests and build on every push or PR that
changes `frontend/` (on `main` and `features/**` branches). It is separate from Stefan's `backend.yml` and
doesn't change it; Stefan owns CI, so agree changes to it with him.


---

## 6. Status of #52 (report a problem)

| Acceptance criterion | Where |
|---|---|
| Required description and optional authorized session reference | `features/issueReports/ReportForm.tsx` on `/report`; a session card in My sessions opens `/report?session=<id>`. The form only links a session the API returns for this user, and `POST /issues` checks it again. |
| Not conflated with no-show allegations | Separate page that only calls `POST /issues`; it says it is not a no-show report and doesn't change session outcomes. |
| Validation and submission feedback without claiming resolution | Description checked before sending (1–2000 UTF-8 bytes); server errors (403, 404, 422) shown in the form with the text kept; the confirmation says a coordinator still has to review the report. |
| Tests for linked/unlinked report, access denial and errors | `features/issueReports/ReportProblem.test.tsx` and `rules.test.ts`. They use the mocked API, so they are component tests, not integration evidence. |

---

## 7. Status of #53 (problem report inbox)

| What it covers | Where |
|---|---|
| Faculty-scoped inbox of open and resolved reports, oldest first, 25 at a time | `features/issueInbox/IssueInbox.tsx` on `/coordinator/issues` (`?status=resolved`) |
| Report detail with description, topic (general or a session) and short references | `features/issueInbox/IssueReview.tsx` on `/coordinator/issues/:id` |
| One-time resolution with a required note (1–2000 UTF-8 bytes); the page says it changes no session, attendance or account | `IssueReview.tsx`, `rules.ts` |
| Already resolved (409) shows the stored resolution; another faculty's report (404) is explained | `IssueReview.tsx` |
| Tests | `features/issueInbox/IssueInbox.test.tsx` and `rules.test.ts`. They use the mocked API, so they are component tests, not integration evidence. |
| Known limit | The API gives only reporter and session IDs, so the screens show short references, not names or session details. |

---

## 8. Status of #54 (no-show adjudication)

| What it covers | Where |
|---|---|
| Faculty-scoped list of no-show reports waiting for a decision, confirmed or rejected, oldest first, 25 at a time | `features/noShowReports/NoShowList.tsx` on `/coordinator/no-show-reports` (`?status=confirmed`, `?status=rejected`) |
| Report detail: session, reporter and absent participant (short references), time and the reporter's evidence, shown as an allegation | `features/noShowReports/NoShowReview.tsx` on `/coordinator/no-show-reports/:id` |
| Confirm or reject with a required note (1–2000 UTF-8 bytes); confirming asks a second time and explains the violation, the session correction and flags | `NoShowReview.tsx`, `rules.ts` |
| Kept apart from general problem reports; opposing reports about one session stay separate entries | Page texts; separate API (`/coordinator/attendance-reports`) |
| Already decided or no longer allowed (409) shows the current state; another faculty's report (404) is explained | `NoShowReview.tsx` |
| Tests | `features/noShowReports/NoShowReports.test.tsx` and `rules.test.ts`. They use the mocked API, so they are component tests, not integration evidence. |
| Known limit | The API gives only user and session IDs, so the screens show short references, not names or session details. |

---

## 9. Status of #56 (flags and suspension)

| What it covers | Where |
|---|---|
| Flags stored for the coordinator's faculty, newest first: violation count, semester and current suspension state; refreshes live on `coordination.changed` | `features/accountDiscipline/FlaggedList.tsx` on `/coordinator/flags` |
| Account page: latest suspension decision by this faculty, suspend (asks a second time) or reinstate with a required private reason (1–2000 UTF-8 bytes); the page explains the effects | `features/accountDiscipline/AccountReview.tsx` on `/coordinator/users/:id` |
| Account history by category (suspension decisions, violations, flags), only events recorded for this faculty | `features/accountDiscipline/AccountHistory.tsx` |
| Only the current home faculty may decide (404 explained); a state that changed meanwhile (409) reloads the page | `AccountReview.tsx`, `rules.ts` |
| Tests | `features/accountDiscipline/Discipline.test.tsx` and `rules.test.ts`. They use the mocked API, so they are component tests, not integration evidence; a real-backend journey is still to be added. |
| Known limits | The API gives only IDs, so accounts, sessions and semesters show as short references. There is no account search: an account is reached from its flag. The current state shown is the latest decision recorded by this faculty. Since #93 the account page refreshes live without losing a reason being typed (unless the account's state flips). |

---

## 10. Status of #68 (sign in with the University of Luxembourg account)

| Acceptance criterion | How it's met | Where |
|---|---|---|
| No university password input or local login fallback | Release builds show only the university button. `/register` opens the login page, and the header has no "Register" link. The test-account form is wrapped in `import.meta.env.DEV`, so `vite build` leaves it out of the bundle. | `pages/LoginPage.tsx`, `features/auth/devLogin.ts`, `pages/RegisterPage.tsx`, `app/AppLayout.tsx` |
| The button goes through the trusted backend; no provider URL from query parameters | The browser goes only to the fixed `GET /api/v1/auth/oidc/login`. The API builds the provider URL (state, nonce, PKCE). React never reads or forwards query parameters. | `features/auth/sso.ts` |
| Safe return after login, no open redirects | The API always lands on `OIDC_POST_LOGIN_PATH` (`/`). The page the user wanted is kept for this tab only (sessionStorage, 10 minutes, an app path checked by `safePath()`, never a token). It is opened once, replacing the landing page in the history. | `features/auth/sso.ts`, `features/auth/redirect.ts`, `app/guards.tsx` |
| Pending, rejected, suspended and SSO-unavailable states | Before leaving, the page asks the API whether sign-in can start (redirects aren't followed). 404 means "switched off here", 429 "too many attempts" and 503 or a network failure "not possible right now"; focus moves to the message. Coming back without a session shows "didn't finish" with the likely reasons (account not connected, or suspended). Pending and rejected students see their status on the home page (#28). A suspension found during use shows "This account can't be used right now". | `pages/LoginPage.tsx`, `pages/HomePage.tsx`, `features/auth/AuthProvider.tsx` |
| Coordinator onboarding | The login page explains that coordinator tools come from Tutti's faculty records, not from the university account, and who to ask if they're missing. Accounts are only connected through the backend's trusted setup; there is no linking screen (#66). | `pages/LoginPage.tsx` |
| Logout destroys the Tutti session; explain university single logout | `POST /auth/logout` revokes the Tutti session. The login page then says the university account is **not** signed out. The API doesn't call the provider's logout, so no single logout happens (docs/identity.md, Stage 3B), and the page advises closing the browser on shared computers. | `features/auth/AuthProvider.tsx`, `pages/LoginPage.tsx` |
| Mobile, keyboard, browser history, route and accessibility tests | The button is a real `<button>` (keyboard, 44px, full width). Back from the university page resets the page (back/forward cache) and re-checks the session. Back after logout doesn't reopen protected pages. `/login` is in the axe check at 1280 and 375px, and the keyboard journey tabs past the button. | `pages/LoginPage.test.tsx`, `features/auth/sso.test.ts`, `features/auth/redirect.test.ts`, `tests/e2e/sso.spec.ts`, `tests/e2e/accessibility.spec.ts`, `tests/e2e/keyboard.spec.ts` |

**Development test accounts.** `npm run dev` still shows a "Development test accounts" form under the
button. The demo without Keycloak (`docs/local-demo.md`) and every Playwright journey log in this way.
To see exactly what a release build shows, start Vite with `VITE_DEV_PASSWORD_LOGIN=false npm run dev`.
To check that a release bundle contains no password form, run
`npm run build && grep -l "current-password" dist/assets/*.js`, which should print nothing.

**Try the real sign-in locally** (synthetic Keycloak, docs/sso-demo.md):

1. Start Keycloak as in `docs/sso-demo.md`. In its admin console (realm `tutti-demo` → Clients →
   `tutti-backend`), add `http://localhost:5173/api/v1/auth/oidc/callback` to *Valid redirect URIs*
   and `http://localhost:5173` to *Web origins*. The committed realm only allows port 8080, but in
   development the browser reaches the API through Vite on 5173.
2. Start the API with `PUBLIC_ORIGIN=http://localhost:5173`, `OIDC_ENABLED=true`,
   `OIDC_ISSUER_URL=http://localhost:8180/realms/tutti-demo`, `OIDC_CLIENT_ID=tutti-backend`,
   `OIDC_REDIRECT_URL=http://localhost:5173/api/v1/auth/oidc/callback`, `OIDC_POST_LOGIN_PATH=/`
   and `OIDC_CLIENT_SECRET` (the same value as Keycloak's `TUTTI_OIDC_CLIENT_SECRET`).
3. Connect a synthetic Keycloak user to an existing local test account. There's no screen or API for
   this (#66). On a disposable local database only, insert the row that trusted setup would write:
   `INSERT INTO oidc_identities (user_id, issuer, subject) VALUES ('<Tutti user id>', 'http://localhost:8180/realms/tutti-demo', '<Keycloak user ID>');`
   The Keycloak user ID is shown on the user's page in the admin console.
4. Open http://localhost:5173/login and press the button. The e2e journey checks the redirect when run
   with `E2E_OIDC_ISSUER=http://localhost:8180/realms/tutti-demo`.

**Still open (don't close #68 on this summary alone):**

- **Backend, failed sign-ins:** when the callback fails (unknown or suspended account, expired attempt),
  the API answers with a JSON error page instead of returning to the app. Ask the backend owner to
  redirect those failures to a fixed path such as `/login`. The page then shows "didn't finish"
  automatically, with no frontend change. Until then, users see the API's error text and must come
  back to Tutti themselves.
- **Checking whether sign-in can start** creates one extra short-lived attempt on the API and counts
  towards its limit of 10 attempts per minute per network address (docs/identity.md, "Initiation
  resource limits").
- **Not yet verified:** a complete sign-in through the React app against Keycloak with a connected
  account (steps above), and the e2e journeys in `tests/e2e/sso.spec.ts` against the running API.
  Uni.lu approval of the provider is still pending (#46/#70), so this is the synthetic demo provider
  only.

---

## 11. Status of #93 (live coordinator, applicant and tutor views)

| What changes live | Event | Where |
|---|---|---|
| Coordinator application queue and review; the applicant's applications (decision, form offered again) | `tutor_application.changed` | `features/tutorApplications/` (`ApplicationQueue`, `ApplicationReview`, `StudentApplications`) |
| Registration queue and review, problem-report inbox and review, no-show queue and review, flagged accounts, account page and its history | `coordination.changed` | `features/verifications/`, `features/issueInbox/`, `features/noShowReports/`, `features/accountDiscipline/` |
| Faculty totals, tutor directory, tutor page (totals, subjects, week, feedback) | `activity.changed`, `tutor_application.changed`, `coordination.changed`, `booking.changed` (week) | `features/coordinatorMonitoring/` |
| The tutor's own totals, upcoming sessions and received feedback | `activity.changed` (own tutor ID), `booking.changed` | `features/feedback/ActivityOverview.tsx` |
| Roles, verification, program, faculty or suspension of the logged-in account | `account.access_changed` → `GET /auth/me` | `app/RealtimeProvider.tsx`, `app/guards.tsx` |

**How it behaves**
- Lists refresh in place and keep their filter (in the URL or the select) and every "Show more" page.
  A polite status line says what changed ("Updated: 1 new report.").
- Detail pages keep a half-written reason or note while the case is still open. If it was decided
  elsewhere (another coordinator or another tab), an info notice says so and the form disappears.
- If a refetch answers 403 or 404 (e.g. the case moved to another faculty), the page drops the
  record and shows the usual "not found" message instead of keeping private data on screen.
- When the account's access changes, protected pages load again from scratch (`<Outlet key>` in
  `RequireAuth`). A suspension ends the session ("This account can't be used right now"); a removed
  role shows "You don't have access to this page"; a new faculty never shows the old faculty's lists.
- Coordinator events carry a case ID, not a faculty. Lists therefore refetch on any event of their
  family, and detail pages only on their own ID (activity events: the tutor's ID).

**Tests:** `pages/LiveCoordination.test.tsx` covers each event family and the access changes
(suspension, removed role, faculty change) with a fake WebSocket and a mocked API. These are component
tests, not integration evidence: real-backend browser journeys for these screens are #94.

---

## 12. Status of #94 (live updates across browsers, tested against the real backend)

| Acceptance criterion | Where it is checked |
|---|---|
| A change made in one browser appears in another without navigation or reload | Every journey in `tests/e2e/live-*.spec.ts` marks the page and checks it was never reloaded |
| Offline/reconnect recovers missed changes and keeps no forbidden state | `live-sessions.spec.ts` (lost connection: the missed request appears once) and `live-access.spec.ts` (suspension while offline, two tabs, expired session) |
| Unauthorized users or faculties never receive or render restricted changes | `live-coordination.spec.ts` (second faculty: no render, no received frame) and `live-sessions.spec.ts` (unrelated student: no frame) |
| Reproducible suite in CI, linked to #36 | `.github/workflows/e2e.yml` runs all browser journeys with PostgreSQL, Redis, the API and the worker |

The journeys and the CI job are described in `tests/e2e/README.md` ("Journeys covered (issue #94)"
and "Continuous integration"). The only app change is the `data-live` attribute that lets the tests
wait for an open connection instead of sleeping.

**Not yet verified:** these journeys and the CI job were written without a running backend. Their
first real run is the GitHub Actions run of the commit that adds them; until that run is green,
#94 isn't done.
