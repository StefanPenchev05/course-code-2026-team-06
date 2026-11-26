# Issue #99 — tutor semester listing verification

## Task / issue

[Issue #99](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/99)
supports FR12 / UC08 / D2 C9: a verified tutor needs a semester ID to create a
weekly availability rule. The issue is backend-only; #31 owns the selector UI.
Inspected on 2026-11-26 on `main`, starting at `c4151d6` with a clean working tree.
No branch creation, commits, pushes, merges or issue updates were performed.

## Existing implementation inspected

The endpoint was already implemented by `1a8afab` and consolidated by `f889cd9`:

- [CalendarService.ListForTutor](../backend/internal/catalog/application/semesters.go)
  checks Identity's `CanTutor`, delegates to the existing full-calendar `List`,
  filters using the receipt's trusted reference instant, normalizes UTC and
  initializes an empty result slice.
- [PostgreSQL calendar adapter](../backend/internal/catalog/infrastructure/postgres/semesters.go)
  uses a repeatable-read **read-only** transaction and `ORDER BY starts_at,id`.
  Filtering preserves this order; no additional sorting or query is needed.
- [Catalog HTTP handler](../backend/internal/catalog/interfaces/http/semesters.go)
  exposes the collection; [bootstrap](../backend/internal/bootstrap/identity.go)
  registers it with Identity's authentication middleware and the existing pool.
  Middleware loads current PostgreSQL permissions using the Redis-backed session.
- The semester entity contains exactly the four permitted response fields. The
  existing CLI calls unfiltered `CalendarService.List` and keeps operator receipts.
- Inspected root AGENTS.md, README, Catalog/Identity/Scheduling documentation,
  relevant catalog/identity/tutoring/audit migrations and tests, CLI dispatch,
  frontend availability components/API adapter, and the real-service test runner.

No functional defect was found in the endpoint, ordering, authorization or date
boundary. The remaining gaps were a route-level persistence-failure check,
explicit real-service evidence for an unverified actor with an active tutor
profile, CLI full-calendar preservation, a machine-readable OpenAPI contract,
and traceability to #99.

## Changes made

- [semester_query_test.go](../backend/tests/integration/semester_query_test.go):
  extend the existing real PostgreSQL/Redis HTTP test with database snapshots
  proving reads do not mutate tables, a pending student with an active synthetic
  tutor profile (403), an exact top-level `data` envelope, actual CLI `list` after
  all semesters have ended, and a real calendar-query failure with sanitized
  HTTP 500 and safe logging. Existing final verification decisions stay immutable.
- [openapi/catalog.yaml](openapi/catalog.yaml): scoped OpenAPI 3.0.3 contract for
  this endpoint, including both deployment-specific Identity cookie names.
- [catalog.md](catalog.md): route/access table, OpenAPI link and explicit frontend
  integration boundary. [requirements-traceability.md](requirements-traceability.md):
  FR12 / UC08 / E4 evidence and #99 link, retaining partial overall feature status.
- This report records acceptance and verification evidence.

No runtime code, interfaces, dependencies, configuration or migrations changed.
There is no duplicate semester repository, DTO model or authorization system.
The fault test renames the semester table only inside the database created by
`isolatedRealtimeDatabase`; it restores the name with `defer`, and the runner
removes its disposable services. No shared/development database is targeted.

## Final HTTP contract

`GET /api/v1/semesters`, with the existing authenticated opaque session cookie:

```json
{"data":[{"id":"00000000-0000-0000-0000-000000000406","name":"Synthetic semester","starts_at":"2027-01-01T00:00:00Z","ends_at":"2027-07-01T00:00:00Z"}]}
```

Only semesters with `ends_at > reference time` qualify, ordered by `starts_at`
then UUID `id`. The existing calendar clock uses UTC microsecond precision;
exactly-ended semesters are excluded. No results: `{"data":[]}`. No query
parameters are accepted. Success is 200 with `Cache-Control: no-store`.
Errors use `{"error":"message"}`: 400 for malformed/unsupported query,
401 for missing/invalid/expired/revoked sessions, 403 for insufficient current
permissions (including suspension/closure), 500 for infrastructure failure with
`internal server error`. No calendar mutation endpoint or operator report is
exposed. Scheduling's semester/DST/overlap/reservation validations are unchanged.

## Acceptance matrix

| Criterion | Evidence | Result |
| --- | --- | --- |
| Read-only tutor endpoint and bootstrap wiring | Existing handler + Identity middleware registration; real HTTP test | Implemented; locally verified |
| Authorized active verified tutor | `TestTutorSemesterAPI`, two approved tutors | 200 |
| Missing/revoked session | Real HTTP test, including logout | 401 |
| Unverified student/tutor; verified non-tutor | Real HTTP pending/non-tutor checks and new pending/profile fixture; deterministic application test also covers rejected verification | 403 |
| Suspended/closed/inactive tutor; coordinator-only | Real HTTP test plus `TestSemesterQueryPermissionsAndClock` | 403 |
| Current/future included; past/exact end excluded | Injected fixed clock in application and PostgreSQL HTTP tests | Verified |
| Chronological ordering with stable ties | SQL `ORDER BY starts_at,id`; real test inserts chronological counterexamples and tied starts | Verified |
| Only four fields, UTC, no operator receipt | Real HTTP field and envelope assertions | Verified |
| Empty collection | Nil/empty repository application case and advanced-clock real HTTP case | `{"data":[]}` |
| Read-only transaction and persisted state | Existing application read-only assertion, PostgreSQL transaction options and new full-table snapshot comparison | Verified |
| Repository failure and safe error | Existing application error propagation; new real query failure and log/body assertions | Sanitized 500 |
| CLI full-calendar behavior retained | Existing application regression plus new actual CLI list when HTTP returns no semesters | Full list and receipt retained |
| API documentation / traceability | Catalog guide, scoped OpenAPI, FR12 / UC08 / E4 links | Updated |
| Supports weekly selector | UUID `id` matches `semester_id`; name and UTC bounds available | Backend ready; frontend wiring separate #31 |
| Hosted CI and human review | Changes remain local and uncommitted | Not claimed; required before repository-level completion |

## Verification

Commands actually executed from the repository root (Go commands from `backend/`):

| Check | Result |
| --- | --- |
| Tracked Go files checked with `gofmt -l`; changed test formatted with `gofmt -w` | Passed; no unformatted files |
| `go test -race ./...` | Passed; service-dependent tests are separately exercised by the runner below |
| `go vet ./...` | Passed |
| `go build ./...` | Passed |
| `bash backend/tests/integration/test_identity.sh -race` | Passed in fresh PostgreSQL 17 / Redis containers; 29 top-level tests passed |
| `bash backend/tests/integration/test_identity.sh -race -run '^TestTutorSemesterAPI$'` | Passed again after the last envelope assertion; includes all added cases and real CLI list |
| `python3 backend/tests/integration/test_schema.py` | Passed: all 35 tables, concurrency, preservation guards, rollback and reapply |
| PyYAML parsing, local OpenAPI reference resolution, exact path/field checks | Passed; a full OpenAPI validator was not run |
| Documentation relative links and `git diff --check` | Passed |

The initial expanded integration run failed because its test setup attempted to
rewrite a final verification decision. That fixture was corrected to insert a
separate synthetic pending actor/profile, preserving the database guard; the
complete suite and final focused run then passed. No production constraint was
disabled or changed.

The standard runner skips the opt-in 200-user realtime load test, the runtime
worker-image smoke subtest (separate Docker-image runner), and live university
SMTP (requires explicit credentials/live-email opt-in). Two standalone subprocess
helper test entries also skip unless invoked by their owning process tests; the
parent process journeys ran. None of these skipped checks tests this endpoint.
Hosted GitHub CI, human review and frontend/browser tests were not run for these
local test/documentation-only changes. The new OpenAPI file received syntax and
reference checks, not certification by a complete specification validator.

Disposable runners cleaned up their own containers/databases. Existing developer
PostgreSQL/Redis and unrelated project containers were preserved. Working tree
changes are limited to the five files listed above and remain uncommitted on
`main`.

## Handoff / next agent

The current frontend has weekly list/edit/delete and skipped-date controls, plus
a separate one-off form. `frontend/src/features/availability/api.ts` has neither
a semester fetch nor a weekly-rule creation call, and there is no create-weekly
form. For #31, add a typed semester collection reader, a selector using the UUID
as `semester_id`, loading/error/empty states and the weekly-create workflow.
Reuse the current HTTP client/session and re-fetch after successful creation;
backend validation remains authoritative. Start with the Catalog OpenAPI,
`WeeklyRules.tsx`, the availability API adapter, and the existing real-backend
`frontend/tests/e2e/availability.spec.ts` (which already verifies the semester
endpoint but provisions weekly rules through the real API).

This task makes no claim of new frontend browser verification, SMTP delivery or
hosted CI. The complete recurring-availability feature remains partially
implemented until its creation UI and its integration journey are delivered.
