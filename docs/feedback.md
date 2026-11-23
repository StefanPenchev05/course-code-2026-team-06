# Feedback, ratings, and tutor activity

A verified, unsuspended learner can rate their own currently completed session once. The rating must be a JSON integer from 1 through 5. An optional comment can contain up to 2,000 characters. Leading and trailing whitespace is trimmed, and an empty comment is omitted. Feedback stays attached to the session for its historical record.

## API

All paths start with `/api/v1` and require a Tutti session. Submission requires the configured same-origin header. Identity determines the viewer and author.

| Method and path | Behavior |
| --- | --- |
| `POST /sessions/{id}/feedback` | The session's student submits feedback. Returns 201. |
| `GET /sessions/{id}/feedback` | Returns active feedback to the two participants or the session's recorded tutor-faculty coordinator. That coordinator can also inspect preserved feedback after a correction. |
| `GET /tutors/me/feedback` | An active tutor lists feedback they received on currently completed sessions. |
| `GET /tutors/me/activity` | An active tutor reads their completed-session count, hours taught, aggregate rating, and upcoming accepted sessions. |
| `GET /coordinator/tutors/{id}/feedback` | A coordinator lists active feedback and retrieves rating statistics for a tutor in their faculty. The list and summary include only sessions assigned to that faculty. |

For example:

```json
{"rating":5,"comment":"The tutor explained dynamic programming clearly."}
```

Single and activity responses use the `data` wrapper. Activity includes `completed_sessions`, `hours_taught`, `average_rating`, `rating_count`, and `upcoming_sessions`. Upcoming entries include subject, scheduled time, mode, and meeting details. Upcoming means a currently accepted session starting at or after the server time. The list endpoints and activity accept `limit` (1–100, default 25) and `offset` (0–100000). Feedback sorts by creation time, newest first, then session ID. Upcoming sessions sort by start time then ID. Pagination includes `has_more`. Coordinator rating statistics cover the full authorized set, independent of page size.

A missing or inaccessible session, tutor, or feedback record returns 404. Duplicate feedback returns 409. An invalid rating, comment, or session outcome returns 422. A fractional rating or another incorrect JSON type returns 400. Comments are private to authorized viewers through these feedback routes. Discovery and profile responses include only `average_rating` and `rating_count` from Feedback.

## Storage and consistency

The Feedback module uses the existing `feedback` table and its session primary key and rating check. To submit, it locks the student's account through Identity, then uses Scheduling's transaction-bound row lock on the session. After rechecking ownership and `COMPLETED`, it inserts the feedback in the same transaction. PostgreSQL's primary key prevents duplicates.

Attendance confirmation shares the session lock. If a correction wins first, the later submission sees `NO_SHOW` and fails. If feedback wins first, it remains stored when the session is corrected. Active queries use only the current `COMPLETED` outcome. Pending allegations have no effect on feedback or statistics.

The same grouped PostgreSQL calculation supplies the public rating summary and teaching totals. A tutor's average is rounded to two decimal places and is null when there are no valid ratings. Rating count includes only sessions with valid feedback. Teaching hours sum the scheduled duration of all currently completed sessions, including unrated ones, then divide by 60. These values are derived because stored counters could drift after a correction. Discovery looks up all tutor IDs on the page in one query. Activity and discovery each combine separate committed reads. A coordinator's report page and totals share one database snapshot.

A coordinator's tutor-level view checks the tutor's current faculty and filters entries and totals by the faculty recorded on each session. Session-level oversight follows the faculty recorded on that session, even after a tutor changes programs. The student participant's faculty does not grant coordinator access.

The module uses the existing session and feedback indexes and requires no schema migration. It has no edit, delete, moderation, or public comment feed. General issue handling and coordinator account administration remain separate.

## Verification

Issue #23's [coordinator monitoring APIs](coordination.md#tutor-monitoring-and-faculty-statistics-fr16--uc12-issue-23)
reuse these same aggregate expressions through a transaction-bound query port.
Faculty averages are calculated over individual qualifying feedback rows, not tutor
averages. Tutor list/detail, faculty counts and activity share the caller's read-only
snapshot. The existing private-comment endpoint and its response contract remain
unchanged; new monitoring responses never include comments.

Run from the repository root:

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The PostgreSQL/Redis suite checks duplicate submissions, rating and comment privacy, faculty restrictions, discovery aggregation, fractional teaching hours, upcoming sessions, and no-show corrections racing feedback.
