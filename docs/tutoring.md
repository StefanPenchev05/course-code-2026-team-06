# Tutoring

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
- **Discovery.** The service checks the requester's program through Catalog. A set-based PostgreSQL query checks the live membership and verification of both sides in the same read snapshot. It includes active profiles, active offerings with approved evidence, and eligible tutors from any faculty. Search covers profile and offering information. Availability, ratings, and teaching statistics belong to later modules.

The write adapters lock the user and student rows, the Catalog program and matching membership rows, and the relevant application records in the same PostgreSQL transaction. Go never needs to make a separate pool call for academic checks that govern a Tutoring write. Program changes and administrative edits should use transactions and coordinate with these locks. Discovery and the offering read port are point-in-time reads.

`RevalidateTutorEligibility(ctx, studentID)` is an internal service operation. It reads the current program under lock, deactivates offerings outside that program with a timestamp and reason, and keeps prior evidence and accepted sessions. A change back to an eligible program does not automatically reactivate an offering. There is currently no approved-student program-change endpoint. A future program-change workflow must invoke revalidation. Discovery checks current membership even before the stored offering is updated.

Decision notifications use the applicant's user ID and a unique decision key. A future mail worker can resolve the university email from the recipient identity. The module queues those records without sending mail.

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
