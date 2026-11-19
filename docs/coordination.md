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

Existing reports are retained without synthetic history. The new description bound is `NOT VALID` to preserve legacy descriptions; it still enforces every new insert or update. See [migration and retention details](database/implementation.md#issue-creation-transaction-contract).

This milestone implements creation and durable faculty ownership. Coordinator listing, reading, resolution, a dashboard, notifications, and suspension workflows are outside this slice. No issue query endpoint is published, including for coordinators. Future query/review capabilities must enforce stored faculty scope on the server and append history in the same transaction as each transition. General reports do not automatically create attendance violations or change session outcomes.

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
# Rebuild using the existing Dockerfile and run issue authorization tests only.
.\backend\tests\integration\test_docker.ps1 -IssuesOnly

# Full formatting, vet, race, build, real-service regressions and migration checks.
.\backend\tests\integration\test_docker.ps1
```

Both commands build the existing runtime and test targets and create isolated, disposable PostgreSQL/Redis test containers. The targeted issue run starts no worker. The full regression run exercises notifications using its local SMTP fixture. Neither command uses the developer's Compose database, changes its port, or removes its volumes. `-IssuesOnly` and `-NotificationsOnly` are mutually exclusive.

`TestIssueReporting` authenticates real users from two faculties (plus a faculty without a coordinator), exercises student/tutor and cross-faculty links, tests unknown/missing session privacy, strict input bounds, injected audit failure, append-only history, concurrent submissions, and lock-controlled program/suspension races. Unit tests cover all session statuses, stale roles, missing faculty, and storage failures. The schema suite verifies upgrade with legacy report data, integrity constraints, complete rollback, and reapplication. Existing attendance tests cover concurrent completion/reporting, conflicting decisions, faculty isolation, threshold routing, and rollback after failed audit writes.
