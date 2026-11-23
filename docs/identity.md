# Identity and authentication

Issue #26 adds a transaction-bound Identity suspension-history read port, consumed
by Coordination's faculty-authorized account audit route. Verification submissions
and finalized reviews are protected from rewriting/deletion by migration 15;
resubmission creates a new attempt. See [audit preservation](audit-history.md) for
historical faculty access, legacy limits and the unresolved DPO retention gate.

> **Authentication requirement supersession:** Uni.lu-only institutional SSO is the approved target (FR1/FR23 Must; [D01](requirements-decisions.md)). Local password registration/login, Argon2id and password-based coordinator activation described here are the current legacy implementation/design, not approved release authentication. Migration remains #46 and #65–#70; protocol and routes await university confirmation. Enrollment verification remains separate and required. This documentation task changes no endpoints or schema.


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

The integration tests supply a capture mailer. The [shared SMTP transport](workers.md) can be injected as the activation mailer by trusted provisioning. Activation sends directly and keeps raw tokens out of the durable notification outbox. Production still requires a trusted provisioning entry point, SMTP configuration, HTTPS, and deployment-level limits for public authentication requests.

## Suspension administration (FR19 / UC14, Issue #25)

The [Coordination workflow](coordination.md#suspension-and-reinstatement-fr19--uc14-issue-25)
provides `POST /api/v1/coordinator/users/{id}/suspend` and `/reinstate`, each with a
required `reason`. Identity's published `SuspensionAccounts` capability owns the
locked account state, student membership and existing `user_suspension_history`;
Catalog resolves the current home faculty in the same transaction. Coordinator-only
targets without student membership have no academic home faculty and return 404.

Suspension is account-wide and has no automatic expiry. Existing login cookies and
fresh logins fail while the account is suspended because authentication reloads
PostgreSQL on every protected request. No Redis role cache or revocation index is
added. Reinstatement allows an unexpired/unrevoked cookie to resume only the roles
it currently qualifies for. Verification, tutor approvals, withdrawn offerings,
closure and previous cancellations remain independent; nothing is automatically
restored. Closed accounts and repeated transitions return conflict.

Account decisions retain actor, home faculty, both suspension states, trimmed reason
and server timestamp. Reinstatement retains last suspension metadata and appends
its own reason rather than overwriting previous evidence. Migration 14 protects the
existing history from UPDATE/DELETE/TRUNCATE and validates new reasons without
rewriting legacy rows. The account, all future session cancellations, histories and
generic outbox notices share one PostgreSQL transaction. Public profiles and logs
do not expose administrative reasons. Apply migration 14 before updated binaries;
see [database migration ordering](database/implementation.md#suspension-administration-transaction-contract).

## Tests

```sh
(cd backend && go test ./...)
backend/tests/integration/test_identity.sh
python3 backend/tests/integration/test_schema.py
```

The Identity integration runner starts isolated PostgreSQL and Redis containers, applies all existing migrations, and tests the HTTP routes and persistence. The plain Go test command runs the unit tests and skips the service-backed test unless its test connection variables are present.

## OIDC Stage 1: login initiation (Issue #65)

The optional `GET /api/v1/auth/oidc/login` endpoint now initiates authentication against the [local Keycloak demo](sso-demo.md). **Only initiation is implemented:** no callback, code exchange, token validation, account linking/creation or application-session creation occurs. Existing password routes, activation, verification, faculty checks and Redis application sessions remain unchanged. Synthetic Keycloak identity is never evidence of enrollment or a source of Tutti privileges.

Start the Keycloak provider as documented, load the existing API/database/Redis environment, then export the following into the **Go API shell**:

```sh
export PUBLIC_ORIGIN=http://localhost:8080
export OIDC_ENABLED=true
export OIDC_ISSUER_URL=http://localhost:8180/realms/tutti-demo
export OIDC_CLIENT_ID=tutti-backend
export OIDC_REDIRECT_URL=http://localhost:8080/api/v1/auth/oidc/callback
export OIDC_TRANSACTION_TTL=5m
(cd backend && go run ./cmd/api)
```

PowerShell uses `$env:OIDC_ENABLED = 'true'` and the corresponding `$env:NAME = 'value'` assignments. Compose's `.env.sso` configures Keycloak; it does not automatically export these Go variables. Stage 1 does not consume the confidential client secret because it never calls the token endpoint. Keep that secret private for Stage 2.

`OIDC_ENABLED` defaults to false, leaving the new route unregistered (404). With it enabled, issuer/client/callback configuration is required. TTL defaults to five minutes and must be whole seconds between one and ten minutes. Invalid settings fail startup. The callback must exactly equal `PUBLIC_ORIGIN + /api/v1/auth/oidc/callback`; no caller-controlled return URL is accepted. HTTP is permitted only for literal loopback/localhost development URLs; an HTTPS app requires an HTTPS issuer. Keep browser, host Go process and issuer on the same hostname contract from the demo guide. Normal bridged Go containers cannot use host `localhost` unchanged.

The provider adapter uses `coreos/go-oidc` discovery with exact issuer checking, rejects discovery redirects, and requires advertised Authorization Code and S256 support. The discovered authorization endpoint must be a query-free URL on the configured issuer's origin, an intentional restriction compatible with this Keycloak deployment. It validates discovery at startup and refreshes it on each initiation using a three-second HTTP timeout. An unavailable/invalid provider prevents initiation; no issuer-check bypass or fallback to an untrusted endpoint is used. A failure of Redis also returns generic `503` without a new cookie or redirect. Enabling OIDC makes successful discovery a startup dependency; disabling it preserves legacy startup behavior.

On successful initiation the endpoint returns `302` to the discovered authorization endpoint, containing `client_id`, fixed `redirect_uri`, `response_type=code`, `scope=openid profile email`, random `state`, random `nonce`, `code_challenge` and `code_challenge_method=S256`. It never returns a PKCE verifier or client secret. `golang.org/x/oauth2` implements PKCE generation/challenge and authorization URL encoding. Library versions are pinned to versions compatible with the existing Go 1.24 module; no toolchain upgrade or ID-token validation is included.

### Browser binding and short-lived storage

- **State:** 32 cryptographically random bytes, encoded base64url. Correlates a future response to this attempt; Redis keys use only its SHA-256 digest: `tutti:identity:oidc:<digest>`.
- **PKCE:** a fresh library-generated verifier remains only server-side in Redis; its S256 challenge travels to Keycloak. Stage 2 must supply the verifier during code exchange.
- **Nonce:** an independent 32-byte random value, stored with the transaction and sent to the provider. Stage 2 must validate the ID token's nonce. A nonce necessarily appears in the protocol authorization URL; it is never written to application logs. Proxies/observability must also redact auth/callback query strings and avoid recording redirect headers.
- **Browser binding:** another independent 32-byte random value is sent only in an HttpOnly, host-only, SameSite=Lax cookie, `tutti_oidc_tx` for loopback HTTP or `__Host-tutti_oidc_tx` for HTTPS. It has path `/`, lifetime matching the transaction, and Secure on HTTPS. Redis stores only its SHA-256 digest, not the raw cookie.

Records contain the binding digest, verifier, nonce and absolute expiry. Redis `SET NX` prevents collisions/overwrite and sets an actual TTL. They use a separate namespace from `tutti:identity:session:*` and contain no authenticated user or roles. Redis is trusted secret storage: keep it private, restrict administrative access and use the existing `rediss://` configuration when transport encryption is needed. These values are not encrypted independently inside Redis; cookie binding/state are hashed, while the verifier and nonce must remain recoverable for the future exchange/validation. No state, cookie, verifier, nonce, provider response body or client secret is logged by this implementation.

An internal transaction-store `Consume(state, browserBinding)` primitive atomically compares the binding digest and deletes a matching record using Redis Lua. Missing, expired, malformed and already-consumed states fail closed; a wrong browser cannot consume another attempt. Explicit stored expiry is checked as well. **This is tested storage support, not a callback:** Stage 2 must validate callback input, require this cookie and call Consume exactly once before exchanging a code. Merely reading a Redis record in a future callback would not provide replay protection.

Each new attempt replaces the one binding cookie, making earlier attempts in that browser unusable; unconsumed records expire automatically. This deliberately supports one outstanding attempt per browser instead of unbounded cookies. SameSite=Lax supports the planned top-level GET callback; no form-post callback is implemented. Loopback HTTP cookies lack Secure by necessity and are for synthetic development only. Cross-origin initiations carrying an explicit foreign `Origin`, or browser `Sec-Fetch-Site: cross-site`, are rejected. Ordinary address-bar/same-origin navigation without Origin is allowed. Query parameters (including return URLs) are rejected with 400; HEAD does not create transactions. Responses use `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Add the broader authentication abuse controls under #38; Stage 1 does not introduce a separate generic rate limiter.

### Verification and Stage 2 handoff

Tests include mock discovery/authorization metadata, issuer mismatch, unsafe endpoints, absent S256/code support, provider outage, random-value independence, matching PKCE/nonce, cookie attributes, forbidden origins/query redirects, safe errors/log redaction and legacy-route regression checks. The existing service-backed runner also verifies real Redis TTL expiration, wrong-browser denial, atomic replay prevention under eight concurrent consumers, and the HTTP initiation→mock discovery→Redis correlation path. The callback remains 404 throughout.

```sh
(cd backend && go test -race ./...)
(cd backend && go vet ./... && go build ./...)
bash backend/tests/integration/test_identity.sh -race
```

Stage 2 still owns callback parsing/error handling, single-use consumption and cookie clearing, code exchange with secret/verifier, ID-token validation (signature, issuer, audience, expiry and nonce), safe stable-identity binding and separately authorized account/session work. Do not enable user creation, role elevation, email-based linking or legacy-password retirement implicitly. Issue #65 as a whole is not complete from initiation alone.
