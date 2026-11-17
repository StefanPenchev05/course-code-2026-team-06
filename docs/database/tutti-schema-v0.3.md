# Tutti — Database Schema v0.3

**Status:** Policy-approved relational design. See the [PostgreSQL implementation and verification instructions](implementation.md).

**Basis:** Deliverable 1 and Team 6 architecture documentation, database v0.2, and decisions agreed in the conversation through 16 November 2026.  
**Companion:** [v0.2 DBML](tutti-schema-v0.2.dbml) for baseline columns. The [v0.3 SQL migrations](../../backend/migrations/) implement the approved changes.

## What changed since v0.2

1. **No-show adjudication is defined.** Either participant reports the other after the scheduled session ends and within **7 days**. A claim enters `attendance_reports(PENDING)`. Only the **tutor's faculty coordinator** can confirm or reject it, with a resolution note. A confirmed no-show may move the session to `NO_SHOW` and inserts a single `user_violations` row for the absent participant. Rejected or pending claims never create violations. A tutor-marked `COMPLETED` session may be challenged and corrected, with status history retained; feedback from a corrected no-show is excluded from public averages.
2. **Cross-faculty responsibilities are settled.** Tutor's faculty coordinator owns session-related issue investigations and policy cancellations. The **affected person's home faculty coordinator** owns account suspension, reinstatement, and per-semester disciplinary flags. If an issue names no tutor or session, it routes to the reporting student's home faculty. Coordinators must not gain general data access to another faculty merely by participating in a cross-faculty case.
3. **Registration flow is settled.** New students submit university email, ID, and program for coordinator verification; until approval they cannot use tutoring features. A rejected applicant can correct and resubmit, but only one verification is pending at a time. Coordinators are preprovisioned from university data and activate their account using an email-based one-time password setup, with **hashed activation tokens** stored in `account_activation_tokens`.
4. **Recurring availability behavior is settled.** Weekly rules are scoped to the semester and IANA time zone. An exception suppresses a single occurrence; deleting a recurring rule removes future *unbooked* occurrences. Booked/requested future occurrences cannot be removed until the session is resolved. Semester rollover requires a new or reconfirmed schedule. Scheduled sessions must fall within their configured semester.
5. **Academic program changes trigger subject revalidation.** Tutor offerings that no longer belong to the tutor's academic program become inactive until a coordinator reviews them. `tutor_offerings` gains `is_active`, `deactivated_at`, and `deactivation_reason`. Existing accepted sessions are not silently deleted: their disposition needs an explicit authorized action.
6. **Data retention categories are set provisionally for the product.** Rejected student verifications: **1 year after rejection**; notification content: **1 year after delivery** (or final failure; `email_failed_at` is the failure retention anchor); qualification evidence: **3 years after tutor approval ends**; sessions, feedback, and status history: **3 years after the session ends**; confirmed incidents and disciplinary records: **3 years after the semester ends**; account personal data: **while active and up to 1 year after closure**, unless earlier erasure is required; sponsor report snapshots: **3 years after creation**. These are **product requirements subject to the university Data Protection Officer's review**, not statutory claims. `users.closed_at` is added to support the closure retention anchor.
7. **Sponsor reporting uses confirmed participation.** An active user is a **distinct student or tutor participating in at least one completed session** during the report period. When exporting, suppress breakdowns that involve fewer than **five distinct participants** and disclose no personal identifiers. Reports are stored as aggregate snapshots.
8. **Account discipline is auditable.** `user_suspension_history` records each home-faculty-coordinator suspension/reinstatement with reason and time. Suspensions last until explicitly reversed; no automatic expiration.
9. **Previously approved booking-expiry policy is captured in schema policy parameters.** Minimum notice **3 hours**; urgent 3–25-hour requests expire according to the hybrid rule below. Both participant identities are blocked atomically to prevent concurrent sessions across student/tutor roles.

## Booking expiration policy (approved)

Let `t_req` be the server-side request timestamp, `t_start` the scheduled start, and `N = t_start - t_req` the notice.

- `N < 3 hours`: reject the booking request.
- `3 hours <= N <= 25 hours`: `expires_at = min(t_req + 1 hour, t_start - 2 hours)`.
- `N > 25 hours`: `expires_at = min(t_req + 48 hours, t_start - 24 hours)`.

At exactly 3 hours the request expires in 1 hour, leaving a 2-hour buffer. At exactly 25 hours it expires in 1 hour, and for intervals just over 25 hours the earlier standard deadline is just over 1 hour away; the policy has no abrupt backward jump. Only `REQUESTED` sessions may expire. Expiry and release of participant booking blocks must occur atomically, and stale accept/decline attempts fail.

**Business-rule checks to automate:** notice 2h59m / 3h / 25h / 25h+1s; 48h and 72h notice; exactly 24h notice; two simultaneous requests for the same person's interval; concurrency between expiry and acceptance; DST boundaries when generating weekly windows.

## Domain table inventory (31)

| Area | Tables | Count |
|---|---|---:|
| Academic catalog | `faculties`, `programs`, `subjects`, `program_subjects` | 4 |
| Identity and student verification | `users`, `students`, `coordinators`, `account_activation_tokens`, `student_verifications` | 5 |
| Tutor qualification | `tutor_profiles`, `tutor_applications`, `tutor_qualifications`, `tutor_offerings` | 4 |
| Scheduling | `session_policies`, `availability_rules`, `availability_exceptions`, `availability_slots`, `tutoring_sessions`, `session_participant_blocks` | 6 |
| Quality, coordination, semesters | `feedback`, `issue_reports`, `semesters`, `attendance_reports`, `user_violations`, `user_semester_flags` | 6 |
| Notification and audit | `notifications`, `user_suspension_history`, `session_status_history`, `application_status_history` | 4 |
| Sponsor (Should-have) | `sponsors`, `sponsor_reports` | 2 |
| **Total** | | **31** |

## PostgreSQL-enforceable constraints and invariants

| Direct DB constraint / index | Description |
|---|---|
| Primary keys + foreign keys | All cross-entity identity and ownership references, including reusable `users.id` for students/tutors. |
| `UNIQUE(lower(users.email))` | University emails case-insensitively unique. |
| `UNIQUE(subjects.code)` | One canonical subject ID/code university-wide, shareable across programs/faculties. |
| `UNIQUE(program_id, subject_id)` | No duplicate program-subject catalog links. |
| `UNIQUE(coordinators.faculty_id)` | At most one coordinator per faculty. |
| Partial unique indexes on `status = 'PENDING'` | One pending `student_verifications` and one pending `tutor_applications` record per student. |
| `UNIQUE(tutor_qualifications.application_id)` | At most one subject/grade per tutor application. Creation workflow ensures **exactly** one. |
| `CHECK (grade BETWEEN 15 AND 20)` | Decimal grade range; current storage format is `NUMERIC(4,2)`. |
| `UNIQUE(tutor_id, subject_id)` | One offering for each tutor and subject; `is_active` governs current eligibility. |
| Composite offering / qualification foreign keys | Offering subject matches its grade evidence and booking tutor matches the offering's tutor. |
| Session duration and purpose checks | Introduction = 30 minutes; other purposes = 60, 90, or 120; timestamp interval equals duration. |
| `CHECK (student_id <> tutor_id)` | Prevent self-booking. |
| Two occupancy rows per active session, with GiST exclusion on `(user_id, tstzrange(starts_at, ends_at, '[)'))` | Prevent any overlapping booking for the same person across both roles. The **two-row guarantee** needs transactional validation, not only the exclusion index. |
| `feedback.session_id` primary key | At most one rating per completed session. |
| `UNIQUE(user_id, session_id)` in violations | At most one countable violation for a particular user in a particular session. |
| `UNIQUE(user_id, semester_id)` in flags | At most one semester threshold flag for each user. |
| Status-specific `CHECK`s | Cancellation/decline reasons, reviewed attendance decisions, timestamp order, etc. |

**Important:** DBML is a conceptual design, not sufficient by itself to enforce these PostgreSQL behaviors. Partial unique indexes, range exclusion constraints (`btree_gist`), and any necessary trigger definitions must be added to actual migration SQL. PostgreSQL foreign keys and checks cannot generally verify complex conditions involving other rows or tables.

## Rules that require application transactions or constrained database routines

- **Faculty authority:** verify the coordinator's faculty for student verification, tutor approval, issue adjudication, suspension, and reinstatement. A tutor's faculty can investigate a cross-faculty incident without gaining broad permissions over the student's other records.
- **Student validation:** ensure verified student status before using student/tutor features, verify the provided identity against the university source only through the coordinator-controlled flow, and ensure no pending verification is duplicated.
- **Tutor eligibility:** verify subject membership in the current program, qualification evidence, and approved application. Only active offerings are searchable/bookable. Revalidate upon program change; coordinate the future accepted sessions explicitly.
- **Booking:** the same `subjects.id` must belong to the student program and the tutor program. Session must fit an available window in its mode and semester. Enforce booking notice and expiry formula using trusted server time. Insert session and **two participant blocks** in the same transaction; reject overlapping intervals under concurrent load.
- **State machine:** accept/decline only while `REQUESTED`, expire unanswered requests only; tutor can complete an accepted session only after its scheduled end. Every session/application change and required notification/outbox record must be persisted atomically. Stale requests must be idempotently rejected.
- **Recurring windows:** generate occurrences from local weekday/time and IANA zone; respect exceptions and semester end; prevent invalid deletion of booked occurrences. Window deletion is physical, with `availability_slot_id` nullable for historical sessions whose source slots were removed.
- **Attendance:** allow either participant to report another participant within seven days *after scheduled end*; only tutor faculty coordinator can confirm/reject. Only confirmed absence counts toward `user_violations`, in the **session's semester**. Preserve a status-history correction when overturning `COMPLETED`; calculate averages only for sessions whose **current** status is `COMPLETED`.
- **Discipline:** late cancellations count directly; confirmed no-shows count only after adjudication. At two semester incidents, generate a single flag routed to the subject user's home faculty coordinator. Only that coordinator can suspend/reinstate; suspension cancels future sessions and sends required emails.
- **Privacy:** subject grades visible only to the entitled coordinator; sponsor exports are aggregated and suppressed for small groups. Set purge/anonymization routines per category and get university DPO sign-off before production.

## Approved policies vs. implementation-level choices

The core stakeholder business decisions above are approved. The following are **implementation details**, not reasons to reopen business-rule elicitation:

- Exactly how long activation tokens remain valid, and the recovery flow for expired activation links.
- Whether the Go worker polls due `expires_at` sessions or uses a durable queued job (always backed by PostgreSQL state checks).
- How many future recurring occurrences are materialized in advance.
- Whether cross-table invariants are protected primarily with service-layer transactions, SQL functions, or triggers.
- How developers enter the academic calendar and which university IANA time zone is chosen.
- How to handle operational retention exceptions such as an unresolved complaint or a valid erasure request, under DPO guidance.

The [implementation notes](implementation.md) describe where the SQL enforces these rules and where the Go service must participate.
