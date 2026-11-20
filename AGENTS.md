# AGENTS.md — Tutti engineering agent guide

> **Scope:** Entire repository, unless a more specific `AGENTS.md` in a subdirectory overrides part of this guide.
>
> **Project:** Tutti — Peer-to-Peer Tutoring Platform, Team 6, Software Engineering 2026.
>
> **Repository:** `StefanPenchev05/course-code-2026-team-06`  
> **Project board:** <https://github.com/users/StefanPenchev05/projects/11>
>
> **Important:** This guide describes the repository inspected on `main` on 2026-11-19. It is not a substitute for inspecting the actual working tree. **Current code, applied migrations, and tests take precedence over assumptions in this file.**

## 1. Mission and working agreement

Build a secure, maintainable university tutoring platform connecting verified students with approved student tutors. Faculty coordinators approve students and tutors, oversee tutoring, resolve attendance issues, and perform authorized administrative actions.

For **every** task, your responsibility is to:

1. Understand the requested behavior and the existing implementation **before editing code**.
2. Make the smallest complete, coherent change in the **correct owning module**.
3. Preserve security, faculty isolation, transactional invariants, and historical records.
4. Add or adapt tests that prove the changed behavior, including negative and concurrency cases where relevant.
5. Run the appropriate checks and report **what ran, what passed, and what could not run**.
6. Explain changes and unresolved integration points to the next agent or human contributor.

Do not start by generating new architecture, new services, or duplicate repositories. First locate the implementation that already handles the responsibility.

### Git and collaboration safety

- Inspect `git status --short --branch` **before** editing. Preserve unrelated local changes; other humans/agents may have work in progress.
- The team has recently worked directly on `main`. **Do not create/switch branches automatically** unless the current task explicitly asks you to. Obey any task-specific branch instructions.
- **Do not commit, push, merge, reset, force-push, delete branches, or discard uncommitted changes without explicit instructions.**
- Do not overwrite another agent's changes. If several agents work concurrently, agree on ownership of shared files (`bootstrap`, migrations, config, `compose.yaml`, CI) and publish interface contracts before editing them.
- GitHub Issues and Project #11 track scope and progress. Read the issue and acceptance criteria when a task is issue-driven. Do not close issues or mark them Done without implementation/test evidence.
- If instructions disagree, follow the current user/task instructions, then applicable security/data requirements; explicitly surface unresolved contradictions rather than silently selecting a new business policy.

## 2. Mandatory pre-work scan (do this before every coding task)

Run/inspect from the repository root (adapt when tools are unavailable):

```bash
pwd
git status --short --branch
git log -5 --oneline
find . -name AGENTS.md -print
find backend/internal -maxdepth 3 -type f | sort
find backend/migrations -maxdepth 1 -type f | sort
```

Then follow this checklist:

1. **Identify the task:** the relevant GitHub issue, requirement IDs, use case, user-visible behavior, authorization scope, and acceptance criteria. Check for existing or partially implemented functionality.
2. **Read the foundations:** `README.md`, this file, and the relevant documents in `docs/`. Inspect `.env.example` for configuration-dependent work.
3. **Read before modifying:** the module's `domain/`, `application/`, `infrastructure/`, `interfaces/http/`, its tests, and the bootstrap wiring. Search for existing ports, helpers, and transaction patterns.
4. **Inspect real schema:** read the relevant SQL in `backend/migrations/`, existing indexes, triggers, foreign keys, and rollback scripts. Do **not** implement from a DBML drawing alone.
5. **Trace the request end to end:** HTTP route → authentication middleware → application service → domain decision → transaction/repository → database / outbox / worker. Identify any cross-module contracts.
6. **Check failure cases:** invalid role/faculty, suspended or unverified account, missing/duplicate record, concurrent requests, stale status, failed audit/outbox write, privacy leakage.
7. **Check impact:** callers, existing endpoint shape, migrations, worker behavior, integration tests, configuration, and documentation. Choose a bounded implementation plan.
8. **Only then edit code.** If an essential source is absent, say what was inspected and what remains unknown; do not pretend a proposed file or feature already exists.

**Search tips:** Prefer `rg 'symbol|endpoint|table' backend docs` and `rg --files backend` when `rg` is available. Read the concrete call sites rather than relying on filenames alone.

## 3. Source-of-truth hierarchy

Use sources in this order when making an implementation decision:

1. **Actual checked-out code, current PostgreSQL migrations, constraints, tests, and implemented API contracts.**
2. **Approved business rules** documented in `docs/database/tutti-schema-v0.3.md`, `docs/1.3-1.4_user-story_mapping--requirement_analysis.md`, and the relevant module docs.
3. **Current issue / acceptance criteria** and explicit instructions from the human requester.
4. `docs/database/tutti-schema-v0.2.dbml`, design diagrams, original requirements and older examples **for context**, not as overriding runtime truth.

The original Deliverable 1 is available in `docs/Deliverable_1.pdf`, with corresponding requirements and UML documents in `docs/`. Some rules were refined after Deliverable 1; use the later approved docs and working migrations to resolve known differences. If two authoritative sources still conflict, flag the discrepancy **before** changing business behavior.

## 4. Actual repository map

```text
course-code-2026-team-06/
├── AGENTS.md                         # This guide; place at repository root
├── README.md                         # Project overview, team, links
├── .env.example                      # Configuration names; never put secrets in Git
├── compose.yaml                      # PostgreSQL, Redis, optional worker profile
├── .github/workflows/backend.yml     # Go + database CI and backend packaging
├── backend/
│   ├── go.mod                         # Go 1.24 module
│   ├── cmd/
│   │   ├── api/main.go               # API executable entry point
│   │   └── worker/main.go            # Background worker entry point
│   ├── internal/
│   │   ├── bootstrap/                # DI/composition, shared DB setup, worker wiring
│   │   ├── identity/                 # Accounts, verification, authentication, sessions
│   │   ├── catalog/                  # Faculties, programs, subjects, membership
│   │   ├── tutoring/                 # Tutor applications, approval, offerings, discovery
│   │   ├── scheduling/               # Availability, bookings, completion, expiration
│   │   ├── coordination/             # Attendance adjudication, violations/flags
│   │   ├── feedback/                 # Ratings, tutor statistics, feedback access
│   │   ├── notifications/            # Delivery worker application + PostgreSQL/SMTP
│   │   └── platform/worker/          # Shared polling/worker mechanics
│   ├── migrations/                   # Numbered .up.sql / .down.sql pairs (currently 000001–000010)
│   └── tests/integration/            # Go integration tests, SQL fixture, shell/Python runners
└── docs/
    ├── database/implementation.md   # DB setup, migration and transaction contracts
    ├── database/tutti-schema-v0.3.md # Approved schema/business-rule design
    ├── identity.md                   # Auth, cookies, verification, activation, API
    ├── catalog.md                    # Catalog endpoints, shared subject membership
    ├── tutoring.md                   # Tutor applications, offerings, discovery
    ├── scheduling.md                 # Recurrence, booking, state transitions
    ├── coordination.md               # Attendance, coordinator review, violations
    ├── feedback.md                   # Ratings and derived activity data
    ├── workers.md                    # Expiration worker, SMTP retries, leases
    └── [requirements / UML documents]# Stakeholders, user stories, use cases, models
```

**Current vs planned:** The project README names **React + TypeScript** for the frontend and **C** as part of the intended stack, but the inspected `main` branch has **no `frontend/` or C implementation directory**. Do not claim either exists or generate a large scaffold unless the task specifically requests it. When implementing the frontend, first inspect the actual repository again; the architecture proposal favors `frontend/src/app`, `pages`, `features`, and `shared`.

## 5. Runtime and coding architecture

### Stack and processes

- **Go 1.24** backend, standard `net/http` router/mux, `pgx/v5` for PostgreSQL, `go-redis/v9` for sessions, `golang.org/x/crypto` for password hashing.
- **PostgreSQL 17** is the durable source of truth for accounts, catalog, tutor qualifications, availability, sessions, violations, feedback, history, and notification outbox.
- **Redis** stores opaque authenticated session state; it must not own durable business data.
- **API process** (`backend/cmd/api`): HTTP routing, authentication, application use cases and persistence.
- **Worker process** (`backend/cmd/worker`): polls pending session expiry and queued email delivery. It can run separately from the API. Notifications can be queued when SMTP/worker is unavailable.
- **Docker Compose** starts development PostgreSQL and Redis; the worker service is under the `workers` profile.
- **GitHub Actions** runs `backend.yml` checks; the pipeline packages API, worker, and migrations after successful `main` checks, but **packaging is not deployment**.

### Within a business module

Most business modules use:

```text
internal/<module>/
├── domain/            # Entities, value objects, invariants, domain errors
├── application/       # Use cases, interfaces/ports, authorization/workflow decisions
├── infrastructure/    # PostgreSQL/Redis/SMTP adapters, concrete persistence
└── interfaces/http/   # Route registration, handlers, DTOs, HTTP error mapping
```

Some modules have fewer layers/files where unnecessary. Follow the **existing** organization rather than creating placeholders merely to fit a template.

Request flow:

```text
HTTP request
  → existing Identity auth / same-origin middleware
  → module HTTP handler
  → module application service (authorization + transaction orchestration)
  → domain checks and narrow ports
  → PostgreSQL/Redis/SMTP adapters
  → commit + safe HTTP response
```

`backend/internal/bootstrap/identity.go`, `shared.go`, and `worker.go` own concrete wiring. Edit bootstrap only to connect real new capabilities; do not put domain rules there.

### Module ownership and dependencies

| Module | Owns | Collaborates with / important boundary |
| --- | --- | --- |
| **Identity** | Users, student verification, coordinator membership, Argon2id, Redis sessions | Provides trusted principal and faculty/role guards; other modules must not implement a second role system. |
| **Catalog** | `faculties`, `programs`, `subjects`, `program_subjects` | Exposes `SubjectBelongsToProgram`, `GetProgramFaculty`; shared subject IDs across faculties. |
| **Tutoring** | Tutor applications, qualifications, profiles, approved offerings | Uses Identity and Catalog; Scheduling reads eligibility through published application/transaction-aware interfaces. |
| **Scheduling** | Recurring rules, exceptions, slots, session state and reservations | Uses Identity, Catalog, Tutoring; completion and expiry belong here, not in HTTP handlers. |
| **Coordination** | Attendance report decisions, confirmed violations, semester flags | Uses session/Identity locks and tutor-faculty authority; general issues and suspension workflows remain additional work. |
| **Feedback** | One-time session feedback and derived activity/rating queries | Uses current Scheduling session status; not a separate mutable counter system. |
| **Notifications** | Queued email delivery, claims, leases, retries and SMTP transport | Business modules enqueue notifications transactionally; worker delivers them. |

**Critical dependency rule:** Cross-module reads/writes happen via **published application interfaces or explicitly injected transaction-bound adapters**. Do not import another module's concrete `infrastructure/postgres` repository into business logic, bypass established locking, or start an unrelated pool query during a transaction that relies on the same row locks. The bootstrap layer may connect concrete adapters.

### Data and transaction rules

- Prefer small, explicit use-case services; keep pure calculations (time ranges, expiration, transitions, eligibility semantics) testable separately.
- Authorization decisions occur on the server; never trust user ID, role, faculty, or tutor ownership from client JSON.
- Multi-row changes must be atomic. For status changes, preserve `*_status_history` and notification outbox consistency with the main write.
- For database changes, add **new, reversible, sequential migrations**; do not edit already-applied migrations. Inspect the latest number before choosing the next.
- Keep SQL parameterized and context-aware; handle database constraint violations intentionally. Never return raw SQL/stack traces to clients.
- Reuse existing PostgreSQL triggers and GiST exclusion constraints for booking reservations. Do not invent a second reservation table or double-write the trigger-managed participant blocks.
- Preserve audit/qualification/disciplinary history. Delete data only when the approved workflow and retention policy allow it; do not use cascade deletion as a shortcut.
- Use UTC or offset-aware timestamps for absolute instants, local calendar dates/times plus IANA zones for recurrence, and deterministic clocks in tests.

## 6. Business invariants that agents must not break

These are intentionally brief. **Read the relevant detailed guide and migrations before changing any rule.**

### Identity and academics

- Coordinator accounts are university-provisioned. Students register with an allowed university email and student number; faculty coordinators must approve verification before tutoring privileges are granted.
- A tutor is **also** a student; one user may book sessions and teach other subjects. Grades are private and visible only in authorized coordinator workflows.
- A tutor application contains **one subject** and a decimal grade in **15.00–20.00**. An approved offering is subject-specific; one tutor profile can hold many offerings.
- One canonical subject ID can belong to multiple programs and faculties. Cross-faculty tutoring is valid **only when both participants' programs contain the subject** and the tutor has an active approved offering.
- Changes to a student's academic program require tutor-offering eligibility revalidation; do not silently reactivate inactive offerings or delete accepted sessions.

### Availability and bookings

- Tutor availability is a weekly semester-bounded recurrence plus concrete slots, exceptions and one-off windows. The local weekday/time must be resolved in its IANA time zone; ambiguous/nonexistent local clock times are rejected.
- `REQUESTED` and `ACCEPTED` sessions reserve **both participants** through PostgreSQL triggers. A person cannot hold overlapping sessions in student and tutor roles; half-open intervals `[start,end)` permit adjacent sessions.
- Never remove or mutate an availability slot in a way that invalidates an active session. Student-facing free intervals are a read projection; final availability/eligibility **must be rechecked in the booking transaction**.
- Minimum booking notice: **3 hours**. Introduction: **30 minutes**. Other purposes: **60/90/120 minutes**.
- Request expiry: for **3–25 hours** of notice, `min(request + 1h, start - 2h)`; for **more than 25 hours**, `min(request + 48h, start - 24h)`. Reuse the domain function and database constraint; do not reimplement different formulas.
- Tutor accepts/declines a still-pending request; students or tutors may cancel with a reason. Less than 24 hours before start is a late participant cancellation. Expiration worker must not override an already-decided session.

### Completion, violations and feedback

- Tutor explicitly marks an accepted session `COMPLETED` **after** its scheduled end; completion is not automatic.
- Either participant may report the other's no-show from the session end through **168 elapsed hours** afterward. A report is an allegation, not a violation; tutor-faculty coordinator adjudicates.
- A confirmed no-show can correct `COMPLETED` to `NO_SHOW`; preserve prior history. Confirmed no-shows and late cancellations count in the **session semester**; at two combined violations, flag the person's **home faculty**. **Never automatically suspend.**
- Tutor's faculty coordinates the session/attendance issue; user's home-faculty coordinator handles account suspension/reinstatement.
- Only the session's verified student may submit one rating (integer **1–5**) for a currently `COMPLETED` session. Comments are private. Ratings, counts, and teaching hours are **derived from sessions currently `COMPLETED`**, so later no-show corrections automatically exclude invalidated sessions while retaining historical feedback.
- Email outbox records queueing, not guaranteed inbox delivery. SMTP sends have **at-least-once** semantics with recoverable leases and possible duplicate actual deliveries. Do not log activation tokens, secrets, personal email bodies, or private grades.

## 7. What to scan for your specific task

| If working on… | Read these first | Test/impact to consider |
| --- | --- | --- |
| **Authentication / roles / students** | `docs/identity.md`, `internal/identity`, `bootstrap/identity.go`, migrations `000002` | Redis expiry, verified/suspended access, same-origin writes, coordinator faculty, password/activation secrecy. |
| **Academic programs / subjects** | `docs/catalog.md`, `internal/catalog`, migrations `000001`, Identity registration call sites | Shared subject IDs, cross-faculty membership, registration transaction locks. |
| **Tutor applications / discovery** | `docs/tutoring.md`, `internal/tutoring`, `internal/catalog`, migrations `000003` | One-subject qualification, grade privacy, concurrent approvals, offering revalidation and cross-faculty discovery. |
| **Recurrence / free intervals** | `docs/scheduling.md` Stage 1, `internal/scheduling`, migration `000004` | Local time/DST, semesters, exceptions, slot deletion vs active bookings, concurrent overlap. |
| **Bookings / cancellation / expiry** | `docs/scheduling.md` Stage 2, `docs/database/implementation.md`, migrations `000005`–`000007`, `bootstrap/shared.go` | Two participant blocks, same-transaction eligibility, race with acceptance/expiration/deletion, audit and outbox atomicity. |
| **Attendance / violations / coordinator cases** | `docs/coordination.md`, `internal/coordination`, `internal/scheduling`, migrations `000006`, `000010` | Seven-day reporting, faculty authority, no-show corrections, semester threshold/idempotency, history. |
| **Feedback / activity / ratings** | `docs/feedback.md`, `internal/feedback`, `internal/tutoring`, current session status | Only completed sessions, private comments, no-show races, aggregate query performance. |
| **Email / jobs / worker** | `docs/workers.md`, `internal/notifications`, `platform/worker`, `bootstrap/worker.go`, migration `000009` | Lease reclaim, retry safety, SMTP at-least-once, provider acceptance vs inbox delivery, graceful shutdown. |
| **Database / migrations** | `docs/database/implementation.md`, **all** current migration pairs and their dependent adapters | Forward + rollback tests, real PostgreSQL constraints, irreversible data impacts, booking trigger behavior. |
| **HTTP routing / integration** | `bootstrap/identity.go`, relevant module's `interfaces/http`, `docs/identity.md` | API route conflicts, CSRF/same-origin, JSON shape, pagination, error codes, middleware consistency. |
| **Frontend (future)** | Check whether `frontend/` now exists; `README.md`, `docs/identity.md`, `docs/catalog.md`, API module guides | React/TypeScript, same-origin cookie auth, no secret or grade leakage, mobile usability, existing API contracts. |
| **CI / Docker / deployment** | `.github/workflows/backend.yml`, `compose.yaml`, `.env.example`, `backend/Dockerfile`, `docs/workers.md` | No secrets, migration ordering, service health, CI parity, deploy vs package distinction. |

## 8. Development and verification commands

From the repository root, with Go, Docker, Python 3, PostgreSQL client tools, and `migrate` available:

```bash
# Inspect the environment; do not overwrite an existing .env.
cp -n .env.example .env

# Local infrastructure.
docker compose up -d --wait postgres redis

# Run migrations after loading your local DATABASE_URL.
# Example local-only URL is documented in docs/database/implementation.md.
migrate -path backend/migrations -database "$DATABASE_URL" up

# Run the API or worker (after loading required environment variables).
(cd backend && go run ./cmd/api)
(cd backend && go run ./cmd/worker)
# Alternatively: docker compose --profile workers up -d --build worker

# General Go checks.
(cd backend && go test -race ./...)
(cd backend && go vet ./...)
(cd backend && go build ./...)

# Real-service integration tests (disposable PostgreSQL/Redis, SMTP fixture as needed).
bash backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

Before running any command that can delete or overwrite data, verify you are targeting a **disposable development/test database**. Do **not** run `migrate down -all` against a shared or production database. The rollback of migration `000010` intentionally refuses to remove its enum types while attendance notification rows remain; do not bypass that safety check.

Use `gofmt` on changed Go files. CI also runs format checks, `go vet`, `go test -race`, `go build`, the integration runner, and migration/concurrency validation. If Docker/service-dependent tests cannot run, say so explicitly and report the other evidence instead.

## 9. API, security, and privacy defaults

- Existing API prefix: `/api/v1`. Use the established handlers and response conventions, including `{"error":"..."}` for errors and `data`/pagination wrappers where already used.
- Validate JSON shape, length, UUIDs, status transitions, and request ownership; use suitable HTTP error semantics (`400`, `401`, `403`, `404`, `409`, `422`, `500`) consistent with neighboring handlers.
- Public endpoints are exceptional and narrowly scoped (for example, registration-safe catalog lookups). Never make an existing protected endpoint public incidentally.
- The API uses Redis-backed opaque cookies and checks current authorization against PostgreSQL. State-changing browser calls require same-origin validation. Never bypass those checks.
- Faculty authorization is a **server-side rule**: tutor-faculty oversight for session-specific issues, home-faculty authority for user sanctions. Do not accept faculty selection from an untrusted request as authority.
- Expose aggregate public tutor ratings, not individual feedback comments, qualification grades, student numbers, or personal account data.
- Follow university privacy/retention requirements. Retention periods in the design are proposed/approved for the project but require Data Protection Officer review before a production deletion schedule is activated.
- Keep credentials, university database access, SMTP passwords, raw tokens, and `.env` out of Git, responses, and logs. Use `.env.example` for documented placeholders only.

## 10. Testing and definition of done

A task is not finished just because it compiles. Before handoff:

- [ ] Implemented the requested acceptance criteria and **only** the necessary supporting scope.
- [ ] Preserved module boundaries; no duplicate implementation, circular dependency, or unmanaged global state.
- [ ] Verified all role, faculty, verification, ownership, and suspension cases touched by the change.
- [ ] Added unit tests for pure rules, and integration tests for real PostgreSQL interactions/transactions where relevant.
- [ ] Exercised invalid inputs, conflicts, retries, and concurrency races when they are part of the behavior.
- [ ] Confirmed history and notification records are consistent with state changes.
- [ ] Kept API documentation, migration docs, `.env.example`, and run instructions accurate if affected.
- [ ] Ran formatter, `go test -race ./...`, `go vet ./...`, and impacted integration suites—or documented why each could not run.
- [ ] Reviewed `git diff` and `git status`; no unrelated files, credentials, generated artifacts, or accidental destructive changes.
- [ ] Reported exactly which capabilities are **implemented**, **tested**, **deferred**, or **blocked**.

## 11. Agent-to-agent handoff (required)

At the end of a task, leave a concise handoff in the issue/PR/conversation with this structure:

```markdown
### Task / issue
- Goal and acceptance criteria:
- Branch and working-tree status:

### Existing implementation inspected
- Modules, documentation, schema, tests, interfaces:

### Changes made
- Files/modules changed:
- New or modified API/Go interfaces:
- Migrations/configuration changes:
- Authorization, privacy and transaction considerations:

### Verification
- Commands actually run:
- Results and failures:
- Tests not run and why:

### Handoff / next agent
- Remaining work or required configuration:
- Known edge cases / risks:
- Contract and ordering dependencies:
- Files or resources the next agent should inspect first:
```

**When handing work to a parallel agent**, identify the exact files/modules you own, any shared-file conflict, expected input/output interfaces, and which agent is responsible for integration tests and bootstrap wiring. Do not claim that code was committed, deployed, emailed, or verified unless the corresponding action actually succeeded.

## 12. Priorities for future agents

Use the current GitHub issue/milestone as the task scope. As of the inspected `main`, core backend modules through Feedback are present. The docs still identify additional work in **general issue-report resolution, coordinator account suspension/reinstatement and monitoring, operational hardening, the React frontend, and Release 1 features**. These are planning directions, **not** permission to implement everything in one task.

Prefer one cohesive vertical slice at a time: define its contract, use the existing owning module, implement domain/application/infrastructure/HTTP integration as needed, test with real dependencies, and hand off clearly. Do not introduce a microservice, new database, new ORM, generic queue, or large refactor without an explicit requirement and evidence that the existing modular monolith cannot support the task.

# Frontend Architecture and Implementation Rules

## 1. Technology Stack

The Tutti frontend uses:

- **React** for UI components.
- **TypeScript** for type safety.
- **React Router** for navigation, if already used by the project.
- **CSS and the existing styling system** for presentation.
- **The existing HTTP client** for communication with the Go backend.
- **The existing testing tools** for unit, integration, and end-to-end testing.

Agents MUST inspect `frontend/package.json` and the existing frontend implementation before introducing dependencies or making architectural changes.

Do not install alternative routing, state-management, styling, form, or HTTP libraries when the existing project already provides a suitable solution.

## 2. Feature-Based Architecture

Organize frontend code by business feature rather than grouping all components, hooks, or API calls into global directories.

Follow this architecture where compatible with the existing repository:

```text
frontend/
├── public/
├── src/
│   ├── app/
│   │   ├── App.tsx
│   │   ├── router.tsx
│   │   └── providers.tsx
│   │
│   ├── pages/
│   │   ├── LoginPage.tsx
│   │   ├── StudentDashboardPage.tsx
│   │   ├── TutorDashboardPage.tsx
│   │   └── CoordinatorDashboardPage.tsx
│   │
│   ├── features/
│   │   ├── auth/
│   │   ├── courses/
│   │   ├── tutors/
│   │   ├── availability/
│   │   ├── bookings/
│   │   ├── feedback/
│   │   ├── issues/
│   │   └── coordination/
│   │       ├── components/
│   │       ├── hooks/
│   │       ├── api/
│   │       └── types/
│   │
│   ├── shared/
│   │   ├── api/
│   │   ├── components/
│   │   ├── hooks/
│   │   ├── types/
│   │   └── utils/
│   │
│   ├── styles/
│   └── main.tsx
│
├── tests/
│   └── e2e/
├── package.json
└── tsconfig.json
```

This structure is a target organization, not permission to recreate existing directories or rewrite working code.

Agents MUST reuse the current structure and expand it incrementally.

Do not create empty folders or placeholder files solely to match the example.

## 3. Responsibilities of Each Layer

### `app/` — Application Composition

Contains application-level configuration:

- Root React component.
- Router configuration.
- Global providers.
- Authentication initialization.
- Application-wide error boundaries.
- Global application configuration.

Do not implement feature-specific API requests or business workflows directly in `App.tsx`.

### `pages/` — Route-Level Screens

Pages assemble feature components into complete screens.

Examples:

- Student dashboard.
- Tutor profile.
- Session booking.
- Coordinator monitoring.
- Coordinator issue resolution.

A page should mainly coordinate layout, navigation, and feature composition.

Avoid placing complex API logic, validation, or business calculations directly inside page components.

### `features/` — Business Features

Each feature owns its UI behavior and its integration with the corresponding backend capabilities.

Example:

```text
features/
└── bookings/
    ├── components/
    │   ├── BookingForm.tsx
    │   ├── BookingCard.tsx
    │   └── BookingList.tsx
    ├── hooks/
    │   ├── useBookings.ts
    │   └── useCreateBooking.ts
    ├── api/
    │   └── bookingsApi.ts
    ├── types/
    │   └── booking.ts
    └── utils/
        └── bookingDisplay.ts
```

Feature folders may contain their own tests.

Create only the files genuinely needed by the implementation.

### `shared/` — Reusable Technical and UI Infrastructure

Contains code shared across multiple features:

- HTTP client configuration.
- Reusable buttons, dialogs, inputs, and loading indicators.
- Generic hooks.
- Date formatting.
- Shared utility types.
- Common error-handling helpers.

Do not move business-specific components into `shared/` merely because they are used more than once within one feature.

Do not create a global collection of unrelated feature-specific API calls.

### `styles/` — Design System and Styling

Contains existing global styles, design tokens, and shared theme definitions.

Reuse established:

- Colors.
- Typography.
- Spacing.
- Breakpoints.
- Form styling.
- Accessibility conventions.
- Responsive layout patterns.

Do not introduce a second design system or mix unrelated styling frameworks.

## 4. Frontend Dependency Rules

Use the following conceptual dependency direction:

```text
app/
  ↓
pages/
  ↓
features/
  ↓
shared/
```

Dependencies should generally flow toward lower-level reusable code.

Rules:

1. `shared/` must not depend on individual business features.
2. Feature components should not import route-level page components.
3. Page components may compose multiple features.
4. App-level code may configure and compose pages and providers.
5. Avoid circular imports between features.
6. Cross-feature behavior should use clearly defined public interfaces or application composition.
7. Do not bypass established architecture to import internal implementation details from unrelated features.

Do not create abstraction layers without a concrete need.

## 5. Component Architecture

Prefer small, focused components with clear responsibilities.

Separate, where useful:

- Presentational components.
- Feature-specific hooks.
- API request functions.
- Input validation.
- Type definitions.
- Display formatting.

Example:

```tsx
function BookingList() {
  const { bookings, isLoading, error } = useBookings();

  if (isLoading) {
    return <LoadingState />;
  }

  if (error) {
    return <ErrorState message="Unable to load bookings." />;
  }

  return (
    <div>
      {bookings.map((booking) => (
        <BookingCard key={booking.id} booking={booking} />
      ))}
    </div>
  );
}
```

This is an architectural illustration, not an instruction to introduce specific hooks or components when equivalents already exist.

Avoid:

- Extremely large page components.
- API calls scattered through unrelated UI components.
- Duplicated form validation.
- Multiple implementations of the same feature state.
- Unnecessary global state.
- Business calculations duplicated from the backend.

## 6. Real Backend API Integration

**The Go backend is the authoritative source of application data and business rules.**

Before implementing frontend functionality:

1. Locate the corresponding Go HTTP handler.
2. Inspect the actual route registration.
3. Inspect request and response DTOs.
4. Read API documentation.
5. Verify authentication requirements.
6. Implement requests using the existing frontend API client.
7. Match the real response structures.
8. Test the workflow against the running Go backend.

For example, coordinator functionality must use existing backend APIs such as:

```text
GET  /api/v1/coordinator/tutors
GET  /api/v1/coordinator/statistics
GET  /api/v1/coordinator/issues
POST /api/v1/coordinator/issues/{id}/resolve
POST /api/v1/coordinator/sessions/{id}/cancel
POST /api/v1/coordinator/users/{id}/suspend
POST /api/v1/coordinator/users/{id}/reinstate
```

Verify that endpoints exist in the current checkout before using them.

Do not invent API endpoints or pretend unavailable functionality already exists.

If a required backend capability is missing, report the gap and follow the task scope.

## 7. API Client Architecture

Centralize shared HTTP behavior in the existing client infrastructure.

For example:

```text
shared/api/
├── client.ts
├── errors.ts
└── types.ts
```

Keep feature-specific API calls close to their feature:

```text
features/
└── coordination/
    └── api/
        ├── coordinatorIssuesApi.ts
        ├── coordinatorMonitoringApi.ts
        └── coordinatorAdministrationApi.ts
```

The shared client should handle common concerns such as:

- API base URL.
- Credentials and cookies.
- Request serialization.
- Consistent response parsing.
- HTTP error normalization.
- Network failures.
- Request cancellation, where supported.

Do not implement competing HTTP clients for different features without justification.

Do not store authentication tokens in localStorage or sessionStorage unless the actual security architecture explicitly requires that design.

The existing Tutti authentication mechanism uses session cookies.

Follow its real cookie and same-origin requirements.

## 8. Authentication and Authorization

The frontend must integrate with the existing Identity backend.

Frontend route guards and conditional rendering are for user experience, not security enforcement.

The Go backend remains responsible for authorization.

Requirements:

- Derive displayed permissions from the authenticated backend session.
- Handle authentication expiration.
- Handle `401 Unauthorized`.
- Handle `403 Forbidden`.
- Handle inaccessible resources consistently.
- Avoid displaying coordinator-only functionality to users lacking coordinator permissions.
- Do not trust frontend role state as authoritative.
- Do not bypass backend restrictions using mock users.
- Do not implement a separate frontend authentication system.

Do not implement a fake SSO provider or fabricate university identities.

The project's approved SSO migration is a separate milestone.

## 9. Server State and Client State

Distinguish between:

**Server state**

Data owned by the backend:

- Tutors.
- Courses.
- Availability.
- Bookings.
- Feedback.
- Issue reports.
- Coordinator statistics.
- Suspension status.

**Client state**

Temporary frontend behavior:

- Open dialogs.
- Selected filters.
- Form input.
- Pagination controls.
- Active tabs.
- Local UI preferences.

Use the project's existing server-state handling strategy.

Do not add Redux, Zustand, TanStack Query, or another state library unless there is a demonstrated need and it fits the current architecture.

Avoid duplicating backend state in unrelated React components.

After successful mutations, refresh or invalidate the affected server data using the existing pattern.

Never optimistically display a successful business transition when the backend rejected it.

## 10. TypeScript and API Contracts

Use explicit TypeScript types for API requests and responses.

Avoid `any` where meaningful types can be defined.

Types must reflect the actual Go DTOs and JSON responses.

For example, where the backend uses:

```json
{
  "data": {
    "id": "uuid",
    "status": "OPEN"
  }
}
```

Do not incorrectly type the response as if it returned the issue object directly.

Preserve:

- Optional and nullable fields.
- Backend enum values.
- Timestamp formats.
- Pagination envelopes.
- Error response structures.
- Field naming conventions.

Do not invent fields to simplify frontend rendering.

If the repository maintains OpenAPI contracts or generated types, follow the existing generation workflow.

## 11. Forms and Validation

Forms should provide immediate, accessible feedback while delegating final validation to the backend.

Use established form and validation conventions.

Requirements:

- Validate required fields for user experience.
- Display backend validation errors.
- Handle pending submissions.
- Prevent accidental duplicate submissions where practical.
- Preserve input when a recoverable request fails.
- Show clear success and failure states.
- Respect backend size and format limits.
- Do not calculate authoritative business decisions in the browser.

For coordinator operations, ensure required reasons and resolution notes are collected before sending requests.

Do not assume client-side validation makes unauthorized requests safe.

## 12. Loading, Empty, Error, and Success States

Every API-connected feature should support relevant states:

- Loading.
- Empty result.
- Successful result.
- Validation failure.
- Authentication expiration.
- Authorization denial.
- Network error.
- Server error.
- Successful mutation.

Do not replace API failures with fabricated success data.

Avoid silently swallowing errors.

Show useful user-facing messages without exposing SQL errors or sensitive server details.

## 13. Accessibility and Responsive UI

Follow the existing design system and accessibility conventions.

Implement:

- Semantic HTML.
- Accessible labels.
- Keyboard navigation.
- Visible focus states.
- Appropriate ARIA attributes where needed.
- Accessible form errors.
- Responsive layouts.
- Usable loading and empty states.
- Proper dialog focus management.

Do not introduce inaccessible custom controls when suitable existing components are available.

## 14. Testing Architecture

Separate testing responsibilities.

### Unit and Component Tests

May use mocks for isolated frontend logic.

Examples:

- Button behavior.
- Form field validation.
- Loading indicators.
- Error rendering.
- Pagination UI.
- Pure formatting functions.

Mocks must be explicitly identified as test doubles.

### API Integration Tests

Must verify compatibility with the actual Go backend.

Use real endpoints and isolated test fixtures.

Do not claim API integration based solely on intercepted or fabricated responses.

### End-to-End Tests

Must exercise the actual application stack:

- React frontend.
- Go HTTP API.
- PostgreSQL with migrations.
- Redis where required.
- Real authentication and authorization behavior.
- Controlled test accounts and test records.

Do not create a fake backend solely to make end-to-end tests pass.

### Smoke Tests

A full-stack smoke test must connect to a real running Go backend.

It should verify at least one meaningful frontend-to-backend workflow appropriate to the feature.

A browser rendering a page with hardcoded data is not proof of successful integration.

## 15. Development and Docker Rules

The project is developed on Windows with Docker Desktop and WSL 2.

Before testing:

1. Inspect the existing `compose.yaml`.
2. Determine whether PostgreSQL and Redis are running.
3. Verify the required database migrations have been applied.
4. Start the real Go API using the repository's documented procedure.
5. Start the existing frontend development server.
6. Verify frontend requests reach the real backend.

Do not:

- Overwrite `.env`.
- Delete existing database volumes.
- Change configured local ports without reason.
- Run destructive database commands against development or production data.
- Create unnecessary mock API services.
- Require real university SMTP credentials for unrelated frontend tests.

Use isolated databases for automated integration testing where possible.

## 16. Frontend Implementation Workflow

For every frontend task, follow this sequence:

1. Read `AGENTS.md`.
2. Inspect the complete repository structure.
3. Understand the requested feature.
4. Locate the existing backend API.
5. Inspect request/response contracts.
6. Inspect existing frontend architecture and reusable components.
7. Identify the smallest necessary frontend changes.
8. Implement the feature within its appropriate feature module.
9. Connect it to the real backend.
10. Add appropriate unit/component tests.
11. Run integration or end-to-end tests against real services.
12. Run TypeScript checks, linting, and production build.
13. Review accessibility and error handling.
14. Document any missing backend capability.
15. Report exactly which checks were executed and passed.

Do not create unnecessary files, placeholder implementations, or duplicate infrastructure.

## 17. Completion and Evidence Requirements

Before marking frontend work complete, agents MUST report:

1. Frontend files created or modified.
2. Existing backend endpoints integrated.
3. API contracts verified.
4. TypeScript and build results.
5. Unit/component test results.
6. Actual frontend-to-backend integration test results.
7. Whether test doubles were used.
8. Authentication and authorization behavior tested.
9. Any missing functionality.
10. Known limitations.

**Do not claim full-stack verification if only isolated frontend tests were executed.**

A feature is considered fully integrated only when its behavior has been exercised against the actual Go backend and relevant persistence services.

## 18. Final Architectural Principles

- Follow the existing frontend structure before introducing new folders or libraries.
- Organize business behavior by feature.
- Keep pages focused on composition.
- Keep shared code independent of business features.
- Keep API integrations inside feature-specific adapters.
- Use TypeScript types matching real backend contracts.
- Reuse the existing design system.
- Do not duplicate backend business rules.
- Do not create fake backend implementations.
- Keep mocked tests separate from real integration evidence.
- Preserve authentication and authorization boundaries.
- Prefer small, maintainable components over monolithic pages.
- Preserve working code and avoid unrelated refactors.

**The Tutti frontend must be a real client of the existing Go application, not an independently simulated version of the backend.**

---

**If unsure where to start:** identify the issue; read `README.md`, this `AGENTS.md`, the relevant `docs/<module>.md`, the current `backend/internal/<module>/` code, affected migrations, bootstrap wiring, and the integration test that exercises the workflow. Then propose the smallest safe change.
