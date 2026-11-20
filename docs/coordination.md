# Coordination: account administration, monitoring and session oversight

## Home-faculty flagged-user review (FR18 / UC12 / UC16 / NFR2, Issue #48)

`GET /api/v1/coordinator/flagged-users` is a read-only faculty inbox. It requires
the existing opaque session cookie and an active coordinator assignment. Identity
reloads current permissions at the middleware and again inside the response's
database snapshot. Missing, expired or revoked sessions return 401; student/tutor
accounts without coordinator authority, suspended/closed coordinators and removed
assignments return 403. An additional student/tutor role does not remove valid
coordinator authority or grant it independently.

```json
{"data":[{"flag_id":"00000000-0000-0000-0000-000000006901","user_id":"00000000-0000-0000-0000-000000006101","semester_id":"00000000-0000-0000-0000-000000005131","strike_count":3,"flagged_at":"2030-01-15T12:00:00Z","is_suspended":false}],"pagination":{"limit":25,"offset":0,"has_more":false}}
```

The response has exactly these six summary fields and `Cache-Control: no-store`.
It excludes names, email, student numbers, grades, transcripts, attendance evidence,
incident details and private suspension reasons. There is no faculty/user search
parameter, aggregate total or separate detail route. Existing
`GET /api/v1/coordinator/users/{id}/audit-history?category=FLAG` and `category=VIOLATION`
remain the authorized detail contracts; they retain their own historical event
scope and return the same empty page for missing and foreign history.

### Ownership, counts and current account state

- The **stored `user_semester_flags.faculty_id`** controls review visibility. A
  program change does not transfer the old flag to a new faculty. Coordinator
  permissions use their current assignment, not a stale cookie/principal faculty.
- Attendance decisions still belong to the **session's stored tutor faculty**.
  The affected participant's home faculty at the threshold receives the flag.
  Suspension/reinstatement still require the user's **current home faculty**.
  Historical flag access grants no additional sanction or attendance authority.
- `strike_count` is the **current total of authoritative `user_violations` rows
  for that user and session semester**, including incidents recorded after the
  flag. It is not the fixed pair of cause links captured by migration 15. A later
  program move can change an incident's home faculty without changing this total
  or transferring the old flag; individual incident details keep their own scope.
  Legacy qualifying violations count even when their actor/faculty metadata is
  unknown. No historical attribution is invented.
- Only confirmed no-shows and late participant cancellations enter that table.
  Pending/rejected allegations, early cancellations, coordinator policy actions
  and suspension cancellations do not count. Different semesters are independent;
  additional incidents never create a second flag for the same user/semester.
- `is_suspended` comes from current persisted Identity account state, not flag
  existence or historical decisions. Reinstatement leaves the flag available.
  Reading the inbox does not create flags, change sanctions or send notifications.

### Pagination, consistency and errors

Only `limit` (default 25, range 1–100) and `offset` (default 0, range 0–100000)
are supported. Unknown or repeated keys, malformed encodings and noninteger values
return 400; out-of-range integers return 422. Unsupported filters, including
`faculty_id`, `user_id` and `semester_id`, are rejected. No match returns 200 with
`data: []` and `has_more: false`; inaccessible IDs cannot be probed through this
collection route. Unsupported mutations return 405. Persistence failures return a
generic 500 without private SQL or account information.

Faculty scope is applied before `ORDER BY flagged_at DESC, id DESC`, `LIMIT` and
`OFFSET`. A bounded extra row determines `has_more`, without global counts. A
read-only repeatable-read transaction combines live coordinator authority, the
flag page, indexed semester counts and a single transaction-bound Identity batch
query for current suspension states. There are no per-user application queries.
Concurrent commits cannot mix counts and suspension state within one response.
Previously started reads may finish from their authorized snapshot; separate pages
can reflect different snapshots, so concurrent insertions can shift offset pages.

Existing indexes on `(faculty_id, flagged_at)` and `(user_id, semester_id)` support
the lookup. No migration, counter table, new dependency or writer change is needed.
Migration 15 remains the prerequisite for the existing milestone's protected audit
writers; this work does not inspect or migrate the persistent developer database.

### Windows verification and integration

From the repository root in Windows PowerShell with Docker Desktop Linux containers:

```powershell
# Rebuild runtime/test images; includes the new flag-review subtests plus #25/#26.
.\backend\tests\integration\test_docker.ps1 -SuspensionOnly
# Also includes flag review and related audit/booking/issue/policy regressions.
.\backend\tests\integration\test_docker.ps1 -AuditOnly
# Complete formatting, vet, race, build, PostgreSQL/Redis, SMTP and schema checks.
.\backend\tests\integration\test_docker.ps1
# Explicit plain package check after building the image.
docker run --rm tutti-backend-tests go test ./...
git diff --check
git status --short --branch
```

`flagged_users_test.go` extends `TestSuspensionAdministration`, so existing CI and
both targeted modes discover it without workflow or runner changes. Tests create
real cancellation/attendance incidents, mixed and post-threshold counts, both
cross-faculty directions, retained flags after program moves, two semesters,
suspension/reinstatement, tied timestamps, page boundaries, stale authority,
expired/revoked sessions, and snapshot consistency during concurrent commits.
They compare every public-schema table before/after read and rejected requests.

The runner uses disposable PostgreSQL 17/Redis, rebuilds the existing worker image
for local SMTP verification, and needs neither a running Compose worker nor real
university credentials. `.env`, host ports 5433/6379, Compose services and persistent
volumes remain untouched. No frontend, automatic sanctions, semester filter,
production-scale latency claim, retention approval or university SSO is included.
U04/DPO retention approval remains unresolved; see [audit history](audit-history.md).

Local verification on 2026-11-20 passed the targeted `-SuspensionOnly` run, the
final full Docker runner, and the explicit plain package check above. The full
runner passed all six flag-review scenarios, existing milestone regressions,
local SMTP/runtime worker and 33-table schema/legacy/rollback/reapply checks.
The new fixture's UUID/text parameter and its date overlap with another suite's
semester were corrected; flag review now uses a separate 2040 test period.
No production writer or constraint changed. Real university SMTP was deliberately
skipped, and package-only tests skip service cases; the full runner executes them.
Hosted GitHub Actions and developer review remain pending for these uncommitted
changes. The developer's persistent database was neither inspected nor migrated.

## Historical audit access (NFR12, Issue #26)

`GET /api/v1/coordinator/users/{id}/audit-history` provides bounded, read-only
SUSPENSION (default), VIOLATION or FLAG history. Live Identity authority and each
event's stored faculty determine visibility; changing the target's program does
not transfer historical records. Migration 15 protects evidence, records explicit
violation actors and SYSTEM flag basis, and preserves final report decisions.
Private account reasons stay separate from shared session cancellation text.

See [audit preservation](audit-history.md) for the complete API/coverage matrix,
legacy unknowns, append-only guarantees, retention/DPO dependencies, migration
rollout restrictions and exact Windows commands (`test_docker.ps1 -AuditOnly`).

## Suspension and reinstatement (FR19 / UC14, Issue #25)

The coordinator of a student's **current home faculty** can suspend or reinstate
that account. Home faculty comes from the locked student program through Catalog;
it does not come from a past session, a tutor's offering, or client input. This
workflow covers students and tutors, including accounts with an additional
coordinator role. A coordinator-only account has no student home faculty and is
outside UC14's target population. Missing and out-of-faculty targets both return
the same 404.

| Method and path (prefix `/api/v1`) | Behavior |
| --- | --- |
| `POST /coordinator/users/{id}/suspend` | Suspend an active, non-closed account and cancel all its future active sessions. |
| `POST /coordinator/users/{id}/reinstate` | Reverse a suspension without restoring earlier session or eligibility changes. |

Both routes require the existing authenticated cookie, current active coordinator
authority, same-origin header and `Content-Type: application/json`. The only input
is `{"reason":"The documented conditions for this decision have been reviewed."}`.
Reasons are trimmed and limited to **1–2000 UTF-8 bytes**. Blank/invalid UTF-8/NUL,
incorrect types, unknown fields, malformed/trailing JSON and bodies over 16 KiB
are rejected. Query parameters are not accepted. The backend supplies actor,
faculty, state, decision ID and time. Both application and database validate reasons.

Success returns **200**, with `Cache-Control: no-store`:

```json
{"data":{"user_id":"00000000-0000-0000-0000-000000005202","decision_id":"00000000-0000-0000-0000-000000005901","is_suspended":true,"changed_at":"2026-11-20T12:00:00Z","cancelled_sessions":4}}
```

The response contains no other participants, session IDs from other faculties,
grades, disciplinary evidence or private reasons. Errors use `{"error":"..."}`:
400 malformed JSON/query, 401 missing/expired login, 403 ineligible actor, 404
inaccessible target, 409 duplicate/closed-account transition or exhausted lock
contention, 413 oversized body, 415 wrong content type, 422 invalid UUID/reason,
and generic 500 for persistence failure. An already suspended target cannot be
suspended again; an unsuspended target cannot be reinstated. Closed accounts
cannot undergo either transition.

### Account access and reinstatement

Identity persists the existing `users.is_suspended` flag until an explicit reversal.
Every protected request reloads PostgreSQL account state; Redis holds only the
account ID under an opaque session key. A cookie issued before suspension therefore
loses access immediately on subsequent requests, across student, tutor and
coordinator roles. Login is also denied. Tutor discovery already excludes suspended
accounts; retained coordinator monitoring remains available for authorized oversight.

Redis keys are not revoked by this transaction. After reinstatement, an unexpired,
unrevoked cookie works with current permissions; expired/revoked sessions require
login. This avoids a second durable state change in Redis outside the PostgreSQL
transaction. Previously started reads can finish from their prior snapshot;
transactional writes recheck account/authority under their existing locks.

Reinstatement does not approve verification/applications, reactivate withdrawn or
academically ineligible offerings, reopen sessions, recreate reservations, erase
flags or change earlier evidence. It restores only access justified by independent
eligibility checks. Last suspension metadata is retained on the account; each
decision, including the new reinstatement reason, is appended to history.
Multi-role targets lose all roles, so reinstatement always requires a currently
active home-faculty coordinator. Flags never trigger suspension automatically.

### Future sessions, privacy and atomicity

“Future” follows Scheduling's upcoming-session boundary: **`starts_at >= decision
time`**, using one server UTC timestamp captured after authority/account locks.
Every `REQUESTED` or `ACCEPTED` session involving the target as student or tutor is
cancelled, including cross-faculty sessions and still-requested rows past their
expiration deadline. Already-started and terminal sessions remain unchanged.

Coordination composes Identity and Scheduling transaction-bound ports. One
transaction updates the account, appends `user_suspension_history`, queues the
account notice, cancels the entire eligible session set, releases both participant
reservations through existing triggers, and appends session history/outbox records.
Session pages contain at most 100 rows, ordered by UUID; paging continues until
none remain. There is no total cancellation cap and no background partial cleanup.
Timeout, cancellation, or any required write failure rolls the whole operation back.

Suspension cancellations have origin `SUSPENSION` and a foreign key
`session_status_history.suspension_history_id` to the account decision. They use the
generic shared reason `Session cancelled following an account suspension.` The
private administrative reason remains in account history; it is not copied to the
other participant's session, email, public profile or logs. The late flag is false:
these cancellations create no participant violation or semester flag.

One `USER_SUSPENDED` or `USER_REINSTATED` notice is queued for the target per
decision, including when there are no future sessions. Each cancelled session
queues the existing `SESSION_CANCELLED` notice for both participants. Account
deduplication uses the decision ID; session deduplication retains its existing key.
Notices are generic; a suspended user is directed to their home-faculty coordinator.
No SMTP connection is needed for the HTTP operation. Worker delivery remains
at least once, and queueing does not promise inbox delivery.

### Locking and verification

Identity locks actor and target accounts in UUID order using `FOR NO KEY UPDATE`.
The first lock may wait. The second uses `NOWAIT`, because existing tutor review
and attendance operations acquire participant/coordinator locks in different
orders. On contention, the transaction rolls back its locks and retries up to ten
attempts with bounded backoff (450 ms total delay); exhausted contention returns
409 with no partial effects. Self-targeting uses one account lock and still requires
the same home-faculty rule.

It then locks/rechecks the coordinator assignment and target student row, and
locks the current program for faculty resolution. Scheduling locks sessions in UUID
order without `SKIP LOCKED` and never acquires another participant account lock.
Bookings and participant decisions use the same target account lock: a prior
committed booking is included; a booking after suspension rechecks eligibility and
fails. Policy cancellation, expiration and completion serialize on session/account
locks and cannot overwrite an already committed terminal state. No coordinator
gets read access to another faculty's session details through this operation.

From the repository root in Windows PowerShell with Docker Desktop Linux containers:

```powershell
# Build existing runtime/test images and run isolated PostgreSQL/Redis suspension tests.
.\backend\tests\integration\test_docker.ps1 -SuspensionOnly
# Full formatting, vet, race, build, regression, local SMTP worker and migration checks.
.\backend\tests\integration\test_docker.ps1
# Plain package tests after the runner has built the test image.
docker run --rm tutti-backend-tests go test ./...
```

Use one targeted switch at a time. Existing targeted modes remain available. The
runner preserves `.env`, Compose host ports and development volumes, needs no
running development worker or live university credentials, and cleans up only its
disposable test containers/network. `TestSuspensionAdministration` is included by
the existing CI integration command. It covers stale cookies, cross-faculty and
authority races, both roles, exact time boundaries, 105-session pagination,
audit/outbox failure, cancelled requests, independent reinstatement eligibility,
preserved evidence and competing lifecycle actions.

Apply **migration 14 before updated API and worker binaries**; their shared session
event writer now references `suspension_history_id`. With the intended database URL
already loaded and `migrate` installed:

```powershell
migrate -path .\backend\migrations -database $env:DATABASE_URL up
```

Tests do not migrate the developer database. See the [database contract](database/implementation.md#suspension-administration-transaction-contract)
for legacy compatibility and rollback guards. Existing historical coordinator
foreign keys can prevent reassignment/removal after decisions; this issue preserves
those references. Large transactions remain subject to the API deadline; no
production-scale latency guarantee is implied. Frontend, SSO, live university SMTP,
automatic sanctions and account deletion remain outside Issue #25.

## Tutor monitoring and faculty statistics (FR16 / UC12, Issue #23)

Monitoring is a read-only Coordination workflow composed from Identity, Tutoring,
Scheduling and Feedback query ports. It uses the existing session cookie and current
coordinator authorization. A multi-role account retains coordinator authority
independently of student verification. Suspended/closed accounts, expired sessions
and removed assignments are denied. Responses use `Cache-Control: no-store`.

| Method and path (prefix `/api/v1`) | Result |
| --- | --- |
| `GET /coordinator/tutors` | Current faculty's approved tutor profiles with activity/rating summaries. Optional `program_id` and `subject_id` support UC12 drill-down. |
| `GET /coordinator/tutors/{id}` | One profile and activity summary, plus paginated approved offerings. |
| `GET /coordinator/tutors/{id}/schedule?from=...&to=...` | Published availability and sessions intersecting the requested interval. |
| `GET /coordinator/statistics` | Faculty tutor counts and session-derived activity/rating totals. |
| `GET /coordinator/tutors/{id}/feedback` | Existing [Feedback endpoint](feedback.md), reused for private comments and ratings. |

### Faculty authority and tutor population

Identity resolves the active coordinator's assignment again inside a PostgreSQL
`REPEATABLE READ READ ONLY` transaction. Every query in one response uses that
transaction and trusted faculty. Client `faculty_id` parameters are rejected.
Tutor list/detail/schedule authorization uses the tutor's **current program faculty**.
SQL applies scope before pagination; missing and inaccessible tutors both return
404. Programs/subjects with no matching own-faculty tutors produce an empty list.

Approved profiles remain visible after deactivation, suspension, closure, approval
end, or loss of eligible offerings, for oversight of retained activity.
`approved_tutors` counts these profiles, not applications or offerings. A pending
application alone never creates an entry. `active` / `active_tutors` additionally
require an active profile without approval end, an active account, latest approved
student verification, and at least one active approved offering belonging to the
current program. Tutoring's existing eligibility predicates supply these rules.
A profile without offerings remains visible with `active: false`.

Session entries, teaching totals and ratings use **the immutable faculty stored on
each session**, never the student's home faculty or a recomputed tutor faculty.
When a tutor moves from A to B, A loses their profile/schedule view; B sees the
current profile and published availability, but no A-owned sessions or feedback.
A retains its historical faculty totals and existing authorized session-feedback
route. Faculty activity therefore need not equal the sum of the currently listed
tutors' activity. Availability has no stored faculty; its visibility follows current
tutor membership. Home-faculty disciplinary flags remain a separate workflow.

Authority and data are consistent **as of the response's snapshot**. A reassignment,
program change or correction committed after it appears on the next request.
Separate endpoints/pages use separate snapshots; concurrent changes can shift offset
pages. Monitoring reads take no business row locks and perform no writes.

### Response and aggregate contract

Lists and schedules return `{"data":[...],"pagination":{"limit":25,"offset":0,"has_more":false}}`.
Pagination is `limit` 1–100 (default 25), `offset` 0–100000 (default 0). Detail applies
these parameters to `data.offerings`, with `offerings_pagination` beside `data`;
profile/activity totals are independent of that page. Statistics accepts no query
parameters. Empty lists are `[]`.

A tutor list item (also the detail fields before `offerings`) looks like:

```json
{"id":"00000000-0000-0000-0000-000000003202","name":"Tutor display name","program_id":"00000000-0000-0000-0000-000000003111","program_name":"Program name","faculty_id":"00000000-0000-0000-0000-000000003101","approved_at":"2026-11-19T12:00:00Z","active":true,"completed_sessions":3,"hours_taught":4.5,"average_rating":5,"rating_count":2}
```

Detail offerings use Tutoring's safe `id`, `subject_id`, `subject_name`, `description`
and `active` fields. Offering `active` means the offering itself is active and belongs
to the current program; tutor-wide eligibility is the profile's `active` field.
Tutors sort by display name then ID; offerings by subject name then ID. The subject
filter matches approved offerings, including retained inactive offerings.

Statistics returns `data` with `faculty_id`, `approved_tutors`, `active_tutors`,
`completed_sessions`, `hours_taught`, `average_rating`, and `rating_count`. Activity
is all-time within stored session faculty. Tutor and faculty queries reuse Feedback's
SQL calculations:

- Only currently `COMPLETED` sessions count. Hours sum scheduled duration minutes
  and divide by 60, including unrated completed sessions.
- Ratings count qualifying feedback rows. Average rounds to two decimal places and
  is `null` when none exist. Faculty averages use individual ratings, not rounded
  tutor averages. Empty totals/counts are zero.
- Confirmed no-shows remove contributions on subsequent reads while preserving
  historical feedback. Pending allegations have no effect.

### Schedule and privacy contract

`from` and `to` are required RFC 3339 timestamps with offsets. `to` must be later,
with at most **31 elapsed days** between them. The query returns original bounds of
entries overlapping `[from,to)`, ordered by start, kind, ID. Output times are UTC.
Historical and future ranges are supported.

`kind: "AVAILABILITY"` contains slot `id`, `starts_at`, `ends_at`, and `mode`;
recurring slots also include `rule_id`, `time_zone`, and `semester_id`. These are
materialized published windows, **not free intervals**. Existing Scheduling creates
them using semester, IANA-zone/DST and exception rules. Excluded occurrences are
absent. One-off/detached slots have no recurrence metadata. Raw exception reasons
are not exposed; no second recurrence engine is introduced.

`kind: "SESSION"` contains session `id`, bounds, `mode`, current `status`, subject
ID/name and semester ID. All statuses remain distinct: requested, accepted,
completed, cancelled, declined, expired and no-show. A window can overlap a session
in the response; never add availability to teaching activity.

Explicit SQL/DTOs exclude grades, transcripts, qualification evidence, student
identities/contact details, session descriptions, meeting secrets and cancellation
evidence. Comments appear only through Feedback routes. Logs contain route/status
metadata only. Errors use `{"error":"..."}`: 400 malformed/duplicate/unknown query
parameters, 401 missing/expired login, 403 ineligible coordinator, 404 inaccessible
tutor, 422 invalid identifier/filter/bounds/range, or generic 500 for internal failure.

### Verification and limitations

From the repository root in Windows PowerShell, with Docker Desktop Linux containers:

```powershell
# Build existing runtime/test images; run isolated PostgreSQL/Redis monitoring tests.
.\backend\tests\integration\test_docker.ps1 -MonitoringOnly
# Full formatting, vet, race, build, integration and migration regression checks.
.\backend\tests\integration\test_docker.ps1
```

The runner preserves `.env`, Compose services, host ports and volumes. It needs no
running worker, live SMTP or SSO credentials. Existing `-IssuesOnly` and
`-NotificationsOnly` modes remain available; select one targeted mode at a time.
`TestCoordinatorMonitoring` is automatically included in the existing CI integration
command. It exercises current/stale authority, faculty changes, weighted ratings,
fractional hours, multiple offerings, real recurrence exceptions/DST, attendance
correction, privacy, pagination and consistent snapshots.

No migration, dashboard counter table, cache or worker job is added. Existing
program/student, tutor/start, faculty/status/start and feedback primary-key indexes
support the queries. Lists batch activity lookups; schedules and offerings are
bounded. Faculty totals still aggregate qualifying history; production-scale load
testing remains separate. Frontend, SSO, sanctions, sponsor exports, upcoming-count
analytics and new SMTP work are outside this issue.

## Policy-based session cancellation (FR19 / UC04, Issue #24)

Coordination owns the authorized action; Scheduling owns the session transition,
reservation triggers, history and lifecycle outbox writes. Send the existing session
cookie, same-origin header and `Content-Type: application/json` to:

```http
POST /api/v1/coordinator/sessions/{id}/cancel

{"reason":"This session does not comply with faculty tutoring policy."}
```

Only an active coordinator assigned to the session's **stored tutor faculty** may
act. Identity locks and rechecks the current assignment and account inside the
transaction. The student's home faculty and the tutor's later program changes do
not transfer session authority. A coordinator role is independent of student
verification on a multi-role account. The route records coordinator capacity even
when that coordinator is also a participant; the participant cancellation route
continues to apply its existing participant rules.

Only `REQUESTED` and `ACCEPTED` may become `CANCELLED`. This reuses Scheduling's
existing cancellation timing: there is no additional policy cutoff. An overdue
request remains cancellable until expiration wins; an accepted session remains
cancellable after its end until completion or another terminal transition wins.
`CANCELLED`, `DECLINED`, `EXPIRED`, `COMPLETED` and `NO_SHOW` return 409, including
repeated policy cancellation. The operation never revives or deletes a session.

`reason` is required, trimmed, and limited to 1–2000 UTF-8 bytes after trimming.
Blank text, NUL and invalid UTF-8 are rejected. The 16 KiB JSON limit and strict
unknown-field/trailing-value checks apply. No query parameters are accepted;
clients cannot supply faculty, actor, timestamps, status or cancellation origin.

Success is **200**, with `Cache-Control: no-store` and this response shape:

```json
{"data":{"id":"00000000-0000-0000-0000-000000001901","status":"CANCELLED","starts_at":"2026-12-07T14:00:00Z","ends_at":"2026-12-07T15:00:00Z","cancelled_at":"2026-12-06T16:00:00Z","cancelled_by_user_id":"00000000-0000-0000-0000-000000001806","cancellation_reason":"This session does not comply with faculty tutoring policy.","cancellation_origin":"COORDINATOR_POLICY","is_late_cancellation":false}}
```

The DTO excludes participant identities, descriptions, meeting secrets, grades and
private feedback. Errors use `{"error":"..."}`: 400 malformed JSON/query, 401 missing
or expired login, 403 inactive/non-coordinator, identical 404 for missing and
wrong-faculty sessions, 409 lifecycle conflict, 413 oversized body, 415 incorrect
content type, 422 invalid UUID/reason, and generic 500 on persistence failure.

### Atomicity, audit and disciplinary isolation

The lock order is current coordinator assignment/account (`FOR SHARE`), then the
faculty-scoped session (`FOR UPDATE`). This path needs no participant eligibility
or disciplinary reads, so it takes no participant account locks after the session.
Competing participant decisions, completion and expiration serialize on the same
session row and recheck its status before writing.

One transaction updates cancellation metadata, releases **both** participant
reservations through existing PostgreSQL triggers, appends status history, and
queues a deduplicated `SESSION_CANCELLED` notification for each participant. A failed
history or either outbox insert rolls everything back. Successful cancellation
retains the original session and earlier history; released intervals can be booked
again subject to ordinary availability and overlap constraints.

Policy cancellation always records `is_late_cancellation: false`, even within
24 hours or after the scheduled start. It creates no violation, semester flag,
suspension or reinstatement. Prior disciplinary evidence remains unchanged.
Migration 13 records `COORDINATOR_POLICY` in the new history `cancellation_origin`
column; new participant cancellations record `PARTICIPANT`. Other events and legacy
cancellations retain `null`, meaning unknown/not applicable. No actor capacity is
inferred from mutable coordinator membership. History retains actor kind `USER`,
actor ID, previous/new states, reason and the same server timestamp as the session.
No general history API is introduced.

Both participants receive the existing generic cancellation email with instructions
to sign in; the reason is available through their existing authenticated session
detail. This interprets UC04's “notified with the reason” through authenticated
access, preserving the lifecycle email privacy convention. Policy evidence is not
included in email bodies or logs. Outbox insertion means queued delivery, not inbox
delivery; the existing worker retains at-least-once SMTP semantics.

### Windows verification and migration order

From the repository root in Windows PowerShell with Docker Desktop Linux containers:

```powershell
# Rebuild existing runtime/test images and run isolated PostgreSQL/Redis policy tests.
.\backend\tests\integration\test_docker.ps1 -PolicyCancellationOnly
# Full formatting, vet, race, build, API, local SMTP worker and migration checks.
.\backend\tests\integration\test_docker.ps1
# Plain package tests using the image built by either runner invocation.
docker run --rm tutti-backend-tests go test ./...
```

The targeted mode is mutually exclusive with `-IssuesOnly`, `-MonitoringOnly` and
`-NotificationsOnly`. Tests create disposable databases/containers, preserve local
`.env` and Compose volumes, and need no running development worker or university
credentials. `TestPolicyCancellation` also runs under the existing CI integration
command. Coverage includes role/faculty and stale-authority cases, strict input,
all terminal statuses, release/rebooking, history/outbox failures, multi-role
capacity, and races against duplicate cancellation, acceptance, participant
cancellation, completion and expiration.

Apply **migration 13 before starting updated API or worker binaries**, because the
shared session-event writer now inserts the new history column. With the intended
development database URL already loaded into the current PowerShell session and
the `migrate` CLI installed:

```powershell
migrate -path .\backend\migrations -database $env:DATABASE_URL up
```

The integration runner applies migrations only to its disposable databases; it
does not migrate the developer's database. Migration 13 rollback refuses to erase
any recorded cancellation origin. Do not delete history or bypass that guard to
force rollback. See the [database contract](database/implementation.md#policy-cancellation-transaction-contract)
for compatibility and retention details. Existing [worker commands](workers.md)
remain applicable; no SMTP or Compose configuration changes are required.

Frontend, suspension/reinstatement, automatic policy detection, a policy-rule
engine, SSO and real university delivery are outside Issue #24.

## General issue reporting (FR10 / UC06)

Coordination owns general issue creation. `POST /api/v1/issues` requires an authenticated, active, verified student; tutors are also students. This follows the existing verified-participant policy used by attendance and feedback. A user who is both a student and a coordinator can report as a student. Coordinator-only, unverified, suspended, and closed accounts cannot submit. The existing Identity cookie and same-origin protections apply.

Send `Content-Type: application/json` with only these fields:

```json
{"description":"I need help with a tutoring-related issue.","session_id":"00000000-0000-0000-0000-000000001901"}
```

`description` is required, trimmed, and must contain 1–2000 UTF-8 bytes after trimming, matching Coordination's existing text limit. Unicode is supported; whitespace-only text, NUL, invalid UTF-8, and oversized descriptions are rejected. The entire request is limited to 16 KiB. Unknown fields, malformed JSON, multiple JSON values, and invalid field types are rejected. In particular, callers cannot supply reporter, faculty, reviewer, status, or timestamp fields.

`session_id` may be omitted or `null`. When present it must be a nonempty UUID for a session in which the caller is either the student or tutor. General issues can reference any session status and have no attendance reporting window. Missing and unrelated sessions return the same 404 response.

Faculty ownership follows the approved schema's existing issue rules:

- A linked report belongs to the session's stored tutor faculty, including when the reporting student has a different home faculty or the reporter teaches other sessions.
- An unlinked report belongs to the reporter's current program faculty. Identity stores one current program per student, so faculty ownership is unambiguous.
- The backend derives both reporter and faculty. It stores faculty ownership without choosing an individual coordinator or requiring that one has already been provisioned. Missing valid academic membership prevents submission.

Successful creation returns **201** with `Cache-Control: no-store`:

```json
{"data":{"id":"00000000-0000-0000-0000-000000002001","reporter_user_id":"00000000-0000-0000-0000-000000001801","faculty_id":"00000000-0000-0000-0000-000000001701","session_id":"00000000-0000-0000-0000-000000001901","description":"I need help with a tutoring-related issue.","status":"OPEN","created_at":"2026-11-19T12:00:00Z"}}
```

The response omits `session_id` for an unlinked report. Errors use `{"error":"..."}`: 400 malformed JSON or unsupported fields, 401 missing/expired authentication, 403 ineligible reporter or invalid origin, 404 inaccessible session, 409 persistence conflict, 413 request too large, 415 unsupported content type, 422 invalid description/session UUID, or 500 generic internal failure. Logs contain route/status metadata, never the description, database error text, or credentials.

### Issue transaction and history

The application uses explicit transaction-bound Identity, Catalog, and Scheduling capabilities wired in bootstrap. It locks the reporter's account and student membership, rereads current eligibility, and resolves the current program faculty or locks the authorized participant session. A concurrent program change or suspension therefore cannot bypass the recheck. Scheduling keeps session participants and faculty immutable. The issue insert and initial history insert share one PostgreSQL transaction; audit failure rolls both back.

Migration `000011` adds `issue_status_history` to the existing `issue_reports` table's workflow. Each new API report has one creation event (`NULL` → `OPEN`), its authenticated actor, and the exact persisted creation timestamp. Foreign keys retain the parent report, a partial unique index prevents duplicate creation events, and triggers reject history updates, deletes, and truncation. No report edit/delete or audit mutation endpoint is exposed. Repeating POST creates a separate report with its own history; this endpoint does not promise request deduplication.

Existing reports are retained without synthetic history. Migration 11 introduced a `NOT VALID` description bound. Migration 12 moves that validation to inserts and actual description changes, allowing resolution to retain legacy evidence that exceeds today's limit. See [migration and retention details](database/implementation.md#issue-creation-transaction-contract).

## Coordinator issue inbox and resolution (FR17 / UC13, Issue #22)

General reports have the existing lifecycle `OPEN` → `RESOLVED`. These routes require an authenticated, active coordinator. Coordinator authority is independent of student verification: a coordinator who also has a pending student role retains coordinator access. Student-only and tutor-only users cannot use these routes. Closed/suspended accounts and removed coordinator assignments are denied by Identity.

| Method and path (prefix `/api/v1`) | Result |
| --- | --- |
| `GET /coordinator/issues` | Faculty-scoped inbox, default status `OPEN`. |
| `GET /coordinator/issues/{id}` | Own-faculty report, including stored resolution information. |
| `POST /coordinator/issues/{id}/resolve` | One-time resolution with a required note; returns 200. Requires the configured same-origin header. |

Every query joins the caller's current coordinator assignment and active account to the report's **stored faculty**. Ownership is not recomputed from the reporter's current program. A caller cannot select another faculty via a query parameter or request body. Missing and other-faculty reports return the same 404 error. Detail and inbox reads are read-only and use `Cache-Control: no-store`.

The inbox supports `status=OPEN|RESOLVED`, `limit` (1–100, default 25), and `offset` (0–100000, default 0). Results order by `created_at` ascending, then `id` ascending. Unknown/duplicate query parameters and malformed integers return 400; unsupported status or out-of-range pagination returns 422. An empty inbox returns `data: []`. The response uses the existing pagination contract:

```json
{"data":[],"pagination":{"limit":25,"offset":0,"has_more":false}}
```

Use `?status=RESOLVED` to review historical reports. Offset pagination provides deterministic ordering within each query, not a snapshot across requests; concurrent inserts or resolutions can shift later pages. Responses contain the existing report fields and, when resolved, `handled_by_coordinator_id`, `resolved_at`, and `resolution_note`. Single-report responses use the `data` envelope. No unrelated account fields, credentials, grades, or private feedback are joined into these DTOs. Audit events remain in PostgreSQL; no separate history endpoint is exposed.

Resolve with only:

```json
{"resolution_note":"I reviewed the evidence with the participants and documented the agreed outcome."}
```

Notes use the same trimming, Unicode, NUL rejection, and 1–2000 UTF-8 byte limit as descriptions. The request retains the 16 KiB body cap and strict JSON decoding. Missing/blank/oversized notes return 422; malformed JSON, raw invalid UTF-8, multiple JSON values, invalid field types, and unsupported fields return 400. Oversized bodies return 413; unsupported content types return 415. Identity determines the resolver, PostgreSQL supplies the timestamp, and the backend controls status. Supplied resolver, faculty, status, timestamp, or actor fields are rejected.

Resolution takes Identity's transaction-bound `LockCoordinatorFaculty` lock before locking the faculty-scoped issue row. It rechecks `OPEN`, conditionally updates only status/resolver/note/time, and appends an `OPEN` → `RESOLVED` event in `issue_status_history` with the exact stored resolver and resolution time. Original reporter, faculty, session, description, creation time, and creation history remain unchanged. The history's report FK references the retained resolution note. History insertion failure or transaction cancellation rolls the update back. Repeated or losing concurrent requests return 409 without overwriting the winning decision or appending another event.

The existing schema permits one coordinator per faculty, so competing authorized requests are tested from that coordinator. Reassignment, removal, suspension, and closure are rechecked under Identity's locks. Existing historical reviewer foreign keys can prevent later coordinator reassignment/deletion; this milestone preserves those constraints and does not add an account-management workflow.

Legacy reports without creation history can be listed, inspected and resolved. Resolution appends only the authentic new event. Migration `000012` preserves legacy descriptions, adds a database bound for resolution notes, and reuses the existing faculty/status/time index. Apply migration 12 before deploying this API. Its down migration preserves reports/history and restores migration 11's validation behavior, which again prevents updating legacy oversized descriptions.

General issue resolution does not change session outcomes, create attendance reports/violations/flags, suspend users, or queue attendance emails. Reopening, editing/deletion, resolution revisions, notifications, a coordinator dashboard, and student-facing report queries remain outside Issue #22. The current Identity session implementation is reused; the separate approved SSO migration remains outside this task.

## Attendance API

Scheduling records a tutor's explicit completion of an accepted session. An attendance report is a separate allegation by either participant about the other. A coordinator's confirmation corrects the session outcome if necessary and creates a disciplinary violation for the absent participant. A rejection records the decision and preserves the session outcome.

All paths start with `/api/v1`. Requests require an Identity session, and writes require the configured same-origin header. An authenticated, verified participant can submit evidence. Only the coordinator assigned to the session's tutor faculty can read or decide evidence. This assignment is checked in PostgreSQL using the session's stored faculty and the coordinator's current assignment.

| Method and path | Result |
| --- | --- |
| `POST /sessions/{id}/no-show-reports` | Creates a pending report. Returns 201. |
| `GET /coordinator/attendance-reports` | Lists the coordinator's faculty reports. |
| `GET /coordinator/attendance-reports/{id}` | Returns the report and its evidence for that faculty. |
| `POST /coordinator/attendance-reports/{id}/confirm` | Records confirmation and returns the reviewed report. |
| `POST /coordinator/attendance-reports/{id}/reject` | Records rejection and returns the reviewed report. |

Submit a description:

```json
{"description":"The other participant did not attend the meeting."}
```

For either decision, send a resolution note:

```json
{"resolution_note":"I reviewed the attendance evidence provided by both participants."}
```

A single report response is wrapped in `data` and includes its ID, session ID, reporter and absent participant IDs, status, description, and reporting timestamp. After review it also includes the reviewer ID, review timestamp, and resolution note. The list defaults to pending reports and supports `status=PENDING|CONFIRMED|REJECTED`, `limit` (1–100, default 25), and `offset` (0–100000). Results sort by reporting time then ID and use pagination metadata with `has_more`.

A missing session returns 401. A user without the required role receives 403. A resource outside the caller's permitted scope returns 404. A duplicate submission, repeated decision, or incompatible session outcome returns 409. Invalid evidence, notes, or reporting times return 422.

## Reporting and adjudication

Reporting is open from the scheduled end through exactly 168 elapsed hours after it, inclusive. The session must be `ACCEPTED` or `COMPLETED` at submission. The server derives the absent participant from the session and its authenticated reporter. PostgreSQL allows one report per reporter and session. Submission leaves the existing session status and violation count intact.

Opposing allegations remain separate. The coordinator can decide each on its own evidence. When the first confirmation changes `ACCEPTED` or `COMPLETED` to `NO_SHOW`, Scheduling writes the correction with the deciding coordinator, time, and resolution note. If a second pending report is later confirmed, it concerns the other participant. It can create that participant's own violation without writing another session transition. The original completion timestamp and history remain for audit. A rejection never changes the session outcome. A report pending when a session is cancelled can be rejected, while confirmation returns a conflict. A pre-existing violation against the same person for the same session also causes confirmation to roll back.

A confirmed no-show counts in the session's semester. Coordination shares the late-cancellation threshold logic and serializes incident counting with the reported participant's Identity write lock. Two countable violations create one flag for that person's current home faculty. The home faculty coordinator receives the flag alert, even when a different faculty reviewed the case. Neither confirmation nor a flag suspends the user.

The [Feedback module](feedback.md) derives completed-session counts, hours taught, and ratings from sessions whose current status is `COMPLETED`. A correction drops the session out of those aggregates. Its feedback remains available to the authorized session coordinator.

## Transactions and notifications

Submission and review lock the Scheduling session row. Confirmation locks the absent user's account first, then the session and attendance report. Completion also locks the actor's account before the session. When actions race, the transaction rechecks the state under lock. The attendance decision, any session outcome correction, violation, flag, history, and queued email writes commit together. Database constraints enforce report uniqueness, reviewer faculty, and the link from a no-show violation to its confirmed report.

Migration `000010` adds the `ATTENDANCE_REPORTED` and `ATTENDANCE_DECIDED` outbox types. Submission queues a notice to the session faculty coordinator when one is assigned. Decisions queue a notice to each participant, while threshold alerts use the existing flag notification. Notice bodies omit private evidence and resolution notes. The existing [worker](workers.md) handles delivery. A rollback of migration 10 refuses to convert the notification enum while attendance email rows are retained.

## Tests and remaining work

From the repository root:

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

On Windows PowerShell with Docker Desktop using Linux containers, run from the repository root (no local Go, database clients, `.env` changes, or SMTP credentials needed):

```powershell
# Rebuild and run issue creation plus coordinator inbox/resolution authorization tests.
.\backend\tests\integration\test_docker.ps1 -IssuesOnly

# Full formatting, vet, race, build, real-service regressions and migration checks.
.\backend\tests\integration\test_docker.ps1
```

Both commands build the existing runtime and test targets and create isolated, disposable PostgreSQL/Redis test containers. The targeted issue run starts no worker. The full regression run exercises notifications using its local SMTP fixture. Neither command uses the developer's Compose database, changes its port, or removes its volumes. `-IssuesOnly` and `-NotificationsOnly` are mutually exclusive.

`TestIssueReporting` includes `coordinator_inbox_and_resolution` as well as the original creation regression tests. The coordinator suite checks multiple/empty faculty inboxes, stable pagination, historical detail, required notes and strict JSON, role and cross-faculty denials, stored ownership after a reporter moves, legacy evidence, failed audit/update rollback, one winning concurrent resolution, and lock-controlled coordinator reassignment/removal/suspension/closure and cancellation. It also verifies that general issue handling leaves scheduling, discipline and notifications unchanged. The schema suite tests actual legacy-data upgrade through migrations 11 and 12, migration 12 rollback without data loss, constraints, complete rollback, and reapplication. Existing attendance tests cover concurrent completion/reporting, conflicting decisions, faculty isolation, threshold routing, and rollback after failed audit writes.
