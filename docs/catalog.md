# Academic Catalog

The internal [semester maintenance CLI](semester-maintenance.md) adds trusted,
explicit calendar creation and read-only impact/rollover reports. It is separate
from the read-only HTTP catalog below. Calendar validation/identity belong to
Catalog; transaction-bound Scheduling and Coordination adapters supply aggregate
dependency evidence. No public calendar administration endpoint is registered.

The Catalog module provides read-only faculty, program, and subject lookups. Its application service validates queries and exposes academic membership checks. The PostgreSQL adapter uses the API's existing pgx pool. Bootstrap wires the Catalog routes onto the same router as Identity and applies Identity's authentication middleware to the protected routes.

The existing migration defines `faculties`, `programs`, `subjects`, and `program_subjects`. A program belongs to one faculty. A subject has one university-wide ID and unique code. The `program_subjects` join table lets several programs point to that same subject, including programs in different faculties. A single program foreign key on `subjects` would prevent that relationship.

## API

All routes below are `GET` requests under `/api/v1`. The protected routes require a valid Tutti session cookie. Pending and rejected students can use these academic lookups while signed in, except the tutor-only semester endpoint described below.

| Route | Access | Result |
| --- | --- | --- |
| `/faculties` | Authenticated | Faculties sorted by name and ID. |
| `/faculties/{id}/programs` | Authenticated | Faculty programs sorted by name and ID. Accepts an optional `level`. |
| `/programs/{id}` | Authenticated | Program ID, name, level, faculty ID, and nested faculty details. |
| `/programs/{id}/subjects` | Authenticated | Page of canonical subjects linked to that program. |
| `/subjects` | Authenticated | Page of university-wide subject results. |
| `/subjects/{id}` | Authenticated | Canonical subject ID, code, and name. |
| `/semesters` | Active verified tutor | Current/upcoming calendar, ordered by start and ID; see the contract below. |
| `/public/faculties` | Public | Same faculty list for registration. |
| `/public/faculties/{id}/programs` | Public | Same program list and optional level filter for registration. |

The level filter accepts `BACHELOR` or `MASTER`. Both subject lists accept `q`, `limit`, and `offset`. The query matches a literal case-insensitive substring of the subject name or code, up to 100 characters. Results are ordered by name, code, and ID. The default limit is 25 and the allowed range is 1–100. Offset defaults to 0 and can be at most 100,000. `has_more` indicates whether another row exists after the current page.

For example, request `GET /api/v1/subjects?q=algorithms&limit=1` with a session cookie. With the development fixture loaded, the response is:

```json
{
  "data": [
    {
      "id": "00000000-0000-0000-0000-000000000122",
      "code": "DEMO101",
      "name": "Algorithms"
    }
  ],
  "pagination": {"limit": 1, "offset": 0, "has_more": true}
}
```

A valid empty list returns `"data": []`. A malformed UUID, unknown or repeated query parameter, or malformed pagination value returns 400. An invalid filter or pagination bound returns 422. A missing session returns 401, and an unknown faculty, program, or subject returns 404. Errors use the API's `{"error":"message"}` shape.

## Use from other modules

Depend on the Catalog application interface through dependency injection:

```go
type AcademicCatalog interface {
    SubjectBelongsToProgram(ctx context.Context, programID, subjectID string) (bool, error)
    GetProgramFaculty(ctx context.Context, programID string) (string, error)
}
```

IDs use the same UUID string representation as Identity. Membership returns false when the relationship is absent, including when a well-formed ID is unknown. A faculty lookup on an unknown program returns `domain.ErrProgramNotFound`.

Tutoring can use this interface to check a prospective tutor's program before reviewing an offering. Scheduling can check both participants' programs against the same canonical subject ID, then combine those results with the Tutoring module's offering status in its booking workflow. The interface performs point-in-time reads. A caller that needs membership to stay stable throughout a write transaction will need a transaction-aware integration.

Identity retains its existing program lookup within the registration and resubmission transactions. That lookup locks the selected program while writing the student and routing the verification to a faculty. Calling a pool-backed Catalog query from that workflow would lose the shared transaction and lock.

## Local data and testing

Follow the [API setup guide](identity.md) to start the API. To try the catalog with fictional data, apply the migrations first and load the [fixture](../backend/tests/integration/catalog_fixture.sql) into a disposable development database:

```sh
psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -f backend/tests/integration/catalog_fixture.sql
curl http://localhost:8080/api/v1/public/faculties
```

The fixture is loaded explicitly. It includes one subject linked to programs in different faculties. Academic data is maintained through trusted imports. The API provides read access only.

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The Go integration runner tests both Catalog and Identity against the existing migrations and a disposable Redis instance. It loads the fixture only in the Catalog test.

Membership checks use `EXISTS` with the existing composite primary key. Program lists use the existing faculty and level index, while subject search uses parameterized substring queries and fetches one extra row per page for `has_more`. There are no new indexes. Global substring search can scan the subject catalog, and large offsets take more work. Revisit query plans as catalog size grows.

## Tutor semester picker (#99; supports #31)

`GET /api/v1/semesters` uses existing authenticated sessions and requires the
same current `CanTutor` permission as availability creation: an active, approved
student with an active tutor profile. Anonymous/expired/revoked sessions return
401; ordinary students, pending/rejected students, inactive tutors, suspended or
closed accounts and coordinator-only accounts cannot use this endpoint (403).

The response is `{"data":[{"id":"...","name":"...","starts_at":"...Z","ends_at":"...Z"}]}`.
Only those four fields are returned. The global calendar includes every current
or upcoming semester (`ends_at >` trusted server time), sorted by `starts_at`
then `id`. Exactly-ended and past semesters are excluded; overlapping configured
semesters remain visible. Timestamps are UTC RFC3339. No results returns
`{"data":[]}`. Query parameters, including client time overrides, are rejected.

This endpoint only reads the existing `semesters` table. It creates no calendar
rows, availability, history, notifications or realtime events. There is no public
variant or HTTP write endpoint; trusted calendar maintenance remains operator-only.
The frontend can use the returned ID as `semester_id` when creating weekly rules;
Scheduling still revalidates the chosen semester and generated occurrences in the
write transaction. No migration is required for this API. The current frontend lists/edits/deletes weekly rules
but has no weekly-rule creation form or semester fetch adapter. Issue #31 must add
a selector using `data[].id` (UUID string) as `semester_id`, display the name and
bounds, and handle loading, errors and an empty calendar. The existing one-off form
is separate. Backend readiness does not imply frontend selector integration.

The HTTP application method `CalendarService.ListForTutor` delegates to the same
`CalendarService.List` use case and `CalendarRepository.CalendarTransaction`
used by the CLI, in a repeatable-read, read-only transaction. It filters the
returned chronological listing at the receipt's trusted `action_at` timestamp;
there is no second semester SQL query or repository interface. The CLI continues
to list all semesters and retain its existing receipt metadata. IDs remain UUID
strings, matching the existing schema; numeric IDs in illustrative examples are
not the repository's ID format. Successful reads use 200; malformed query
parameters use 400; authentication/permission failures use 401/403, and unexpected
infrastructure errors use the existing sanitized 500 response.

The scoped [OpenAPI 3 contract](openapi/catalog.yaml) and
[issue #99 verification matrix](semester-api-verification.md) record the API and test evidence.
