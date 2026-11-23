# Audit preservation — Issue #26 / NFR12

This guide describes the implemented application workflows through migration
`000015_audit_preservation`. It extends the existing module histories; it does
not introduce an event store, a general audit service, or a deletion workflow.

Issue #48 adds a separate [home-faculty flag inbox](coordination.md#home-faculty-flagged-user-review-fr18--uc12--uc16--nfr2-issue-48):
`GET /api/v1/coordinator/flagged-users`. It reuses the existing per-user FLAG and
VIOLATION history routes for details rather than duplicating private evidence.
The inbox uses stored flag faculty, current coordinator authority, current total
user/semester violations (including legacy rows and later incidents) and current
Identity suspension state in one read-only snapshot. A reinstated or relocated
user retains the old flag. An old faculty may see that authorized semester total
without gaining access to another faculty's individual incident details or current
sanction authority. The six-field summary excludes reasons, grades and evidence.
No audit record, retention rule, migration or writer changes for Issue #48.

## Investigation and authoritative sources

The implementation review read the root `AGENTS.md` (no nested guides), the
complete repository tree, README, configuration examples, Dockerfile, Compose,
CI, all migration pairs 1–14, relevant module layers, bootstrap and tests.
There is no frontend or separate `api/` specification directory in this checkout.

All documentation was reviewed, including:

- `1.1-1.2_stakeholders--user-stories.md`,
  `1.3-1.4_user-story_mapping--requirement_analysis.md`,
  `2_use-case-models.md`, `3_domain-model.md`, `4_sequence-diagram.md`.
- `requirements-decisions.md`, `requirements-traceability.md`.
- `identity.md`, `catalog.md`, `tutoring.md`, `scheduling.md`, `coordination.md`,
  `feedback.md`, `workers.md`.
- `database/implementation.md`, `database/tutti-schema-v0.3.md`,
  `database/tutti-schema-v0.2.dbml`.
- Both historical PDFs (`Deliverable_1.pdf` and
  `Tutti-Deliverable_1_presentation.pdf`) using text extraction, and all ten
  images under `images/`. No document was unreadable or modified for extraction.

Knowledge map:

| Responsibility | Source and interpretation |
|---|---|
| Traceability | Normative NFR12 in the requirements mapping: actual writers, actor, time, state and atomic evidence; table existence alone is insufficient |
| Discipline/account rules | FR18/FR19, UC13/UC14/UC16, schema v0.3 and current Coordination/Identity/Scheduling guides |
| Faculty/privacy | NFR1/NFR2/NFR4 and module contracts: session tutor faculty differs from account home faculty |
| Database/transactions | Applied migrations and concrete writers, then database implementation/schema v0.3 |
| Retention | U04 in requirements decisions; provisional design periods are not university approval |
| Open decisions | U01 start grid, U02 SSO, U03 academic evidence, U04 DPO retention approval; none is silently resolved here |
| Historical material | v0.2 DBML, original PDFs/UML describe superseded multi-subject applications, expiry and semester rules; current migrations and approved refinements govern |

Initial gaps: mutable session/application histories, qualifications, violations,
flags and finalized decision evidence; no explicit violation recording actor or
historical home faculty; no captured flag basis; no retained offering withdrawal
after reapproval; application history used transaction-start time instead of the
recorded review time; no authorized suspension-history listing. Existing #22–25
transactions, actor fields, stored faculties and cancellation links were retained.

## Workflow coverage

All rows below describe supported application workflows. PostgreSQL fixtures,
university provisioning, and privileged manual changes are not additional
implemented business APIs.

| Workflow / owning module | Event and durable evidence | Actor / time | Historical faculty | Transaction and verification |
|---|---|---|---|---|
| Registration / Identity | New user/student and PENDING `student_verifications` attempt | Submitting student ID; database `submitted_at` | Stored submission faculty | `Repository.Register`; Identity integration |
| Verification resubmission / Identity | New PENDING attempt; previous rejected attempt retained | Student ID; new database submission time | Each attempt's own faculty | `Repository.Resubmit`; Identity integration |
| Verification review / Identity | PENDING → APPROVED/REJECTED in the original attempt; rejected reason retained | Coordinator FK; database `reviewed_at` | Original attempt faculty | `Repository.Review`; row lock and conditional workflow; Identity integration |
| Tutor application / Tutoring | Initial PENDING and PENDING → APPROVED/REJECTED in `application_status_history`; immutable application/qualification | Student for submission, coordinator for review; exact parent submission/review timestamp | Stored application faculty | `transaction.CreateApplication/Decide`; `TestTutoringAPI`, audit insertion/outbox rollback and competing reviews |
| Tutor profile/offering approval / Tutoring | Profile creation and approved qualification/offering from the approved application; old qualifications survive explicit reapproval | Application reviewer and decision time; profile also retains `approved_at` | Approved application faculty | Same approval transaction; `TestTutoringAPI` |
| Eligibility withdrawal / Tutoring | Active → inactive offering plus `tutor_offering_deactivations` retaining offering, qualification, program, faculty, reason and time | SYSTEM; exact `deactivated_at` | Program faculty when revalidation runs, stored in event | `transaction.Deactivate` in caller's transaction; failure, concurrent revalidation, reapproval preservation tests |
| Booking/respond/completion / Scheduling | Initial REQUESTED; REQUESTED → ACCEPTED/DECLINED; ACCEPTED → COMPLETED in `session_status_history` | Authenticated participant; injected UTC clock | Immutable session tutor faculty | Existing booking transaction; `TestBookingLifecycle` |
| Participant cancellation / Scheduling + Coordination | REQUESTED/ACCEPTED → CANCELLED, origin PARTICIPANT; late cancellations append `user_violations` | USER cancellation history; SYSTEM automatic strike with NULL recording user, affected participant retained separately; event/recording time equals cancellation time | Session history: session faculty; violation: locked participant's home faculty at recording | History precedes violation in the same transaction; violation FK/trigger proves cause; booking and audit rollback tests |
| Expiry / Scheduling worker | REQUESTED → EXPIRED history | Explicit SYSTEM, NULL user; worker's trusted UTC batch time | Stored session faculty | Locked bounded batch, conditional update, history/outbox; booking/worker regressions |
| Attendance submission/rejection / Coordination | PENDING allegation with reporter/time, then single REJECTED decision with reviewer/time/note | Reporter, then coordinator; injected UTC clock | Stored session tutor faculty | Report transaction; attendance tests; rejection never creates a strike |
| Attendance confirmation / Coordination + Scheduling | Single CONFIRMED decision, linked NO_SHOW violation; session ACCEPTED/COMPLETED → NO_SHOW where needed | Reviewer is recording actor, absent participant is affected user; incident at session end, recorded at review | Report/session: session faculty; violation: absent user's locked home faculty at review | Report + correction history + violation + flag + outbox atomic; attendance/concurrency tests |
| Semester flag / Coordination | Append-only `user_semester_flags`, unique user/semester, two distinct contributing violation FKs | Explicit SYSTEM; triggering decision time | Home faculty captured at flag creation | Affected account lock serializes counts; same incident transaction; flag insertion rollback, duplicate and semester tests; never auto-suspends |
| General issue / Coordination | Initial OPEN and OPEN → RESOLVED `issue_status_history`; original report and final resolution immutable | Reporter/coordinator; exact database creation/resolution time | Stored report faculty | Issue + history same transaction; `TestIssueReporting` including failures/races |
| Policy cancellation / Coordination + Scheduling | CANCELLED history, origin COORDINATOR_POLICY | Authorized coordinator; trusted decision time | Stored session tutor faculty | Existing policy transaction; `TestPolicyCancellation`; no strike |
| Suspension/reinstatement / Identity + Coordination | False → true / true → false `user_suspension_history`, private reason and decision ID | Authorized coordinator; trusted UTC decision time | Target's locked home faculty at decision | Existing account transaction; `TestSuspensionAdministration`, failures/concurrent decisions |
| Suspension cancellation / Scheduling | Future REQUESTED/ACCEPTED → CANCELLED, origin SUSPENSION and FK to authentic account decision | Acting coordinator; same account decision time | Session history retains session faculty; linked private decision retains home faculty | Same account transaction across all future sessions; generic session reason, no strike; suspension tests including >100 sessions |

There is no supported account-closure, coordinator reassignment, grade-edit,
independent tutor-profile deactivation or general program-change administration
API. Revalidation is an existing internal transaction-bound capability, not
authorization to build those APIs. Activation tokens retain their issuing
coordinator/creation/consumption metadata; password activation is not a new
disciplinary transition. Profile text edits are not disciplinary history.

## Append-only and cause guarantees

Migration 15 rejects UPDATE, DELETE and TRUNCATE of session/application history,
qualifications, violations, flags and offering withdrawals. Existing issue and
suspension history guards remain. Verification, tutor application, attendance
and general issue records retain original submission fields and permit only
their pending/open → final decision; final decisions cannot be rewritten,
deleted or truncated. Existing status/actor/faculty constraints still apply.

New late violations explicitly identify SYSTEM as the automatic recording actor;
the affected participant remains the authenticated USER actor in their source
cancellation history. They require matching immutable PARTICIPANT cancellation history
with the same session, actor and time and an actual late cancellation. The
database links that history ID; Coordinator code does not write Scheduling's
tables. No-show violations require the confirmed attendance decision, affected
participant, reviewer, review time and scheduled end. All cause writes share the
business transaction. Policy and suspension origins cannot qualify as late
participant violations.

New flags store SYSTEM and two distinct same-user/same-semester violation FKs.
The writer selects the first two incidents ordered by recording time and ID;
this captures a sufficient basis at creation, not a mutable list of every later
incident. Existing records remain counted under FR18. A flag's unique key and
account locking prevent duplicate flags. Further incidents remain independent
immutable facts.

Restricted-role integration tests grant DML and TRUNCATE intentionally to a
NOSUPERUSER role, prove that the triggers reject mutation, and compare full
before/after evidence. These guarantees do not defeat a database owner or
superuser who can disable triggers, drop schema or change privileges. Production
application roles should not own tables or have DDL/role-administration rights.
The repository does not provision a production database role.

## Coordinator historical read contract

`GET /api/v1/coordinator/users/{id}/audit-history`

Parameters: `category=SUSPENSION|VIOLATION|FLAG` (default SUSPENSION), `limit`
(default 25, 1–100), `offset` (default 0, 0–100000). One category per request;
there is no combined cross-module timeline. Unknown/duplicate query parameters
are 400, invalid identifiers/categories/ranges are 422. No mutation route exists.

Response: `data` plus `pagination: {limit,offset,has_more}`. Ordering is event
recording time descending, then event ID descending; one extra row detects
`has_more`. Faculty predicates run before LIMIT/OFFSET. No total count is exposed.
Unknown accounts and accounts with no visible records both return an empty list.
All successful and handler error responses use `Cache-Control: no-store`.

Authentication uses the existing Redis cookie and live Identity principal. The
repository resolves active coordinator authority again in a read-only repeatable
read transaction, shared with all source queries. A stale middleware faculty is
never used as authority. Closed/suspended/removed coordinators are denied;
students and tutors have no access to this administrative route. Authority is
consistent as of the read snapshot; later changes apply to subsequent requests.

Each event's stored faculty controls visibility, independently of the target's
current program. A move does not transfer earlier private reasons or strikes to
the new faculty. An old faculty can still read its own events without current
student personal details. Existing composite reviewer/faculty FKs can prevent
reassignment of coordinators who own historical decisions; this issue does not
redesign that provisioning constraint.

Identity supplies suspension history through a published transaction-bound read
port. Coordination reads its own violations/flags. DTOs exclude names, email,
student numbers, grades, qualifications and other participants. Suspension
reasons appear only in this authorized account projection; shared session
cancellation wording remains generic. Violation/flag evidence IDs do not grant
permission to read an attendance report owned by another session faculty.
Existing issue, attendance, verification and application detail endpoints remain
their authorized read paths; no duplicate endpoints were added for those records.

## Legacy data and migration ordering

No backfill guesses a missing actor, reason, timestamp, origin, faculty or
transition. Pre-15 violations retain NULL new metadata; their historical home
faculty cannot safely be inferred from today's program, so they are absent from
the new faculty-filtered violation listing. Existing flags have authentic stored
faculty/time and remain visible with NULL actor/basis. Earlier histories and
oversized legacy reasons survive unchanged. Existing parent submissions do not
justify synthesizing missing status-history rows. Historical application-history
timestamp differences are preserved; new writers use exact parent event time.

Deploy migration 15 before the updated API/worker writers. Old writers cannot
create the newly required violation/flag metadata, so coordinate process rollout;
do not run old writers against the upgraded schema. Stop/drain old writers for
the migration. No migration was automatically applied to the persistent local DB.

The down migration refuses to remove any new violation metadata, flag actor/basis,
or offering withdrawal evidence. Before those exist it removes the new guards,
indexes and empty structures, preserving all original rows. Reversing that
protection is privileged maintenance, not a retention workflow. After new evidence
exists, retain the schema and use a forward repair. Earlier migrations are unchanged.

## Retention and privacy inventory

There is no automatic cleanup, archival or export process for these categories.
Records currently remain until privileged administration intervenes. That is an
implementation fact, **not an approved indefinite-retention policy**. U04 requires
university/DPO approval of periods and erasure/anonymization procedures.

| Category/storage | Sensitive information | Ordinary mutation / evidence references | Provisional design period, not approved policy |
|---|---|---|---|
| `student_verifications` | Student, faculty, decisions, reviewer and rejection reason | Submission/final decision protected; new attempts append; user/coordinator/faculty FKs | Rejected verification evidence: 1 year; other account evidence requires U04 classification |
| `tutor_applications`, `tutor_qualifications`, `application_status_history`, `tutor_offering_deactivations` | Grades, qualification evidence, reviewer, program/faculty, decisions/reasons | Immutable evidence/final decisions; offerings may change through existing workflows; qualification and offering FKs retain causes | Qualifications proposed 3 years after approval ends; exact linked-history handling requires approval |
| `session_status_history` | Participant/actor references, times, cancellation/decision reasons | Append-only; session FK and suspension-decision FK; session identity/faculty immutable | Session/history proposed 3 years after session end |
| `attendance_reports` | Allegations, reporter/absent user, review details | Original evidence and final decisions protected; session/reviewer FKs; confirmed decision referenced by violation | Discipline proposed 3 years after semester end; allegation classification requires U04 |
| `user_violations` | Affected user, actor, session/semester, home faculty and cause | Append-only; attendance or cancellation-history links; no ordinary erasure | Discipline proposed 3 years after semester end |
| `user_semester_flags` | Affected user, semester/faculty, incident basis | Append-only; two evidence FKs for new flags; no automatic sanction | Discipline proposed 3 years after semester end |
| `issue_reports`, `issue_status_history` | Complaint description, reporter, resolver and resolution note | Protected report/final decision and append-only history; faculty/session/report FKs | Final category-specific classification/period remains subject to U04 |
| `user_suspension_history` | Private reasons, actor, affected account, past/current suspension state, faculty/time | Append-only; account/coordinator/faculty references; cancellation history retains decision FK | Discipline/account history classification requires U04; account design proposes up to 1 year after closure, discipline 3 years after semester end |
| `notifications` | Recipient, message payload, queued/sent/failed times, retry/lease metadata, source IDs | Operational delivery state remains mutable; source FKs preserve references, deduplication prevents duplicate queueing; not an append-only audit-event table | Terminal notification cleanup proposed after 1 year; no cleanup enabled |

Foreign keys prohibit removing referenced evidence through ordinary deletes.
Append-only behavior is separate from eventual privacy/erasure obligations and
does not establish GDPR compliance. Privileged migrations/owners can ultimately
remove data; migration 15 specifically refuses evidence-losing rollback. A future
approved retention implementation needs category decisions, legal holds, handling
of linked records and backups, minimized exports, and an independently audited
privileged archival/anonymization/deletion procedure. Ordinary HTTP access must
not be given a bypass for these protections.

## Windows PowerShell verification

From the repository root, with Docker Desktop running Linux containers:

```powershell
# Rebuilds runtime and test images; fresh PostgreSQL 17 and Redis, no .env/Compose DB use.
.\backend\tests\integration\test_docker.ps1 -AuditOnly

# Required full checks: format, vet, race tests, build, all real-service tests,
# local SMTP/runtime worker, schema upgrade/rollback/reapply and concurrency.
.\backend\tests\integration\test_docker.ps1

# Explicit non-race package check using the image built above.
docker run --rm tutti-backend-tests go test ./...

# Optional focused worker run; rebuilds the worker image and uses local SMTP fixtures.
.\backend\tests\integration\test_docker.ps1 -NotificationsOnly

git diff --check
git status --short --branch
```

Tests live in the existing package/fixture infrastructure: new audit subtests run
inside `TestSuspensionAdministration`; Tutoring tests cover exact event times,
withdrawal preservation and audit failures; Booking tests cover authentic incident
causes. `test_schema.py` tests actual 14→15 legacy preservation, down refusal,
safe rollback and reapplication. Existing CI discovers them through its unfiltered
integration package and schema runner; no CI configuration change is needed.

Native Go is optional on Windows: the test Dockerfile supplies the compiler,
race toolchain, psql and migrate. Package-only Go runs skip service-dependent
integration cases without their test environment; the full runner supplies it.
All test resources have a unique isolated network; cleanup touches only that run.
No university SMTP credentials or running Compose worker are required. Existing
worker start/log commands remain in [workers.md](workers.md).

Persistent development migration remains a separate, developer-reviewed action.
After inspecting the scripts and securely setting your own connection environment,
verify it is version 14 before applying exactly the next migration:

```powershell
migrate -path backend/migrations -database $env:DATABASE_URL version
# Only after reviewing the rollout/rollback limitations above:
migrate -path backend/migrations -database $env:DATABASE_URL up 1
```

Do not use the disposable-test URLs for persistent setup, do not overwrite `.env`,
and do not run rollback/all or delete Compose volumes against developer data.

## Implementation handoff — 2026-11-20

### Task / issue

- Issue #26, NFR12 and related FR18/FR19: authentic actor/time/cause evidence,
  append-only preservation, faculty-authorized historical reads and documented
  retention behavior.
- Branch `feature/feedback_coordinator_operations`, starting at committed Issue
  #25 (`6f0fba8`), initially clean. Changes remain uncommitted for developer review.
  No branch switch, commit, push, PR, issue closure or persistent DB migration.

### Existing implementation inspected

The investigation and workflow tables above identify all reviewed documents,
modules, real schema, writer transactions, published interfaces and tests.
Existing Identity authority, Scheduling locks/cancellation origins, Coordination
decisions, Notifications outbox, and #22–25 work are retained. There is no new
microservice, generic event framework or alternative transaction mechanism.

### Changes made — complete file inventory

Paths in this inventory are relative to the repository root.

| Layer | New files | Modified files / responsibility |
|---|---|---|
| Domain | `backend/internal/coordination/domain/audit.go` | Minimal private audit DTO; unknown actor metadata remains nullable |
| Application / authorization | `backend/internal/coordination/application/audit.go`; `backend/internal/identity/application/audit.go` | `backend/internal/tutoring/application/eligibility.go`: pass already locked program/faculty through `Deactivate(ctx, offering, program, faculty)`; `backend/internal/scheduling/application/booking.go`: persist cancellation history before its strike in the same transaction |
| Infrastructure | `backend/internal/coordination/infrastructure/postgres/audit.go`; `backend/internal/identity/infrastructure/postgres/audit.go` | `backend/internal/coordination/infrastructure/postgres/discipline.go`: actors, event home faculty, exact times, captured flag basis; `backend/internal/tutoring/infrastructure/postgres/write.go`: exact application time and transactional withdrawal evidence |
| HTTP / wiring | `backend/internal/coordination/interfaces/http/audit.go` | `backend/internal/bootstrap/identity.go`: wire Identity read port and protected GET route |
| Migrations | `backend/migrations/000015_audit_preservation.up.sql`; `backend/migrations/000015_audit_preservation.down.sql` | Existing migration pairs unchanged; one narrow evidence table, nullable legacy metadata, guards/FKs/indexes and evidence-preserving down refusal |
| Unit tests | `backend/internal/coordination/application/audit_test.go`; `backend/internal/coordination/interfaces/http/audit_test.go` | `backend/internal/tutoring/application/eligibility_test.go`; `backend/internal/tutoring/application/service_test.go`: verify trusted withdrawal context |
| Integration tests | `backend/tests/integration/audit_test.go` | `backend/tests/integration/attendance_test.go`; `backend/tests/integration/issues_test.go`; `backend/tests/integration/issue_inbox_test.go`; `backend/tests/integration/suspension_fixture_test.go`; `backend/tests/integration/suspension_test.go`; `backend/tests/integration/tutoring_test.go`; `backend/tests/integration/tutoring_revalidation_test.go`; `backend/tests/integration/test_schema.py` |
| Runner | — | `backend/tests/integration/test_docker.ps1`: additive mutually exclusive `-AuditOnly`, preserving existing modes and isolated cleanup |
| Documentation | `docs/audit-history.md` | `docs/coordination.md`; `docs/identity.md`; `docs/tutoring.md`; `docs/database/implementation.md`; `docs/requirements-decisions.md`; `docs/requirements-traceability.md` |

New interfaces: Coordinator `AuditRepository.ListAudit` / `AuditService.List`,
Identity `SuspensionHistory.ListSuspensionHistory`, transaction-bound adapters and
the GET route above. No mutation API or client-supplied actor/faculty authority.
The writer/reader interfaces preserve module ownership; withdrawal context comes
from existing Identity/Catalog ports rather than independent cross-module reads.
No Compose, Dockerfile, CI, dependency, port, environment or volume changes.

### Verification

Commands actually executed from Windows PowerShell (compilation inside the
existing Docker test image; native Go was unavailable):

| Command/check | Result |
|---|---|
| `docker run --rm --mount "type=bind,source=$auditBackend,target=/src" -w /src --entrypoint gofmt tutti-backend-tests -w <changed Go files>` | Applied formatting; final full runner's `gofmt -l cmd internal tests` found no unformatted files |
| `.\backend\tests\integration\test_docker.ps1 -AuditOnly` | Passed after updating older issue fixtures/assertions for preserved verification/report evidence; subsequent final full suite also covers all targeted cases |
| `.\backend\tests\integration\test_docker.ps1` | Passed on final implementation; rebuilt runtime/test targets; disposable PostgreSQL 17 and Redis |
| `go vet ./...`, `go test -race ./...`, `go build ./...` | Passed inside the full runner |
| `bash tests/integration/test_identity.sh -race` | Passed inside the full runner: Identity, Catalog, Tutoring, availability/bookings, attendance/feedback, issues, monitoring, policy, suspension/audit and Notifications |
| `python3 tests/integration/test_schema.py` | Passed: 33 tables, real legacy upgrades, 12–15 rollback/reapply/guards, concurrent reservation exclusion, complete disposable rollback/reapply |
| `docker run --rm tutti-backend-tests go test ./...` | Passed on the final image; service-dependent cases skip here because this package-only invocation has no test-service environment |
| `TestNotificationDelivery/worker_runtime_container` | Passed with rebuilt runtime image and local SMTP; worker entry point, retry/lease/failure/acknowledgment scenarios also passed |
| `TestUniversitySMTP` | Intentionally skipped: requires explicit live-email mode and real credentials; local SMTP verification passed |
| `git diff --check` (also with `-c core.safecrlf=false`) | Passed; working tree reviewed, no unrelated generated files or credentials |
| Relative Markdown file-target check over changed guides | 144 links resolved; no historical binary documentation edits |

The first targeted run failed two old issue tests: one deleted a verification
fixture, and one expected a foreign-key error instead of the new evidence guard.
Those tests now retain evidence and assert the stronger protection. A synthetic
attendance fixture that inserted a false late strike now asserts its rejection,
then tests a real confirmation/retry. No production constraint was disabled to
make the suite pass. The pre-existing privileged legacy-description fixture
explicitly disables/re-enables guards only within its disposable setup transaction;
the schema suite separately verifies real legacy upgrades without bypasses.

Final acceptance assessment:

| Acceptance criterion | Status | Evidence |
|---|---|---|
| Every supported account/disciplinary change has time and actor | Verified | Writer matrix; USER review actors, SYSTEM strikes/flags/withdrawals/expiry; exact time and cause checks; honest legacy NULLs |
| Audit records append-only and faculty-scoped | Verified | Migration guards; restricted NOSUPERUSER mutation tests; historical faculty tests and query predicates |
| Suspension/reinstatement history remains available | Verified | Existing immutable decisions plus bounded historical read; repeated decisions, cancellation links and preserved evidence tests |
| Audit access follows role authorization | Verified | HTTP/live-cookie and direct stale-principal tests; program moves; reassignment/removal/suspension/closure; foreign/missing responses reveal no counts |
| Retention behavior documented | Verified | Category inventory, present storage behavior, privileged boundary, guarded rollback and unresolved U04/DPO approval |

Security review covered parameterized SQL, strict query/UUID/page validation,
explicit DTOs, no-store, no audit write routes, historical ownership, live role
checks, preserved private reasons, actor/cause separation, rollback on audit
failure, cancellation rollback, duplicate decisions/flags and concurrency.
The full existing policy/suspension suites still prove that administrative
cancellations do not generate participant strikes.

### Handoff / next agent

- **Assessment: Complete for Issue #26's supported backend workflows.** No required
  verification remains pending. Hosted CI/deployment is not claimed.
- Review migration 15 and the rollout paragraph before migrating the persistent
  version-14 development DB. The test runner never applied it there. Coordinate
  API/worker rollout; old violation writers are incompatible with the new metadata
  requirements. Down refuses to erase new audit evidence.
- Legacy absent actors/faculties/cause links remain unknown. Historical violation
  rows without authentic home-faculty context are intentionally not guessed into
  the new read endpoint. There is no retrospective repair claim.
- Existing historical coordinator/faculty FKs constrain reassignment; production
  runtime-role provisioning and privileged manual operations remain administrative
  responsibilities. Superusers can bypass trigger protections.
- U04 retention approval, archival/erasure, SSO, general account closure/role
  administration, automatic suspension and frontend dashboards are intentionally
  outside this task. Nothing here approves indefinite retention or GDPR compliance.
- Inspect this guide, migration 15, the Coordination audit reader, Identity read
  port, Tutoring eligibility port and `backend/tests/integration/audit_test.go`
  first. The branch, local `.env`, Compose services and prior milestone work were
  preserved; the developer can review and commit the changes manually.
