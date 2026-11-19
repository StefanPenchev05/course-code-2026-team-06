# Tutti PostgreSQL setup

> **Authentication requirement supersession:** Uni.lu-only institutional SSO is the approved target (FR1/FR23 Must; [D01](../requirements-decisions.md)). Local password registration/login, Argon2id and password-based coordinator activation described here are the current legacy implementation/design, not approved release authentication. Migration remains #46 and #65–#70; protocol and routes await university confirmation. Enrollment verification remains separate and required. This documentation task changes no endpoints or schema.


The first eight paired [migrations](../../backend/migrations/) create all 31 tables in the [approved v0.3 design](tutti-schema-v0.3.md). Migration 11 adds issue history, bringing the current total to 32; migrations 12 and 13 support issue resolution and cancellation-origin audit without adding tables. Where v0.3 leaves columns unspecified, the migrations use the [v0.2 DBML](tutti-schema-v0.2.dbml) as a baseline. The migration database should be dedicated to Tutti. The first migration installs `btree_gist` and its rollback removes it.

| Migration | Domain and key constraints |
| --- | --- |
| 1 | Academic catalog and semesters. Unique subject codes and program–subject links. |
| 2 | User identities, activation, and verification. Case-insensitive unique email, one coordinator per faculty, and one pending verification per student. |
| 3 | Tutor profiles, applications, qualifications, and offerings. One pending application per student, one qualification at most per application, grades from 15.00 to 20.00, and one offering per tutor and subject. Composite foreign keys match each qualification to its applicant and each offering to the correct tutor and subject. |
| 4 | Policy parameters and recurring and concrete availability. Weekly rules, per-date exceptions, and an exclusion constraint against overlapping concrete windows for one tutor. |
| 5 | Sessions and reservations. Duration, purpose, notice, expiry, semester and slot fit, matching tutor/offer/slot, and cross-role overlap protection. |
| 6 | Feedback, issue and attendance reports, violations, and semester flags. One feedback row per session, at most one violation per user and session, at most one flag per user and semester, and a confirmed attendance report required for a no-show violation. |
| 7 | Email notification records, suspension history, and status history. History records store the actor, time, and both states for a transition. |
| 8 | Sponsors and aggregate report snapshots. Reporting-period, count, and feedback-average checks. |
| 9 | Email outbox delivery leases, attempts, retry schedule, failure category, and a pending-delivery index. |
| 10 | Outbox notification types for attendance reports and decisions. |
| 11 | Append-only issue status history, one creation event per report, and bounded new issue descriptions. |
| 12 | Bounded resolution notes and description validation on inserts/actual changes, allowing resolution to preserve legacy evidence. |
| 13 | Explicit participant/policy cancellation origin in session history, preserving unknown legacy origins and guarding against rollback evidence loss. |

## Local commands

From the repository root, start PostgreSQL 17 using Docker Compose. The defaults below match `.env.example`. If you change credentials or the port, update the connection URL accordingly. Percent-encode credentials if they contain URL punctuation.

```sh
cp -n .env.example .env
docker compose up -d --wait postgres
export DATABASE_URL='postgres://tutti:tutti_dev@localhost:5432/tutti?sslmode=disable'
migrate -path backend/migrations -database "$DATABASE_URL" up
```

Install the `golang-migrate` CLI with PostgreSQL support if it is missing. For example, with Go and its binary directory on your path:

```sh
go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
```

Roll back one migration with `down 1`. To reset a **disposable local** database, roll back all migrations and reapply them. Rollbacks delete schema objects and their data.

```sh
migrate -path backend/migrations -database "$DATABASE_URL" down 1

# Reset a disposable local database.
migrate -path backend/migrations -database "$DATABASE_URL" down -all
migrate -path backend/migrations -database "$DATABASE_URL" up
```

The integration runner requires Docker, Python 3, `migrate`, and PostgreSQL client tools (`psql`, `pg_isready`). It starts a separate PostgreSQL 17 container on an ephemeral local port with a temporary in-memory data directory and removes that container on exit.

```sh
python3 backend/tests/integration/test_schema.py
```

The runner checks the full table inventory, constraints, and SQLSTATEs. It holds one booking open in a transaction and waits for the competing connection to block, then verifies that only one booking and its two blocks commit. It verifies upgrade 10 → 11 → 12 with legacy issue data, authentic legacy resolution, migration 12 rollback without losing history, migration 13 constraints/legacy-history preservation/rollback guard, all thirteen rollbacks on disposable fixtures, and replays the migrations from empty. On Windows, `.\backend\tests\integration\test_docker.ps1` runs these checks using the existing Dockerfile and disposable containers, without native database tooling or changes to Compose services.

## Policy cancellation transaction contract

Issue #24 / FR19 / UC04 uses Coordination's authenticated coordinator service and
Scheduling's injected transaction-bound cancellation adapter. Identity locks the
active coordinator assignment/account before Scheduling locks the session by ID
and immutable faculty. Only `REQUESTED`/`ACCEPTED` can become `CANCELLED`; no new
timing cutoff is imposed. Participant eligibility is irrelevant to this authority,
and no participant account locks or discipline writes follow the session lock.
Existing lifecycle writers serialize on the same row.

One transaction persists cancellation actor/reason/time with a false late flag,
releases both participant blocks through existing triggers, appends history, and
queues `SESSION_CANCELLED` for both participants. History/outbox failure restores
the original session and both reservations. No trigger, reservation table, state,
notification enum or disciplinary rule is duplicated. Existing faculty/session
indexes and the session primary key support the scoped lookup.

[Migration 13](../../backend/migrations/000013_policy_cancellation.up.sql) adds the
nullable `session_status_history.cancellation_origin` text column. Non-null values
must be `PARTICIPANT` or `COORDINATOR_POLICY`, on a `USER` transition from
`REQUESTED`/`ACCEPTED` to `CANCELLED` with a nonblank reason of 1–2000 UTF-8 bytes.
Existing history is untouched, including legacy reasons outside today's bounds;
null means unknown/not applicable. Do not infer historical capacity from current
coordinator assignments. New application cancellations set their explicit origin;
the application supplies trusted actor, authority and transactional consistency.
This adds no general session-history mutation API or retention job.

Apply migration 13 before updated API **and worker** binaries: their shared event
writer now includes the column for all session events. An older binary remains
schema-compatible after the additive upgrade, but cannot record explicit origins;
use the updated API for policy actions. The [down migration](../../backend/migrations/000013_policy_cancellation.down.sql)
takes an exclusive history-table lock and refuses to drop the column if any origin
has been recorded, including participant origins. With no such records, rollback
drops only the new column/check and preserves prior history. Stop updated writers
and restore compatible binaries before an intentional rollback. Never delete
history to bypass the retention guard. Migration testing does not alter the
developer's database; exact PowerShell commands are in [Coordination](../coordination.md#windows-verification-and-migration-order).

## Issue creation transaction contract

The [Coordination issue service](../coordination.md#general-issue-reporting-fr10--uc06) implements FR10 / UC06 creation. The same transaction locks and rechecks the reporter's current Identity state and program, obtains the current Catalog faculty for an unlinked report or the authorized Scheduling participant session for a linked report, and inserts `issue_reports` plus `issue_status_history`. Linked issues use the session's immutable tutor faculty, enforced again by the existing `(session_id, faculty_id)` foreign key. The existing `(faculty_id, status, created_at)` index supports future scoped inbox queries.

New reports begin `OPEN` and write a `NULL` → `OPEN` history event with actor kind `USER`, the reporter ID, and the report's database-generated creation timestamp. PostgreSQL enforces history references, actor metadata, actual status changes, and at most one creation event per report. Triggers reject history UPDATE, DELETE, and TRUNCATE. The parent FK does not cascade; historical reports cannot be deleted while their history remains. The application is responsible for authenticating the actor, deriving faculty/participation, and inserting both rows atomically; raw administrative SQL is not an authenticated workflow.

Migration 11 does not fabricate audit entries for older reports. Its 1–2000 UTF-8 byte/non-whitespace description constraint uses `NOT VALID`. Because PostgreSQL still checks new versions of existing rows, that constraint would reject resolution of oversized legacy evidence. Migration 12 replaces it with an insert/update-of-description trigger that validates new or actually changed descriptions. Unchanged original evidence remains intact. New descriptions retain the same bounds, and no automated retention deletion is added.

Deploy migration 11 before the new API binary. Rolling back that migration deliberately drops the history table and removes the new constraint while retaining issue reports. Archive any required history before an intentional rollback; never run destructive migration tests on the developer's regular or shared database. The test runner exercises rollback only on its disposable database.

## Issue inbox and resolution transaction contract

Issue #22 / FR17 / UC13 extends the same Coordination service. Reads join the active coordinator's current assignment against `issue_reports.faculty_id`; they never infer report ownership from a student's current program. The existing faculty/status/creation-time index supports bounded `OPEN` or `RESOLVED` inbox queries ordered by creation time and ID.

Resolution locks the current Identity coordinator assignment/account (`FOR SHARE`) before the scoped report (`FOR UPDATE`). It permits only `OPEN` → `RESOLVED`, updates resolver/note/time with a conditional `WHERE status='OPEN'`, and appends the transition with matching actor/time. Both statements share one transaction. The retained report contains the note referenced by the history record. A failed history insert rolls back the update; concurrent or duplicate resolution returns conflict. Creation history and original evidence remain untouched. Legacy reports need no fabricated creation event to receive a new authentic resolution event.

Migration 12 adds a `NOT VALID` check requiring a non-whitespace resolution note of at most 2000 UTF-8 bytes for newly written resolved rows. Existing timestamp, resolver and composite faculty foreign keys remain in force. The append-only history triggers remain unchanged. General issue resolution writes no session, attendance, violation, flag, or notification data.

Apply migration 12 before deploying the inbox/resolution API. Its down migration drops only the new validation machinery and reinstates migration 11's `NOT VALID` description check; it deletes no reports or history. After rollback, oversized legacy descriptions are readable but cannot be updated until migration 12 is reapplied. Older down migrations remain destructive. Existing composite reviewer foreign keys also constrain coordinator reassignment/deletion after review; changing that historical model is separate work.

## Monitoring read contract (Issue #23 / FR16 / UC12)

Coordination starts a `REPEATABLE READ READ ONLY` transaction and resolves active
coordinator authority through Identity in that snapshot. Explicit transaction-bound
Tutoring, Scheduling and Feedback adapters reuse it for every query in the response.
Current tutor membership follows `students.program_id` / `programs.faculty_id`;
session data follows immutable `tutoring_sessions.faculty_id`. Permission changes
committed before the snapshot apply immediately; changes after it apply next time.

Counts/hours use current `COMPLETED` sessions left-joined to the unique feedback row,
with no offering/availability join multiplying the population. Faculty ratings use
the underlying ratings rather than averages of tutor averages. Profile queries use
`EXISTS` for offering eligibility. Tutor lists batch statistics for at most 101 IDs
(the requested page plus a lookahead). Schedule queries require an interval of at
most 31 days and bounded pagination. Existing indexes suffice; no migration or
persisted counter is introduced. See [API and population definitions](../coordination.md).

## Booking transaction contract

The session write creates exactly two participant blocks for `REQUESTED` and `ACCEPTED` through a database trigger. Any conflict aborts the session write and both block writes. A transition out of these states deletes both blocks in the same transaction. A separate guard prevents direct changes that would remove or alter an active reservation. The GiST exclusion alone guarantees that existing blocks never overlap for the same user. The triggers guarantee that an active session has both matching blocks. Both concrete availability and reservations use half-open intervals `[start, end)`.

The [Go booking service](../scheduling.md#stage-2-session-booking) commits the session, audit history, and queued email records together. PostgreSQL manages participant blocks through triggers. Go locks each session before responding and checks its deadline using trusted server time. The database independently checks the expiry formula against the stored request and start timestamps. The API translates exclusion failures (`23P01`) to booking conflicts.

Each new session needs a source slot and must fit its mode and interval and the chosen semester. PostgreSQL prevents deletion of that slot while a session is active. Deleting a slot later clears the reference from historical sessions and preserves their scheduled details. To change a concrete window's time or mode, replace the occurrence. When deleting only one generated occurrence, write its exception in the same transaction so generation does not recreate it.

## Application responsibilities

- **Identity and faculty authority.** Verify registration through the university flow, provision coordinators, validate activation-token hashes and single use, and check the authenticated actor's permissions. Foreign keys can match a recorded reviewer to a recorded faculty. Go must verify that the faculty was selected correctly and that the actor is authorized to see or change that record.
- **Tutor approval and eligibility.** Create exactly one subject and grade with each application in a transaction. Approval and offering activation must follow the coordinator decision. For a booking, verify student approval, both program memberships in the subject, and the tutor's active profile and approved active offering. Revalidate on program changes and handle existing sessions explicitly.
- **Scheduling.** Choose the configured semester and tutor faculty, validate IANA time zones, generate recurring occurrences through daylight-saving changes, apply exceptions, and remove only future unbooked occurrences when a rule is deleted. Manage semester calendar edits without invalidating existing sessions. Reconfirm schedules at semester rollover.
- **Session transitions and auditing.** Authorize each actor, enforce legal transitions, calculate late cancellations, and write histories and durable email records with the change. The database stores status values, action metadata, and reservations. It does not authenticate actors or append application or session status history automatically.
- **Attendance and discipline.** PostgreSQL validates the opposing participants, a seven-day reporting window, reviewer faculty, and the confirmed-report reference for a no-show violation. Go decides the outcome, records a confirmed violation and any session correction, and excludes feedback for sessions no longer completed. Count violations in the session semester and create a flag once the count reaches two. Serialize these updates per user so simultaneous incidents cannot miss the threshold. Route the flag and any manual suspension or reinstatement to the home faculty coordinator. A flag never suspends an account on its own.
- **Issue and feedback workflows.** Route issues with a session or tutor to the tutor's faculty, otherwise to the reporter's home faculty. Allow only the requesting student to submit feedback and only while the session is completed. Review issue access case by case for cross-faculty requests.
- **Sponsor reports and privacy.** Compute distinct participants in completed sessions and suppress breakdowns with fewer than five distinct participants before export. Export aggregates only. Restrict qualification grades and any notes to entitled users. Keep migration privileges separate from the runtime role and grant runtime audit writers append access without update or delete access.

## Implementation choices and retention

UUID primary keys default to `gen_random_uuid()` where they are not shared role IDs. Qualification records also carry the applicant student ID to let composite foreign keys enforce ownership. Activation tokens have a unique binary hash plus creation, expiry, and use times. Suspension history stores the affected home faculty, coordinator, old and new state, reason, and timestamp. Attendance uses `PENDING`, `CONFIRMED`, and `REJECTED`. Seven days is measured as 168 elapsed hours from the scheduled session end. Earlier profile-image ideas fall outside the approved 31-table model.

The schema records the time anchors for retention. Rejected verifications and delivered or finally failed email content have a proposed one-year period from decision or delivery/failure. Account data has a period of up to one year after closure. Qualification evidence, session records, disciplinary records, and sponsor snapshots have a proposed three-year period from approval end, session end, semester end, and snapshot creation respectively. The eventual cleanup should review dependencies and unresolved complaints, then anonymize or remove the eligible data in dependency order under a university Data Protection Officer approved policy. Retention deletion is not automated.
