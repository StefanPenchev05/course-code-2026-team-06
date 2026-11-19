# Coordination: issue reports and attendance decisions

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
