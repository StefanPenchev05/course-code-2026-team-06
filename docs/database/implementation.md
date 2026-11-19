# Tutti PostgreSQL setup

> **Authentication requirement supersession:** Uni.lu-only institutional SSO is the approved target (FR1/FR23 Must; [D01](../requirements-decisions.md)). Local password registration/login, Argon2id and password-based coordinator activation described here are the current legacy implementation/design, not approved release authentication. Migration remains #46 and #65–#70; protocol and routes await university confirmation. Enrollment verification remains separate and required. This documentation task changes no endpoints or schema.


The first eight paired [migrations](../../backend/migrations/) create all 31 tables in the [approved v0.3 design](tutti-schema-v0.3.md). Where v0.3 leaves columns unspecified, the migrations use the [v0.2 DBML](tutti-schema-v0.2.dbml) as a baseline. The migration database should be dedicated to Tutti. The first migration installs `btree_gist` and its rollback removes it.

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

The runner checks the full table inventory, constraints, and SQLSTATEs. It holds one booking open in a transaction and waits for the competing connection to block, then verifies that only one booking and its two blocks commit. It also verifies all ten rollbacks and replays the migrations from empty.

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
