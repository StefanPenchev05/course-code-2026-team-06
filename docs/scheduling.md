# Scheduling stage 1: availability

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

## Stage 2 and verification

Stage 2 needs a booking transaction that rechecks the slot, both participants, academic eligibility, and the approved offering while creating a session. The free-time endpoint reports a read snapshot. It cannot hold time for a subsequent request. Stage 2 will also implement the booking lifecycle, expiration, and notifications.

Run from the repository root:

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The service-backed runner uses the migrated PostgreSQL database and Redis. Its Scheduling test covers ownership, suspension, rule reconciliation, one-off windows, exceptions, reserved-time subtraction, overlap under concurrency, and a booking transaction racing slot deletion. Booking rows in that test are fixtures inserted directly through the existing database constraints.
