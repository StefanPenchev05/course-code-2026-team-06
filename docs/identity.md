# Identity and authentication

The Go backend's Identity module handles student registration, coordinator verification, and email/password login. It uses the existing PostgreSQL migration schema and Redis sessions. Student and tutor permissions can coexist on the same account. Tutors get tutoring access only while their student verification is approved and their tutor profile is active.

## Local setup

Use Go 1.24 or later, Docker Compose, and the PostgreSQL-enabled `migrate` CLI. Copy `.env.example` to `.env` and set `UNIVERSITY_EMAIL_DOMAINS` to the university domains permitted to sign up. Use test domains in a disposable environment. The migrated database must contain the academic catalog and any university-provisioned coordinator records before those workflows can be used.

From the repository root:

```sh
cp -n .env.example .env
# Edit .env and fill in UNIVERSITY_EMAIL_DOMAINS.
docker compose up -d --wait postgres redis
set -a
. ./.env
set +a
migrate -path backend/migrations -database "$DATABASE_URL" up
(cd backend && go run ./cmd/api)
```

| Setting | Default or requirement |
| --- | --- |
| `DATABASE_URL` | Required PostgreSQL connection URL. |
| `REDIS_URL` | `redis://127.0.0.1:6379/0`. Use an authenticated, isolated Redis instance for deployment. |
| `HTTP_ADDR` | `127.0.0.1:8080`. |
| `PUBLIC_ORIGIN` | `http://localhost:8080`. Set the exact browser origin. HTTPS enables a Secure `__Host-` cookie. Plain HTTP is accepted only for a loopback origin. |
| `UNIVERSITY_EMAIL_DOMAINS` | Required comma-separated allowlist. Domain matching is case-insensitive and exact. |
| `SESSION_TTL` | `8h`. Redis uses a fixed lifetime from login. |
| `ACTIVATION_TTL` | `24h`. |
| `ARGON2_MEMORY_KIB`, `ARGON2_ITERATIONS`, `ARGON2_THREADS` | `65536`, `3`, `2`. Stored hashes retain the parameters used to create them. |
| `ARGON2_CONCURRENCY` | `2`. Bounds the number of simultaneous password computations in one process. |

Serve the web client and API under the same public origin, for example through a reverse proxy. All writes require an exact `Origin` match, including login and logout. For local CLI requests, supply `Origin: http://localhost:8080`. Reads with an authenticated session use the cookie. JSON bodies need `Content-Type: application/json` and are limited to 16 KiB. The local Redis service is disposable and sessions are lost when it restarts.

## API

All paths are under `/api/v1`.

| Method and path | Request and result |
| --- | --- |
| `POST /auth/register` | `name`, `email`, `student_number`, `program_id`, `password`. Returns 201 with a verification ID and `PENDING`. |
| `POST /auth/login` | `email`, `password`. Returns 200, a safe profile and current permissions, and the opaque session cookie. |
| `POST /auth/logout` | Authenticated. Revokes the current Redis session and clears its cookie. Returns 204. |
| `GET /auth/me` | Authenticated. Returns profile, roles, verification status and available permissions. A rejected student also sees the rejection reason. |
| `POST /auth/resubmit-verification` | Authenticated rejected student. Send `name`, `email`, `student_number`, and `program_id`. Returns 201 with the new pending verification. |
| `POST /auth/activate-coordinator` | `token` and new `password`. Returns 204 on one-time activation. |
| `GET /coordinator/verifications` | Coordinator's pending queue. Optional `limit` (1–100, default 50) and `offset` (default 0). |
| `GET /coordinator/verifications/{id}` | Verification details from the coordinator's own faculty. |
| `POST /coordinator/verifications/{id}/approve` | Explicit approval after the coordinator checks university records. Returns 204. |
| `POST /coordinator/verifications/{id}/reject` | Non-empty `reason`. Returns 204. |

Passwords for registration and activation must be 12–256 bytes. Invalid credentials return a generic 401. Faculty-mismatched requests return 404, duplicate registration details or already-decided verifications return 409, and validation failures return 422. Authentication does not confer student permissions while verification is pending or rejected.

## Authorization and activation

The application layer owns workflow decisions. PostgreSQL transactions keep registration, resubmission, review, and password activation atomic. Coordinator queries derive faculty from the actor's current database assignment. Pending verification state is checked under a row lock before it can be decided. Resubmission keeps previous decisions and routes the new record using the selected program. The schema stores current registration details rather than snapshots. If the student moves to another faculty, the former faculty can see its old decision without seeing current registration details.

Redis stores the account ID under a key derived from the opaque cookie and automatically expires the key. The API loads current verification, roles, suspension and closure from PostgreSQL on protected requests. Future routes can compose `RequireAuthentication`, `RequireVerifiedStudent`, `RequireCoordinator`, `RequireFacultyAccess`, and `RequireSameOrigin`. Faculty middleware expects a lookup that derives the resource faculty on the server.

A trusted university provisioning process can call `application.Service.IssueActivation` with the pre-provisioned coordinator ID and an `ActivationMailer`. It stores a SHA-256 digest of a random token and hands the raw token to the mailer for that coordinator's stored address. The public activation route consumes the token and sets the password in one transaction. Once a password is set, other issued activation tokens cannot activate the account. An email delivery failure can be retried by issuing a new token. The PostgreSQL record from a failed delivery expires normally.

The integration tests supply a capture mailer. Production still needs a trusted provisioning entry point and a mail adapter, HTTPS with the configured origin, and deployment-level rate limits for public authentication requests. The module does not send production activation emails on its own.

## Tests

```sh
(cd backend && go test ./...)
backend/tests/integration/test_identity.sh
python3 backend/tests/integration/test_schema.py
```

The Identity integration runner starts isolated PostgreSQL and Redis containers, applies all existing migrations, and tests the HTTP routes and persistence. The plain Go test command runs the unit tests and skips the service-backed test unless its test connection variables are present.
