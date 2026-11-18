# Coordination: attendance and no-show decisions

Scheduling records a tutor's explicit completion of an accepted session. An attendance report is a separate allegation by either participant about the other. A coordinator's confirmation corrects the session outcome if necessary and creates a disciplinary violation for the absent participant. A rejection records the decision and preserves the session outcome.

## Attendance API

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

The PostgreSQL suite covers concurrent completion and reporting, conflicting decisions, faculty isolation, threshold routing, and rollback after failed audit writes. General issue reports and coordinator suspension or reinstatement remain separate features.
