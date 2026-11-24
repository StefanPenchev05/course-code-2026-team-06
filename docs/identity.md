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

Authenticated profile responses (`GET /api/v1/auth/me` and successful legacy login) include `program_id` for users with a student role, including tutors and pending/rejected students. It comes from the current server-loaded student record and is omitted for coordinator-only accounts. A client can use it with `GET /api/v1/programs/{id}/subjects` to offer subjects in the student's program. It is a UI hint, not authorization: discovery/booking still enforce current membership server-side.

Client-disconnected operations that return `context.Canceled` (including wrapped errors) exit without an error JSON write or an error-level request-failure log only when the request context is also cancelled. Genuine server errors, independently cancelled operations on live requests, and timeouts retain their existing error behavior. The same narrow cancellation handling applies to Catalog and Tutoring handlers.

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

The optional `GET /api/v1/auth/oidc/login` endpoint now initiates authentication against the [local Keycloak demo](sso-demo.md). **Stage 1 initiates the flow; [Stage 2 below](#oidc-stage-2-callback-and-verified-provider-identity) now handles the callback, exchange and ID-token verification.** Stage 3A adds the internal persistent binding described below; callback account resolution, automatic account creation and application-session creation remain unimplemented. Existing password routes, activation, verification, faculty checks and Redis application sessions remain unchanged. Synthetic Keycloak identity is never evidence of enrollment or a source of Tutti privileges.

Start the Keycloak provider as documented, load the existing API/database/Redis environment, then export the following into the **Go API shell**:

```sh
export PUBLIC_ORIGIN=http://localhost:8080
export OIDC_ENABLED=true
export OIDC_ISSUER_URL=http://localhost:8180/realms/tutti-demo
export OIDC_CLIENT_ID=tutti-backend
export OIDC_REDIRECT_URL=http://localhost:8080/api/v1/auth/oidc/callback
export OIDC_TRANSACTION_TTL=5m
# After privately loading your local Keycloak secret into this shell:
export OIDC_CLIENT_SECRET="$TUTTI_OIDC_CLIENT_SECRET"
(cd backend && go run ./cmd/api)
```

PowerShell uses `$env:OIDC_ENABLED = 'true'` and the corresponding `$env:NAME = 'value'` assignments. Compose's `.env.sso` configures Keycloak; it does not automatically export these Go variables. Stage 2 requires `OIDC_CLIENT_SECRET`, privately supplied with exactly the value configured as Keycloak’s `TUTTI_OIDC_CLIENT_SECRET`. For PowerShell use `$env:OIDC_CLIENT_SECRET = $env:TUTTI_OIDC_CLIENT_SECRET` after privately loading it. A missing secret fails startup; never put it into frontend configuration, screenshots or logs.

`OIDC_ENABLED` defaults to false, leaving the new route unregistered (404). With it enabled, issuer/client/callback/client-secret configuration is required. TTL defaults to five minutes and must be whole seconds between one and ten minutes. Invalid settings fail startup. The callback must exactly equal `PUBLIC_ORIGIN + /api/v1/auth/oidc/callback`; no caller-controlled return URL is accepted. HTTP is permitted only for literal loopback/localhost development URLs; an HTTPS app requires an HTTPS issuer. Keep browser, host Go process and issuer on the same hostname contract from the demo guide. Normal bridged Go containers cannot use host `localhost` unchanged.

The provider adapter uses `coreos/go-oidc` discovery with exact issuer checking, rejects discovery redirects, and requires advertised Authorization Code and S256 support. The discovered authorization endpoint must be a query-free URL on the configured issuer's origin, an intentional restriction compatible with this Keycloak deployment. It validates discovery at startup and refreshes it on each initiation using a three-second HTTP timeout. An unavailable/invalid provider prevents initiation; no issuer-check bypass or fallback to an untrusted endpoint is used. A failure of Redis also returns generic `503` without a new cookie or redirect. Enabling OIDC makes successful discovery a startup dependency; disabling it preserves legacy startup behavior.

On successful initiation the endpoint returns `302` to the discovered authorization endpoint, containing `client_id`, fixed `redirect_uri`, `response_type=code`, `scope=openid profile email`, random `state`, random `nonce`, `code_challenge` and `code_challenge_method=S256`. It never returns a PKCE verifier or client secret. `golang.org/x/oauth2` implements PKCE generation/challenge and authorization URL encoding. Library versions are pinned to versions compatible with the existing Go 1.24 module; no toolchain upgrade is needed. Stage 2 adds ID-token verification using the already installed OIDC library.

### Browser binding and short-lived storage

- **State:** 32 cryptographically random bytes, encoded base64url. Correlates a future response to this attempt; Redis keys use only its SHA-256 digest: `tutti:identity:oidc:<digest>`.
- **PKCE:** a fresh library-generated verifier remains only server-side in Redis; its S256 challenge travels to Keycloak. Stage 2 supplies the verifier during code exchange.
- **Nonce:** an independent 32-byte random value, stored with the transaction and sent to the provider. Stage 2 validates the ID token’s nonce. A nonce necessarily appears in the protocol authorization URL; it is never written to application logs. Proxies/observability must also redact auth/callback query strings and avoid recording redirect headers.
- **Browser binding:** another independent 32-byte random value is sent only in an HttpOnly, host-only, SameSite=Lax cookie, `tutti_oidc_tx` for loopback HTTP or `__Host-tutti_oidc_tx` for HTTPS. It has path `/`, lifetime matching the transaction, and Secure on HTTPS. Redis stores only its SHA-256 digest, not the raw cookie.

Records contain the binding digest, verifier, nonce and absolute expiry. Redis `SET NX` prevents collisions/overwrite and sets an actual TTL. They use a separate namespace from `tutti:identity:session:*` and contain no authenticated user or roles. Redis is trusted secret storage: keep it private, restrict administrative access and use the existing `rediss://` configuration when transport encryption is needed. These values are not encrypted independently inside Redis; cookie binding/state are hashed, while the verifier and nonce must remain recoverable for the future exchange/validation. No state, cookie, verifier, nonce, provider response body or client secret is logged by this implementation.

An internal transaction-store `Consume(state, browserBinding)` primitive atomically compares the binding digest and deletes a matching record using Redis Lua. Missing, expired, malformed and already-consumed states fail closed; a wrong browser cannot consume another attempt. Explicit stored expiry is checked as well. **Stage 2 now uses this storage primitive:** it validates callback input, requires this cookie and calls Consume exactly once before exchanging a code. Merely reading a Redis record in a future callback would not provide replay protection.

Each new attempt replaces the one binding cookie, making earlier attempts in that browser unusable; unconsumed records expire automatically. This deliberately supports one outstanding attempt per browser instead of unbounded cookies. SameSite=Lax supports the planned top-level GET callback; no form-post callback is implemented. Loopback HTTP cookies lack Secure by necessity and are for synthetic development only. Cross-origin initiations carrying an explicit foreign `Origin`, or browser `Sec-Fetch-Site: cross-site`, are rejected. Ordinary address-bar/same-origin navigation without Origin is allowed. Query parameters (including return URLs) are rejected with 400; HEAD does not create transactions. Responses use `Cache-Control: no-store` and `Referrer-Policy: no-referrer`. Add the broader authentication abuse controls under #38; Stage 1 does not introduce a separate generic rate limiter.

### Initiation verification

Tests include mock discovery/authorization metadata, issuer mismatch, unsafe endpoints, absent S256/code support, provider outage, random-value independence, matching PKCE/nonce, cookie attributes, forbidden origins/query redirects, safe errors/log redaction and legacy-route regression checks. The existing service-backed runner also verifies real Redis TTL expiration, wrong-browser denial, atomic replay prevention under eight concurrent consumers, and the HTTP initiation→mock discovery→Redis correlation path. The enabled callback now returns 400 for missing/invalid input, and successful protocol verification is tested separately below.

```sh
(cd backend && go test -race ./...)
(cd backend && go vet ./... && go build ./...)
bash backend/tests/integration/test_identity.sh -race
```

Stage 2 is described below. Stage 3A provides persistent internal binding; Stage 3B must integrate callback account resolution and separately authorized application-session creation. Do not enable user creation, role elevation, email-based linking or legacy-password retirement implicitly. Issue #65 as a whole is not complete from protocol verification alone.

## OIDC Stage 2: callback and verified provider identity

`GET /api/v1/auth/oidc/callback` is registered alongside initiation only when `OIDC_ENABLED=true`. It accepts a normal IdP top-level redirect without requiring Origin or rejecting cross-site fetch metadata. SameSite=Lax cookies, state and browser binding provide correlation; no domain/email/role claim grants application permissions.

The handler strictly parses a bounded query (8 KiB total, 4 KiB per value), rejects duplicate parameters, bad encoding, control characters, unknown parameters, invalid/missing state, missing/empty code, simultaneous code/error and duplicate/missing browser cookies. It accepts standard `session_state`, `iss`, `error_description` and `error_uri` parameters but never fetches or reflects provider error URLs/descriptions. An `iss` response parameter, when supplied, must match the configured issuer. Only GET is accepted; HEAD does not consume a transaction.

The application `OIDCCallback.Complete(ctx, OIDCResponse, browserBinding)` calls the existing atomic Redis Consume operation before any network exchange. Wrong-browser, missing, expired and replayed state fail closed. A wrong-browser or malformed request never consumes the valid transaction or clears another outstanding attempt's cookie. After a matching valid transaction is consumed, the handler clears exactly the temporary cookie (same name/path/host/Secure/HttpOnly/SameSite settings), including on provider denial, exchange failure or rejected ID token. Expired/missing transactions are not treated as matches; their cookies naturally expire. A consumed attempt cannot be retried: the user must start login again.

The confidential exchange uses `golang.org/x/oauth2` with the original PKCE verifier, exact registered callback and HTTP Basic client authentication. Authentication style is fixed to avoid automatic credential-method probing/retries. Discovery, token and JWKS HTTP calls use a three-second timeout and reject HTTP redirects; request cancellation is respected. Token and JWKS endpoints must be valid URLs on the exact configured issuer origin, preventing accidental client-secret disclosure to a different host. Discovery is checked at startup; the verified startup provider/JWKS endpoint is retained for exchange and verification. Signing-key rotation at that JWKS URL uses the library's cached key set and refresh-on-unknown-key behavior. Endpoint/issuer changes require a deliberate configuration update/restart.

A valid token response must contain a nonempty access token, explicit Bearer token type and a bounded string ID token. Tokens remain request-local and are never persisted or returned. OIDC verification uses `coreos/go-oidc` with signature, issuer, audience and expiration checks enabled, and an explicit **RS256-only** signing policy matching the demo realm. The adapter additionally enforces:

- Exact issuer equality and a nonblank subject of at most 255 bytes, retained unchanged as an opaque identifier.
- Required `iat`, future-issued/not-before skew limited to 60 seconds, expiry strictly in the future and later than issuance, and sensible `nbf`/expiry ordering. This is stricter than the library's default five-minute `nbf` leeway.
- The retained nonce, using constant-time comparison.
- `azp` equal to this client whenever supplied and mandatory for multiple audiences. No additional audiences have been approved, so an audience other than this client is rejected even with matching `azp`; multi-resource audience support would need an explicit future trust policy.
- JOSE `typ`, when supplied, must be `JWT`; Keycloak's payload `typ`, when supplied, must be `ID`. Access-token types are rejected. OIDC does not require these optional type fields, so their absence alone does not fail a valid ID token. The `id_token` response field, signature, nonce and other checks remain mandatory.
- An optional `at_hash` must match the returned access token.

Provider-denied or invalid authentication returns generic 401; malformed/unmatched correlation returns 400; Redis/discovery/token-transport failure returns generic 503. No provider body, raw error, token, subject, code, state, nonce, verifier or secret is included in response/log messages. Responses use no-store and no-referrer and never redirect to an error-supplied URL. The protocol necessarily brings code/state in the callback URL: this JSON development response does not erase browser history. Configure proxies/access logs to redact query strings and response cookies; Stage 3 should finish with a fixed safe post-login redirect. Nothing is written to localStorage/sessionStorage.

Successful callbacks return only:

```json
{"status":"oidc_identity_verified","message":"OIDC identity verified; Tutti account linking and sessions are not implemented."}
```

### Stage 3 boundary

The trusted provider adapter returns `domain.VerifiedIdentity{Issuer, Subject}` through `application.OIDCExchange.ExchangeAndVerify`. After single-use browser correlation, `application.OIDCCallback.Complete` returns that identity in `OIDCCallbackResult.Identity` and sets `Consumed` so the HTTP layer can clear the cookie. The current handler deliberately does not serialize or persist the identity. Stage 3A now provides internal resolution/provisioning below. Stage 3B must integrate resolution **after** this successful result, using the exact `(issuer, subject)` key. It must not construct a `Principal` or infer enrollment, tutor qualification, coordinator faculty or suspension state from Keycloak claims.

There are no PostgreSQL writes/migrations, application-session Redis writes, application cookies, account-linking changes, React changes or legacy-login removal in Stage 2. A browser with no prior Tutti session still receives 401 from `/api/v1/auth/me` after OIDC success. An already-existing independent legacy session is not changed by this protocol-only callback.

### Stage 2 verification

Mock-provider tests use test RSA signing keys and cover valid exchange/callback, client authentication/PKCE/redirect matching, missing/invalid responses, claim/signature/type/algorithm rejection, issuer/audience/authorized-party/nonce/time checks, key rotation, provider denial, transport failure/cancellation, strict parameter/cookie parsing, privacy and single-use concurrency. The service-backed suite combines actual Redis with HTTP initiation/callback and a signed mock provider; exactly one concurrent callback succeeds and no Tutti session is created. Existing authentication and cancellation regressions remain in the suite.

Use the Go and integration commands above. For local Keycloak, configure the exact callback/client secret, create only a synthetic user as in the [demo guide](sso-demo.md), start the API, then navigate to `/api/v1/auth/oidc/login` in the same browser. Expected result after Keycloak sign-in is the development JSON above; `/api/v1/auth/me` remains unauthenticated without an independent legacy session. This is not real Uni.lu identity or academic verification.

## OIDC Stage 3A: persistent identity binding

Migration [000016](../backend/migrations/000016_oidc_identities.up.sql) adds `oidc_identities` with UUID `id`, `user_id`, exact `issuer`, exact `subject` and server-generated `created_at`. Unique constraints enforce `(issuer, subject)` and one binding per user for the single-provider MVP. C collation makes comparison byte-exact and case-sensitive. A restrictive foreign key preserves account referential integrity without cascade deletion. Bounded, nonempty values are checked in SQL, with trusted issuer/UTF-8/control-character validation in the application. No token, password, assertion, email, name, role or academic claim is stored in this table.

`issuer + subject` is the stable identity key. Subject strings are opaque and retained unchanged. Names/emails can change, be recycled or be unverified; matching either to an account does not prove ownership. Neither this resolver nor provisioner accepts email, student number or Keycloak roles as linking inputs.

### Resolution capability

`application.NewOIDCIdentityResolver(reader OIDCIdentityReader, trustedIssuer)` validates the configured trusted issuer using the same HTTPS/loopback policy as OIDC. `ResolveOIDCIdentity(ctx, domain.VerifiedIdentity)` accepts **only a server-side identity already verified by Stage 2**, rejects an incoming issuer different from that configuration, and performs an exact pair lookup. Do not deserialize `VerifiedIdentity` from a browser as proof of authentication: the Go type is a trusted internal contract, not a cryptographic certificate.

`postgres.Repository.LookupOIDCIdentity` uses a read-only repeatable-read transaction to read the mapping and existing Identity `readPrincipal` projection from one snapshot. It does not rewrite mapping timestamps or any account data. Returned roles, academic program, verification, active tutor profile, faculty, suspension and closure are current PostgreSQL authority, independent of IdP claims. An active pending/rejected student can be resolved, matching existing login behavior, but `VerifiedStudent()` and `CanTutor()` remain false. An inactive tutor profile does not grant tutor privileges. Coordinator authority comes only from Tutti's coordinator row. Suspended, closed or roleless accounts return `ErrForbidden` with an empty principal.

Unknown exact pairs return `domain.ErrOIDCIdentityUnlinked` (also matching `ErrNotFound`), not a newly registered account. Invalid fields/untrusted issuers return `ErrInvalid`; database/cancellation failures remain errors and never become an authenticated principal. Resolution is a point-in-time result, not a durable authorization grant. Future session establishment must recheck account eligibility at its authorization boundary; existing protected requests must continue reloading current permissions.

### Trusted provisioning capability

`application.NewTrustedOIDCProvisioner(writer OIDCIdentityWriter, trustedIssuer)` exposes `LinkOIDCIdentity(ctx, existingUserID, verifiedIdentity)` to trusted **internal setup only**. It is deliberately a separate capability from the resolver. No HTTP endpoint, public administrator route, CLI, account-linking screen or callback-selected user ID is added. The current callback still returns its Stage 2 development response; it does not resolve, link or authenticate a Tutti user.

A future authorized linking workflow must establish both ownership proofs: the external identity must come from successful Stage 2 verification, and the choice of existing Tutti account must come from a trusted provisioning record or an explicitly authorized ownership-confirmation workflow. Simply knowing a user ID is not permission to link it. For this course stage, integration fixtures serve as trusted setup for existing synthetic users. Do not hand the provisioning capability to the ordinary callback or choose a user ID from an IdP claim/query string. A broader administrator/linking workflow remains separate work.

The PostgreSQL writer locks the target `users` row with `FOR NO KEY UPDATE`, consistent with Identity suspension/program-change locking, and rechecks the existing principal **inside** the transaction. It allows an existing active student (including restricted pending/rejected students) or an active coordinator, and rejects missing, suspended, closed and roleless accounts. It writes only the new mapping; it never changes credentials, verification, academic data, profiles, offerings, roles or history.

`INSERT ... ON CONFLICT DO NOTHING` plus both unique constraints resolves concurrent identity/account collisions. After a conflict, only an already-existing **exact same pair and account** succeeds idempotently. Its original row ID/creation timestamp remain unchanged. A pair already attached to another account, or an account already attached to another pair, returns `ErrConflict`; nothing is reassigned. Unknown targets return `ErrNotFound`. SQL failures roll the transaction back, and an account-status change that obtains the account lock first is rechecked before any insert. There is no relink/unlink/update/delete capability or automatic reactivation.

A trusted composition root or test fixture can construct the two capabilities without changing Identity's existing service:

```go
repository := postgres.New(pool)
resolver, err := application.NewOIDCIdentityResolver(repository, configuredIssuer)
// Give only resolver to future authenticated callback orchestration.

provisioner, err := application.NewTrustedOIDCProvisioner(repository, configuredIssuer)
// Give provisioner only to explicitly trusted setup; never public HTTP input.
```

These capabilities do not need Redis, a session store or password service. Constructors alone perform no binding/database writes. Stage 3A does not add a production provisioning entry point or automatically wire the resolver into the callback.

### Migration, reset and recovery

Apply migration 16 before code begins using persistent bindings. It is additive and leaves all existing accounts, authorization rows and historical evidence unchanged. The [down migration](../backend/migrations/000016_oidc_identities.down.sql) drops only an **empty** mapping table and refuses to silently remove populated login associations. Production rollback/retirement of populated bindings requires an explicit reviewed plan, not forced deletion; migration privileges should remain separate from ordinary runtime access. On disposable databases, tests remove only their own synthetic mappings before testing rollback/reapply.

Resetting the demo Keycloak realm can generate new subjects/signing keys. The same issuer/name/email does not make a new subject the old user: it remains unlinked. A changed issuer similarly creates a different identity key. Preserve current bindings and require an approved, trusted reconciliation/recovery workflow; neither email-based relinking nor overwrite-on-conflict is provided here. Do not reset a shared IdP assuming Tutti accounts will reconnect automatically.

### Stage 3B handoff and verification

After successful `OIDCCallback.Complete`, Stage 3B should call `resolver.ResolveOIDCIdentity(ctx, result.Identity)` before any application authentication decision. It must handle unlinked and disabled users safely, preserve pending/rejected restrictions, and establish sessions only through a separately authorized Identity workflow with fresh account checks. Unknown identities must not trigger implicit registration or email matching. Stage 3A itself creates no Redis application session, sets no application cookie, and leaves the Stage 2 success response unchanged. Session creation, browser account-linking/onboarding and safe post-login redirects remain future work.

Application tests cover trusted issuer/field validation, unknown/failure handling and unchanged principal permissions. Real PostgreSQL tests cover both unique constraints under concurrent attempts, exact idempotence, mutable email/name, disabled accounts, existing student/tutor/coordinator state, account-close races and rollback after insert failure. Snapshots verify no changes to existing user/role/qualification/session/feedback/history records, while Redis checks verify no application sessions are issued. The schema runner checks migration 16 up/down/reapply, uniqueness, restrictive FK, existing-data preservation and its populated-table rollback guard on a disposable database.

Stage 3A verification on 2026-11-23: `go test -race ./...`, `go vet ./...` and `go build ./...` passed from `backend`; `bash backend/tests/integration/test_identity.sh -race` and `python3 backend/tests/integration/test_schema.py` passed from the repository root. The schema runner verified all 34 tables, migration 16 up/down/reapply and its populated-table guard. The service-backed suite skipped only the unrelated opt-in worker-container and live university SMTP probes. No production migration or live IdP provisioning was performed for Stage 3A; Stage 2 cryptography and its existing callback behavior were preserved and covered by regression tests.

## Realtime account access (#88)

Migration 18 captures existing verification, membership/program, coordinator
assignment, tutor role, suspension/reinstatement and closure changes. The outbox
retains the affected account only as a private routing target; public
`account.access_changed` has no `entityId`. Only that account is a candidate;
coordinator reviews use separate scoped case events. Changed permissions can
close the socket before any final event. The existing 15-second revalidation
interval, 3-second check budget and per-event checks are unchanged. Reconnect
must refetch `/api/v1/auth/me` and authorized visible data. See [realtime
delivery](realtime-delivery.md). No new account/SSO/program administration API
is introduced.
