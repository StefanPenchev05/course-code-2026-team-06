# Academic Catalog

The internal [semester maintenance CLI](semester-maintenance.md) adds trusted,
explicit calendar creation and read-only impact/rollover reports. It is separate
from the read-only HTTP catalog below. Calendar validation/identity belong to
Catalog; transaction-bound Scheduling and Coordination adapters supply aggregate
dependency evidence. No public calendar administration endpoint is registered.

The Catalog module provides read-only faculty, program, and subject lookups. Its application service validates queries and exposes academic membership checks. The PostgreSQL adapter uses the API's existing pgx pool. Bootstrap wires the Catalog routes onto the same router as Identity and applies Identity's authentication middleware to the protected routes.

The existing migration defines `faculties`, `programs`, `subjects`, and `program_subjects`. A program belongs to one faculty. A subject has one university-wide ID and unique code. The `program_subjects` join table lets several programs point to that same subject, including programs in different faculties. A single program foreign key on `subjects` would prevent that relationship.

## API

All routes below are `GET` requests under `/api/v1`. The protected routes require a valid Tutti session cookie. Pending and rejected students can use these academic lookups while signed in.

| Route | Access | Result |
| --- | --- | --- |
| `/faculties` | Authenticated | Faculties sorted by name and ID. |
| `/faculties/{id}/programs` | Authenticated | Faculty programs sorted by name and ID. Accepts an optional `level`. |
| `/programs/{id}` | Authenticated | Program ID, name, level, faculty ID, and nested faculty details. |
| `/programs/{id}/subjects` | Authenticated | Page of canonical subjects linked to that program. |
| `/subjects` | Authenticated | Page of university-wide subject results. |
| `/subjects/{id}` | Authenticated | Canonical subject ID, code, and name. |
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
