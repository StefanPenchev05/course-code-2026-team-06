# Tutoring

Issue #26 / migration 15 makes application history and qualification evidence
append-only and freezes submitted applications except for their single review.
New history timestamps equal the application's submission/review time. Automated
offering withdrawal now appends `tutor_offering_deactivations` with SYSTEM,
qualification, current program/faculty, time and reason in the same transaction;
later explicit reapproval cannot erase it. See [audit preservation](audit-history.md)
for the workflow matrix, failure tests and retention limitations.

A verified student applies to tutor one university subject at a time. The coordinator assigned to the student's faculty reviews that application's subject, grade, and description. Approval adds a subject offering to the student's tutor profile. The same account can still request tutoring as a student.

The module lives in [`backend/internal/tutoring`](../backend/internal/tutoring). Its application service validates workflows and depends on Catalog's academic interface and Identity's principal. The PostgreSQL adapter owns `tutor_applications`, `tutor_qualifications`, `tutor_profiles`, and `tutor_offerings`. It also writes application history and decision email records. Bootstrap uses the existing PostgreSQL pool, router, Redis sessions, and authorization middleware.

## API

All paths start with `/api/v1`. Every endpoint requires a session cookie. All writes require the configured same-origin `Origin` header. See the [Identity setup guide](identity.md).

| Method and path | Access and result |
| --- | --- |
| `POST /tutor-applications` | Verified student. Submits one subject and returns 201 with the application ID and `PENDING`. |
| `GET /tutor-applications/me` | Student. Lists their applications. |
| `GET /tutor-applications/{id}` | Applicant or their faculty coordinator. Returns status, subject, dates, and any rejection reason. |
| `GET /coordinator/tutor-applications` | Coordinator. Lists their faculty's applications. |
| `GET /coordinator/tutor-applications/{id}` | Authorized coordinator. Includes the private grade and application description. |
| `POST /coordinator/tutor-applications/{id}/approve` | Authorized coordinator. Approves the pending application and returns 204. |
| `POST /coordinator/tutor-applications/{id}/reject` | Authorized coordinator. Requires a JSON `reason` and returns 204. |
| `GET /tutors` | Verified student. Searches for eligible tutors for a required `subject_id`. |
| `GET /tutors/me` | Verified student. Shows their tutor profile and its approved offerings, including inactive ones. |
| `GET /tutors/{id}` | Verified student. Shows an eligible tutor profile and active approved offerings. |

Example application body:

```json
{
  "subject_id": "00000000-0000-0000-0000-000000000221",
  "grade": 17.50,
  "description": "I can help students understand algorithms."
}
```

Example 201 response:

```json
{"application_id":"62aa3b94-9ed4-4dca-9759-9c1eedf9f7d3","status":"PENDING"}
```

Grades must be JSON numbers from 15.00 through 20.00 in ordinary decimal notation, with at most two decimal places. The backend stores exact hundredths. Coordinator details return the grade as a decimal string such as `"17.50"`. Student application views, tutor profiles, and search results omit grade and qualification IDs. Profile and search results include name and descriptions but omit email, student number, and review details.

Application lists and discovery return `{"data":[...],"pagination":{"limit":25,"offset":0,"has_more":false}}`. The default limit is 25, maximum 100, and offset can be up to 100,000. Application lists order by newest submission and then ID. The coordinator queue accepts `status=PENDING|APPROVED|REJECTED` and defaults to pending. Discovery accepts `faculty_id`, `program_id`, and `level=BACHELOR|MASTER` and sorts by tutor name and ID. Faculty and program filters apply to tutors. The requesting student's faculty does not filter candidates.

Missing sessions return 401. Users without the required role or verification receive 403. Private applications outside the actor's ownership or faculty return 404. Malformed requests return 400 and invalid application fields, search filters, or grades return 422. Conflicting applications and already decided reviews return 409.

## Writes, eligibility and history

- **Submission.** Lock the applicant and read their current program, faculty, verification, and suspension state. A transaction-bound Catalog adapter checks the program and subject link. Insert the application, its single qualification, and the initial history together. PostgreSQL's pending-application index resolves conflicts, and an application for an active subject offering returns 409.
- **Decision.** Resolve the coordinator's current faculty, then lock the applicant and the pending application and qualification. Approval rechecks the applicant's verified and active state, current faculty, grade, and program membership. An application that has moved outside the reviewing faculty cannot be approved there. Rejection requires a reason. The decision, profile and offering changes, history, and one pending email record commit together. Concurrent review attempts can produce only one decision.
- **Repeat approval.** A second subject reuses the tutor profile. If a previously approved offering is inactive and the coordinator explicitly approves a new application, the module reactivates the same offering ID using the newly reviewed qualification. The previous application, grade, and history remain available to authorized reviewers. A disabled tutor profile needs an authorized lifecycle decision before taking on more approved subjects.
- **Discovery.** The service checks the requester's program through Catalog. A set-based PostgreSQL query checks the live membership and verification of both sides in the same read snapshot. It includes active profiles, active offerings with approved evidence, and eligible tutors from any faculty. Search covers profile and offering information. See [Feedback](feedback.md) for aggregate ratings included in discovery and profiles and for the tutor activity endpoint.

The write adapters lock the user and student rows, the Catalog program and matching membership rows, and the relevant application records in the same PostgreSQL transaction. Go never needs to make a separate pool call for academic checks that govern a Tutoring write. Program changes and administrative edits should use transactions and coordinate with these locks. Discovery and the offering read port are point-in-time reads.

### Internal offering revalidation (Issue #16)

**Implemented:** `Service.RevalidateTutorEligibility(ctx, studentID)` reconciles the current program in its own transaction. `RevalidateTutorEligibilityInTransaction(ctx, tx EligibilityTransaction, studentID)` runs the **same** reconciliation logic in a caller-owned transaction. These are internal capabilities, not HTTP routes. Invalid UUIDs fail before persistence; unknown students return Tutoring's not-found error. A known student without a tutor profile or active offerings safely requires no offering writes.

Reconciliation locks the affected Identity account and student, reads the current program, locks the program through Catalog, then locks only that student's active offerings in subject order and checks current subject membership through the same transaction-bound Catalog adapter. Only active offerings whose canonical subjects are absent from that program are deactivated. Shared subjects remain active even across faculties. The only business table it modifies is `tutor_offerings`: `is_active=false`, `deactivated_at=statement_timestamp()` and a nonempty program-ineligibility reason. Migration `000003` already enforces consistent active/inactive metadata; no new migration is needed.

Inactive offerings are not selected, and the guarded update also requires `is_active`. Repeated or concurrent reconciliation preserves the original deactivation time/reason. Reconciliation does not create applications, approvals, qualifications, history events or notifications, and never reactivates an offering. Returning to the old program is insufficient: a new one-subject application and explicit authorized coordinator approval must restore the offering. That existing approval workflow retains the offering ID, attaches new qualification evidence, clears deactivation metadata, and preserves earlier evidence/history.

Revalidation does not write `tutoring_sessions`, participant blocks, session history or feedback. REQUESTED, ACCEPTED and historical sessions remain intact; it does not cancel or reschedule them. Handling an existing session after an academic change requires a separate authorized action. Discovery and `GetApprovedOffering` check live membership independently of reconciliation, so a stale active flag alone never grants entitlement. The owner profile remains able to show inactive offerings; public views omit them and private grades.

### Shared transaction integration contract

**Not implemented:** an approved end-to-end academic-program-change administration workflow that automatically invokes reconciliation. The existing Identity verification-resubmission path is not such an approved-student administration endpoint. There is no new public route or automatic trigger in this change.

A future trusted application service must receive a narrow transaction-bound Tutoring port, supplied by bootstrap/infrastructure; Identity or Catalog business logic must not import Tutoring's concrete PostgreSQL repository. The adapter factory `postgres.EligibilityInTransaction(pgx.Tx, Dependencies)` exposes the existing implementation as `application.EligibilityTransaction` without opening a pool transaction. Supply Identity's `LockStudent` and `catalog.New(catalogpostgres.InTransaction(tx))` through `Dependencies`. All operations and locks must use that exact `pgx.Tx`; never call the independent service method from inside a program-update transaction.

The future orchestrator must:

1. Begin one transaction. Acquire Identity account `FOR NO KEY UPDATE` and student lock **before** updating the program. For multiple affected accounts, acquire them in UUID order, matching Scheduling's participant order.
2. Validate and lock the proposed program using the transaction-bound Catalog interface. Authorize the academic change in its owning workflow; the internal reconciliation port does not authorize administrative requests.
3. Update the student's program within this transaction.
4. Call `RevalidateTutorEligibilityInTransaction` using the bound adapter. It rereads the current program under the account/student lock, then locks the program, offerings and matching membership rows. Tutoring application/review writers also acquire the affected student's account before offering changes.
5. Commit both the program change and deactivations together, or **roll back the entire transaction on any error**, including a late membership/deactivation error. The bound function does not commit, roll back or start a nested transaction. Retry transaction conflicts only by retrying the whole authorized use case.

A separate committed program update followed by independent reconciliation is not atomic. Current discovery/booking checks provide additional protection but do not replace this integration contract. Arbitrary SQL writers that skip the account lock are outside the contract. Existing Catalog `FOR SHARE` reads stabilize present program/membership rows; trusted catalog edits must respect their locks. Missing membership cannot grant tutoring access, and a later added membership never automatically restores a withdrawn offering.

Scheduling resolves a tutor hint, locks both accounts in UUID order, rechecks current principals and the approved offering, and locks current academic membership before inserting a booking. If the program change commits first, a waiting new booking sees the changed program/inactive offering and fails. If the booking commits first, a later program change preserves that existing request/session. Neither ordering grants permission to silently cancel an earlier booking. Keep this lock protocol when integrating future administration.

Decision notifications use the applicant's user ID and a unique decision key. The [mail worker](workers.md) resolves the university email from the recipient identity and submits the queued record to SMTP.

## Scheduling interface

Scheduling can depend on `application.OfferingEligibility` and call `GetApprovedOffering(ctx, tutorID, subjectID)` to resolve the stable offering ID. The result requires an active tutor profile, active offering, approved evidence, verified student identity, and current tutor program membership. The read returns a not-found error for a currently unavailable entitlement.

A booking workflow must additionally check the requesting student's current program through Catalog and coordinate these checks with its write transaction so that a concurrent deactivation or program change cannot invalidate a newly written booking. Existing sessions are not changed by the Tutoring module.

## Local setup and tests

Start PostgreSQL and Redis, apply the existing migrations, and run the Go API using the [Identity setup instructions](identity.md). Test the route and persistence workflows against isolated services:

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The shared integration runner executes Tutoring, Identity, and Catalog tests. The Tutoring cases exercise faculty boundaries, cross-faculty discovery, grade privacy, concurrent decisions, profile reuse, revalidation and reactivation, and transaction rollback after a failed notification insert.

Tutor search and profile responses include aggregate ratings from the [Feedback module](feedback.md). The same guide covers private feedback history and teaching activity.

### Issue #16 verification coverage

- `application/eligibility_test.go`: valid/invalid identifiers, missing student, no active offerings, all/some/no eligible subjects, idempotency/no automatic reactivation, and propagation of student/program/list/membership/deactivation errors. Repository fakes check rollback contract; real persistence rollback is covered below.
- `tests/integration/tutoring_test.go` and `tutoring_revalidation_test.go`: same-faculty change, cross-faculty shared subject preservation, owner visibility/privacy, live discovery/approved-offering exclusion, timestamp/reason, exact historical approval-evidence preservation, non-tutor/unknown student, repeated concurrent reconciliation, and stable-ID reapproval. Approval is raced against reconciliation's account lock. Forced second-offering update failure proves both independent reconciliation rollback and caller-owned program-change rollback against the migrated schema.
- `TestBookingLifecycle/program_change_and_revalidation`: PostgreSQL-observed lock waits test **both** program-change/booking orderings, rejection of later bookings, explicit reapproval, and unchanged snapshots of REQUESTED/ACCEPTED/historical sessions, participant blocks, session history, feedback, prior qualifications/applications and another tutor's offering. The program-change orchestrator is test-only; it is not an implemented production workflow.

The test suites use the actual migrations, not a simplified schema. Live SMTP/SSO, unrelated frontend work and production deployment are outside this issue.

Verification on 2026-11-19: `go test -race ./...`, `go vet ./...` and `go build ./...` from `backend`, plus `bash backend/tests/integration/test_identity.sh -race` and `python3 backend/tests/integration/test_schema.py` from the repository root all passed. Final Go runs used the working tree directly, without an overlay. The integration runner skipped its opt-in worker-container and live university SMTP probes; these are unrelated to revalidation acceptance. The schema runner verified all 31 tables, integrity/concurrency checks, all ten rollbacks and reapplication. No new migration, public program-change endpoint or automatic invocation is claimed.
