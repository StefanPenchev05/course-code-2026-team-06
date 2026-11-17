# Scheduling

## Stage 1: availability

Tutoring availability belongs to the tutor's account and can serve any of their approved subject offerings. A **rule** defines one recurring local weekday and clock interval for a semester. A **slot** is a concrete published time window, either generated from a rule or entered as a one-off. An **exception** records a skipped local date for one rule. A **reservation** is the time held by a requested or accepted session. Published windows can include reserved time.

The [Scheduling module](../backend/internal/scheduling) puts recurrence and reconciliation in its domain, authorization and transaction workflows in its application service, and persistence in its PostgreSQL adapter. Bootstrap uses the existing connection pool, router, and Identity session and origin checks. Scheduling uses Tutoring's availability access interface and a transaction-bound Identity student lock to recheck tutor permissions before each write.

## Recurrence and time zones

Rules use ISO weekdays (Monday = 1), same-day local start and end times, an IANA zone, and `ONLINE`, `ONSITE`, or `BOTH`. Creation and editing materialize future occurrences through the configured semester end. An occurrence must start within the semester and finish by its end. Windows that straddle a semester boundary are skipped. Tutors set up the schedule for the next semester separately.

The generator iterates calendar dates in the rule's zone and resolves each local endpoint independently. Absolute timestamps can shift across daylight-saving changes while the local teaching hour stays the same. If an affected start or end has no corresponding instant, or corresponds to two instants, the mutation returns 422 with the local date and zone. A window crossing a clock change with two unambiguous endpoints uses those instants, so its elapsed duration can differ from its wall-clock duration. The API binary includes Go's IANA time-zone data for runners without a system zone database. There is no background materialization worker.

Edits retain the IDs of future slots whose bounds and mode stay the same. Existing exceptions remain attached to their local dates when a rule is edited. Deleting a future generated slot adds a matching exception in the same transaction. Restoring an exception rematerializes that date under the current rule, provided it is a valid future occurrence and fits without overlap. A failed restore keeps the exception. A one-off window can replace a skipped occurrence. One-off windows use explicit timestamps and must fit inside one configured semester.

## API

All routes are relative to `/api/v1/tutors`. They require an authenticated session. Writes require the existing same-origin check.

| Method and path | Behavior |
| --- | --- |
| `GET /me/availability/rules` | List owned rules. |
| `POST /me/availability/rules` | Create a rule and its slots. Returns 201. |
| `PATCH /me/availability/rules/{id}` | Update weekday, local times, zone, or mode. Returns the updated rule. |
| `DELETE /me/availability/rules/{id}` | Delete a rule and its removable future slots. Returns 204. |
| `GET /me/availability/rules/{id}/exceptions` | List skipped local dates. |
| `DELETE /me/availability/rules/{id}/exceptions/{date}` | Restore a skipped date in `YYYY-MM-DD` format. Returns 204. |
| `POST /me/availability/slots` | Create a one-off window. Returns 201. |
| `DELETE /me/availability/slots/{id}` | Remove a future slot and record an exception if recurring. Returns 204. |
| `GET /me/availability` | Tutor view of upcoming published windows, including reserved portions. |
| `GET /{id}/availability` | Verified student view of free intervals for a tutor visible through Tutoring. |

All `/me` routes require a verified student with an active tutor profile. Tutor identity comes from the session. A rule or slot owned by someone else returns 404. An overlapping window or a modification that would remove a window backing an active session returns 409. Invalid scheduling values and dates return 422.

Example recurring rule request:

```json
{
  "semester_id": "00000000-0000-0000-0000-000000000304",
  "iso_weekday": 1,
  "local_start_time": "14:00",
  "local_end_time": "17:00",
  "time_zone": "Europe/Luxembourg",
  "allowed_mode": "BOTH"
}
```

Example one-off replacement:

```json
{
  "starts_at": "2026-12-07T18:00:00+01:00",
  "ends_at": "2026-12-07T20:00:00+01:00",
  "allowed_mode": "ONSITE"
}
```

Lists return `{"data":[...],"pagination":{"limit":25,"offset":0,"has_more":false}}`, with a maximum page size of 100. Slot lists sort by start and ID and accept `from` (inclusive lower bound) and `to` (exclusive upper bound) as RFC 3339 timestamps or UTC dates. The student view clips intervals to the requested range and subtracts active participant blocks for the tutor in either role. A slot can yield multiple free fragments with the same source slot ID. No reservation identities are returned. The owner view returns the original bounds of published windows that intersect the requested range.

## Deletion and concurrency

The app serializes writes per tutor using the existing Identity user row lock. A rule mutation and its slot and exception changes share one transaction. PostgreSQL's existing GiST exclusion constraint remains the final overlap guard and permits adjacent ranges. Rule updates replace changed future slots and leave unchanged slots in place. Since published slot boundaries and mode are immutable in the schema, a booked slot keeps its original ID, bounds, and mode until its dependent session is resolved.

Deleting a rule physically deletes its removable future slots and exceptions. Past and already-started slots are retained with the rule reference cleared. Individual deletion also applies only to future slots. When a slot is removed, PostgreSQL clears that slot reference for terminal sessions and preserves their recorded times. An active session requires a non-null slot reference, so an attempted removal rolls back atomically.

A concurrent session insert takes a share lock on its source slot through the existing booking trigger. An availability delete therefore waits or wins before the insert gets the source slot. The existing active-session check prevents both operations from committing when they conflict. There are no new migrations.

## Stage 2: session booking

A booking is one session with a student and a tutor. The request identifies an approved subject offering and a published source slot. At creation, the API resolves the subject, tutor, faculty, and semester and rechecks eligibility against both users' current programs. The interval must fit the slot and semester, use a mode allowed by the slot, and start at least three hours after the trusted server timestamp. Self-booking is rejected. An introduction is exactly 30 minutes. Homework, exam preparation, course review, and other sessions may last 60, 90, or 120 minutes.

| Current state | Action | Result |
| --- | --- | --- |
| New | Verified student submits a request | `REQUESTED` |
| `REQUESTED` | Assigned tutor accepts before the deadline | `ACCEPTED` |
| `REQUESTED` | Assigned tutor declines before the deadline with a reason | `DECLINED` |
| `REQUESTED` | Internal expiration runs at or after the deadline | `EXPIRED` |
| `REQUESTED` or `ACCEPTED` | A participant cancels with a reason | `CANCELLED` |

Online acceptance requires an HTTP or HTTPS meeting URL. Onsite acceptance requires a room or location. Acceptance rechecks current account, offering, and academic eligibility while holding locks. An accepted session remains reserved. Decline, expiration, and cancellation release both reservations. The existing row survives each transition. Stage 3B adds explicit completion and coordinator adjudication.

### Request deadline

Let `N` be the elapsed time from the trusted request timestamp to session start.

| Notice | Expiration |
| --- | --- |
| Less than 3 hours | Request rejected |
| 3 through 25 hours, inclusive | Earlier of request time + 1 hour and start time − 2 hours |
| More than 25 hours | Earlier of request time + 48 hours and start time − 24 hours |

Acceptance and decline are rejected at the expiration instant, even if the request is still stored as `REQUESTED`. The [background worker](workers.md) calls the same Scheduling expiration operation with a configurable batch size, defaulting to 50. `FOR UPDATE SKIP LOCKED` lets workers process different rows. Repeated polls preserve a single history entry and one queued email per recipient for each expiration.

### Booking API

All routes below are under `/api/v1` and require an authenticated, verified student. Writes use the same-origin guard.

| Method and path | Behavior |
| --- | --- |
| `POST /sessions` | Creates a request and both reservations. Returns 201. |
| `GET /sessions/me` | Lists sessions where the caller is a student or tutor. |
| `GET /sessions/{id}` | Returns the session to either participant. |
| `POST /sessions/{id}/cancel` | Either participant sends a non-empty `reason`. Returns the updated session. |
| `GET /tutors/me/session-requests` | Assigned tutor's sessions. Defaults to `REQUESTED`. |
| `POST /tutors/me/session-requests/{id}/accept` | Assigned tutor sends `meeting_link` or `meeting_room`. |
| `POST /tutors/me/session-requests/{id}/decline` | Assigned tutor sends a non-empty `reason`. |

Example request:

```json
{
  "offering_id": "00000000-0000-0000-0000-000000000441",
  "availability_slot_id": "00000000-0000-0000-0000-000000000451",
  "starts_at": "2026-12-07T14:00:00Z",
  "duration_minutes": 60,
  "mode": "ONLINE",
  "purpose": "EXAM_PREPARATION",
  "description": "Help with dynamic programming."
}
```

Successful single-session responses use `{"data":{...}}`. Lists accept `status`, `limit` (1–100, default 25), and `offset` (0–100000, default 0) and sort by start and ID. Responses include the caller's `viewer_role` and meeting details once acceptance supplies them. Grades, emails, and contact details are omitted. Inaccessible sessions return 404, missing or insufficient permissions return 401 or 403, invalid input returns 400 or 422, and overlap or a stale transition returns 409.

### Transaction boundaries

- **Go application rules.** Enforce caller permissions, lifecycle transitions, minimum notice, purpose and meeting information, and both participants' current academic eligibility.
- **Lock order.** Resolve the offering owner or session participants as a read hint. Lock both Identity accounts in UUID order and their student rows before validating eligibility or locking the session. Stage 1 shares the tutor account lock. Tutoring locks the offering, profile, qualification, and approval rows. Catalog locks membership and the program used to resolve faculty. Booking locks the slot and selected semester before inserting the session. Academic and discipline writers should use compatible account locks. The row locks on eligibility, slots, and semesters also coordinate with direct updates to those rows.
- **PostgreSQL safeguards.** Existing triggers create, preserve, or release both participant blocks and require a valid source slot. The GiST exclusion constraint rejects overlaps across student and tutor roles while allowing adjacent half-open intervals. PostgreSQL also checks mode and time against the slot on insertion, semester boundaries, minimum notice, expiry, duration, and purpose. A recurring slot uses its rule's semester. A one-off slot uses the most recent configured semester that contains the full window. The fixed policy row is selected from the database and initialized under a transaction lock if absent.
- **Atomic records.** Session changes, audit history, and deduplicated pending notifications commit together. A new request notifies the tutor, acceptance or decline notifies the student, cancellation notifies the other participant, and expiration queues one notification for each participant. No email delivery is implied by a queued record.
- **Late cancellations.** Less than 24 hours before start is late. Exactly 24 hours is not. The Coordination operation records one violation for the cancelling participant in the session's semester. At two countable incidents it creates one flag for the participant's home faculty and queues a notification if that faculty has a coordinator. The user remains unsuspended. Confirmed no-shows use the same per-user lock and semester counter.

## Stage 3B: completion and attendance

The assigned tutor can send `POST /api/v1/tutors/me/sessions/{id}/complete` with their session cookie and the configured same-origin header. The server records completion at or after the scheduled end of an `ACCEPTED` session. Success returns 200 with the updated session, including `completed_at` and `completed_by_tutor_id`. The status change releases both reservations and commits with its history entry. No client timestamp is used.

Completion leaves any pending allegations intact. An authorized confirmation may correct an `ACCEPTED` or `COMPLETED` session to `NO_SHOW` while preserving prior history. The [Coordination guide](coordination.md) describes participant reporting, faculty-scoped review, and semester violations.

## Verification

A displayed free interval is a snapshot. It becomes a reservation only after the booking transaction commits. Stage 3A adds [automatic expiration and email delivery](workers.md). Stage 3B adds completion and attendance through the existing API and outbox.

Run from the repository root:

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The service-backed suite uses the migrated PostgreSQL schema and Redis. Stage 1 exercises availability management and protected deletion. Stage 2 covers cross-faculty booking, cross-role and concurrent overlap checks, an offering deactivation or slot deletion racing a request, response and expiration races, the semester flag threshold, and rollback when history or notification persistence fails.
