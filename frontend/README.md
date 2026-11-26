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
```

### A test account

Open http://localhost:5173/register and sign up with an address on a domain listed in
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
│   ├── auth/             # session state (useAuth), login/logout, sign-up, resubmission
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
| – | #52 / #53 | Report a problem, coordinator issue inbox | Anastasiia | ⚠️ API only on branch `feature/feedback_coordinator_operations` (not merged) |
| – | #35 | Remaining coordinator screens (monitoring, policy cancellation) | Anastasiia | ⚠️ same branch |
| – | #56 | Violation flags and suspension | Anastasiia | ❌ blocked on #25, #26, #48, #49 |
| last | #59 | Accessibility and mobile checks | both | – |
| later | #68 | Uni.lu SSO login | Nicolle | ❌ blocked on #46 (university contract) |

**SSO note:** the approved requirement is Uni.lu SSO only (FR1/FR23). The current email/password login is
temporary. All login code lives in `features/auth/` so #68 only changes that folder and the login page.

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

## 7. Status of #53 (problem report inbox)

| What it covers | Where |
|---|---|
| Faculty-scoped inbox of open and resolved reports, oldest first, 25 at a time | `features/issueInbox/IssueInbox.tsx` on `/coordinator/issues` (`?status=resolved`) |
| Report detail with description, topic (general or a session) and short references | `features/issueInbox/IssueReview.tsx` on `/coordinator/issues/:id` |
| One-time resolution with a required note (1–2000 UTF-8 bytes); the page says it changes no session, attendance or account | `IssueReview.tsx`, `rules.ts` |
| Already resolved (409) shows the stored resolution; another faculty's report (404) is explained | `IssueReview.tsx` |
| Tests | `features/issueInbox/IssueInbox.test.tsx` and `rules.test.ts`. They use the mocked API, so they are component tests, not integration evidence. |
| Known limit | The API gives only reporter and session IDs, so the screens show short references, not names or session details. |
