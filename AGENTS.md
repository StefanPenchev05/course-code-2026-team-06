# AGENTS.md — Tutti engineering agent guide

> **Scope:** Entire repository, unless a more specific `AGENTS.md` in a subdirectory overrides part of this guide.
>
> **Project:** Tutti — Peer-to-Peer Tutoring Platform, Team 6, Software Engineering 2026.
>
> **Repository:** `StefanPenchev05/course-code-2026-team-06`  
> **Project board:** <https://github.com/users/StefanPenchev05/projects/11>
>
> **Important:** This guide builds on the repository inspected on `main` on 2026-11-19 and the real-time planning review on 2026-11-23. It is not a substitute for inspecting the actual working tree. **Current code, applied migrations, and tests take precedence over assumptions in this file.**

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

**Current vs planned:** The project README names **React + TypeScript** for the frontend and **C** as part of the intended stack, but as checked on 2026-11-23 `frontend/` contains only `.gitkeep` and there is no implemented C integration directory. Do not claim either exists or generate a large scaffold unless the task specifically requests it. When implementing the frontend, first inspect the actual repository again; the architecture proposal favors `frontend/src/app`, `pages`, `features`, and `shared`.

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
| **Frontend (future)** | Check whether frontend source now exists; `README.md`, `docs/identity.md`, `docs/catalog.md`, API module guides | React/TypeScript, same-origin cookie auth, no secret or grade leakage, mobile usability, existing API contracts. |
| **Real-time / WebSockets** | `docs/realtime.md`, `docs/identity.md`, `internal/identity/interfaces/http/middleware.go`, `bootstrap/identity.go`, `docs/workers.md` | Cookie auth before upgrade, exact Origin, TimeoutHandler bypass only for socket route, authorization/faculty isolation, bounded connections, no pre-commit publication, worker fanout and reconnect resync. |
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

## 12. Real-time events and WebSocket work (#84–#94)

**Planning reference:** `docs/realtime.md` is the version-1 protocol/design baseline; `docs/codex-realtime-foundation.md` is the scoped Codex handoff for #85/#86. These are **proposals until implemented and tested**, not evidence that real-time transport exists.

- **Implementation sequence:** #85 event contract → #86 authenticated gateway → #87 scheduling event publication → #90 shared React client → #91/#92 booking/availability UX → #88 coordination/feedback publishers → #93 coordinator UX → #89/#94 load/security/browser E2E. Keep each task narrowly scoped.
- **Session security:** `GET /ws` must reuse Identity's authenticated opaque Redis session and validate the exact trusted browser Origin **before upgrading**. Check current account, suspension, role and faculty privileges server-side; do not authorize arbitrary client-declared rooms, IDs, roles or faculties. Plan periodic/session-change revalidation for long-lived connections.
- **HTTP server hazard:** `backend/internal/bootstrap/identity.go` currently passes the entire mux through `http.TimeoutHandler` (10 seconds). An upgrade/hijacked WebSocket connection cannot run through that wrapper. Arrange a dedicated route/handler bypass with its own liveness timeouts and shutdown, **without weakening normal HTTP request timeouts**.
- **Domain invariants:** Database writes, eligibility checks, audit history, booking locks and email outbox remain the source of truth. Publish change notifications only **after successful commit**; no notification after rollback. An unavailable socket consumer must not invalidate an already committed business write.
- **Transport boundaries:** The connection hub belongs in platform infrastructure and is wired by bootstrap. Business application code depends on narrow publisher ports, not the hub implementation or HTTP handlers. Do not turn WebSockets into a second mutable API or a duplicate authentication layer.
- **Data minimization:** Use a versioned event envelope with an opaque event ID, type and minimal scoped identifier where needed. Do not broadcast grades, feedback comments, emails, student numbers, private disciplinary evidence/reasons, access tokens or arbitrary complete domain objects.
- **Workers and scale:** The worker is a separate process; API-local callbacks cannot report worker-originated expiry. For #87 plan transactional event storage/dispatch and for #89 address multiple API replicas (e.g. Redis pub/sub for transient fanout). Do not confuse pub/sub with durable event history or automatic replay.
- **Recovery and safety:** Assume best-effort event delivery. Reconnect with bounded backoff and refetch authorized HTTP data every successful connection to cover missed events. Bound per-connection queues, idle time, resource usage and cleanup; deduplicate event IDs and handle unknown versions safely.
- **Frontend status:** As inspected for this planning work on 2026-11-23, `frontend/` contains only `.gitkeep`. Do not assume React providers, components or cache libraries already exist. Coordinate with the frontend foundation work (#27) and the #90 owner before creating a shared client.
- **Verification:** Cover cookie auth, exact Origin, rejected/suspended sessions, cross-faculty isolation, concurrent connections, timeout bypass, slow-client disconnect, shutdown, transaction rollback, worker publication, reconnect/resync and multiple browser contexts. Report which suites actually ran.

## 13. Priorities for future agents

Use the current GitHub issue/milestone as the task scope. As of the inspected `main`, core backend modules through Feedback are present. The docs still identify additional work in **general issue-report resolution, coordinator account suspension/reinstatement and monitoring, operational hardening, the React frontend, and Release 1 features**. These are planning directions, **not** permission to implement everything in one task.

Prefer one cohesive vertical slice at a time: define its contract, use the existing owning module, implement domain/application/infrastructure/HTTP integration as needed, test with real dependencies, and hand off clearly. Do not introduce a microservice, new database, new ORM, generic queue, or large refactor without an explicit requirement and evidence that the existing modular monolith cannot support the task.

---

**If unsure where to start:** identify the issue; read `README.md`, this `AGENTS.md`, the relevant `docs/<module>.md`, the current `backend/internal/<module>/` code, affected migrations, bootstrap wiring, and the integration test that exercises the workflow. Then propose the smallest safe change.
