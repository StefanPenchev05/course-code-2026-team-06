# Issue #65 — integrated OIDC authentication evidence

## Task / issue and inspected implementation

Goal: FR1 / FR23 / UC01 / NFR1–NFR3 backend OIDC authentication using the
**simulated** Keycloak provider established by #46. This is not official
University of Luxembourg SSO, enrollment approval or a production deployment.

Started on `main` at `2aebdd5` with a clean tree. Read root AGENTS.md, related
issues #46 / #65–#70, Identity domain/application/adapters/HTTP, Redis sessions,
bootstrap/configuration, migration 16 and existing constraints, OIDC security
and real-service tests, Keycloak config, README and Identity/demo guides.

Already present: validated configuration, library-backed discovery/code flow/S256,
cryptographic state/nonce/browser binding, short-lived atomic Redis transactions,
code exchange and strict RS256 ID-token validation with key rotation, exact
issuer/subject resolution, trusted internal binding and uniqueness constraints.
The main gap was integration: the callback returned protocol-only JSON instead
of resolving an account, rotating a session and redirecting. Resource limiting,
configurable bounded provider timeout/post-login destination and final session
and browser evidence were also missing.

No Git write operation was performed by the implementing agent. Other activity
advanced the checkout through `feafbf2`, `3a29bbe` and `979771f` during work; those
commits were preserved. Final working-tree status is reported in the conversation.
No branches were created/switched, issues closed, or merges performed by the agent.

## Changes and architectural decisions

- `application/oidc_authentication.go` composes the existing protocol callback,
  resolver reader and existing Identity Service. Only verified identities reach
  resolution, and no provisioning writer reaches the callback. It rechecks the
  current Principal before issuing an opaque session. Unknown/conflicting accounts
  receive generic authentication failure; suspended/closed/roleless accounts fail.
- `Sessions.Rotate` and the existing Redis adapter atomically create a fresh
  hashed-key session and revoke the previous browser session. Collisions preserve
  existing keys. Stored content remains only the Tutti UUID with fixed TTL.
  Legacy login/registration and local logout retain their existing behavior.
- HTTP callback clears consumed transaction cookies, sets existing application
  cookie attributes and returns 303 to a validated fixed path. It never serializes
  the provider identity/tokens. Duplicate application cookies are rejected before
  consumption, and errors remain generic/no-store/no-referrer.
- Bootstrap wires the resolver and session workflow. `OIDC_HTTP_TIMEOUT` defaults
  to 3s (whole seconds 1–10); `OIDC_POST_LOGIN_PATH` defaults to `/` and accepts only
  a bounded safe same-origin path. Invalid enabled configuration fails startup;
  `OIDC_ENABLED=false` remains the explicit legacy migration mode.
- No existing HTTP rate limiter was available. A narrow Redis limiter bounds
  initiation to 10 attempts per TCP peer and 120 globally per minute, across API
  replicas. It ignores untrusted proxy headers, expires counters, and fails closed.
- Existing provider cryptography/protocol libraries and identity-binding schema
  are reused. No dependency, migration, business role system, scheduling rule,
  frontend screen or public linking endpoint was added.

The eligibility recheck and Redis write do not form a distributed transaction.
Every protected request reloads current PostgreSQL authorization; a concurrent
or later suspension/role/faculty change therefore cannot grant stale access. A
lost Redis acknowledgement can leave an unreturned session until expiry or revoke
an older session without issuing a new cookie. The callback never reports success
on storage failure, and its consumed state cannot be retried. See the fuller
[lifecycle and trade-offs](identity.md#oidc-stage-3b-authenticated-tutti-sessions).

## Changed-file inventory

All backend paths below are relative to `backend/`:

| Area | Files |
| --- | --- |
| Application workflow/contracts | `internal/identity/application/oidc_authentication.go`, `oidc.go`, `service.go` |
| Application tests | `internal/identity/application/oidc_authentication_test.go`, `oidc_test.go` |
| Infrastructure | `internal/identity/infrastructure/oidc/provider.go`, `internal/identity/infrastructure/redis/oidc_limit.go`, `session_store.go` |
| HTTP and handler tests | `internal/identity/interfaces/http/oidc.go`, `oidc_callback.go`, `oidc_test.go`, `oidc_callback_test.go` |
| Composition/config tests | `internal/bootstrap/identity.go`, `oidc.go`, `oidc_test.go` |
| Real-service regression tests | `tests/integration/oidc_callback_test.go`, `oidc_session_test.go`, `oidc_test.go` |
| Real provider/browser harness | `tests/integration/oidc_keycloak_test.go`, `keycloak_browser.mjs`, `test_oidc_keycloak.py` |
| Root/docs | `.env.example`, `README.md`, `docs/identity.md`, `docs/sso-demo.md`, `docs/openapi/identity-oidc.yaml`, this report |

## Final HTTP and trust contract

| Route | Success / failures |
| --- | --- |
| `GET /api/v1/auth/oidc/login` | 302 to trusted discovery URL plus temporary binding cookie. Queries/return URLs 400; disallowed origin 403; rate limit 429 with Retry-After 60; unavailable provider/Redis 503. |
| `GET /api/v1/auth/oidc/callback` | Valid signed identity + existing eligible binding: rotated opaque session cookie, consumed temporary-cookie deletion, 303 fixed configured path. Malformed/wrong-browser/expired/replayed state 400; denied/rejected/unlinked/conflicting identity 401; ineligible account 403; transaction/lookup/session infrastructure failure 503. |
| `GET /api/v1/auth/me` | Existing Redis session plus current PostgreSQL Principal; provider JWTs are not used for ordinary requests. |
| `POST /api/v1/auth/logout` | Existing authenticated exact-Origin-protected local logout, 204 after revocation and cookie clearing. Provider browser session remains separate; no new post-logout redirect or ID-token retention is introduced. |

The existing cookie names, HttpOnly/SameSite=Lax/path `/`, HTTPS Secure/`__Host-`
behavior and `SESSION_TTL` remain authoritative. Provider tokens stay request-local,
while Redis application keys store hashes of fresh opaque tokens, not raw tokens.
A failed OIDC attempt never calls password login. Existing unrelated sessions are
not an implicit fallback. App permissions come exclusively from Tutti's database.

The default provider contract remains issuer
`http://localhost:8180/realms/tutti-demo`, client `tutti-backend`, callback
`http://localhost:8080/api/v1/auth/oidc/callback`, scopes `openid profile email`.
Use the same local secret as Keycloak's `TUTTI_OIDC_CLIENT_SECRET` in Go's private
`OIDC_CLIENT_SECRET`. Keep issuer validation enabled. The [scoped OpenAPI](openapi/identity-oidc.yaml)
describes parameters, cookies, redirects, status codes and local logout.

## Commands and results (2026-11-26)

| Executed check | Result |
| --- | --- |
| `gofmt -w` changed Go files; tracked files checked with `gofmt -l` | Passed, no outstanding formatting |
| `(cd backend && go test -race ./...)` | Passed; service tests separately exercised below |
| `(cd backend && go vet ./...)` | Passed |
| `(cd backend && go build ./...)` | Passed |
| `bash backend/tests/integration/test_identity.sh -race` | Passed: 30 top-level real-service tests, including existing legacy-auth/business regressions |
| `bash backend/tests/integration/test_identity.sh -race -run '^TestOIDC'` | Passed on the final OIDC test changes; opt-in browser entry skipped here |
| `python3 backend/tests/integration/test_oidc_keycloak.py` | Passed against actual Keycloak + production Go bootstrap + PostgreSQL/Redis + Chromium; six journeys below |
| `python3 backend/tests/integration/test_schema.py` | Passed: 35 tables, constraints, concurrency, preservation guards, rollback and reapply |
| `node --check backend/tests/integration/keycloak_browser.mjs`; Python harness syntax compilation | Passed |
| OpenAPI YAML parse, route set and local-reference checks | Passed; not a full OpenAPI validator |
| Documentation relative links and `git diff --check` | Passed |

The standard runner intentionally skips the opt-in Keycloak test; its separate
command above was executed. It also skips opt-in 200-user realtime load, live
university SMTP and the separate runtime worker-image smoke test. Subprocess
helper entries skip as standalone tests while their owning process tests run.
Those broader optional suites were not rerun for OIDC. No live university or
external email was sent; notification regression tests use local fixtures.
Frontend unit/fake-API suites were not changed or rerun; no React SSO integration
or multi-engine/mobile browser coverage is claimed. Hosted CI and human security
review remain pending.

Initial development checks exposed stale protocol-only 200/no-session assertions,
a malformed-callback assertion temporarily left in an initiation-only fixture,
an invalid generated realtime namespace in the browser harness, and an incorrectly
selected same-faculty fixture for the new cross-faculty check. Those tests were
adapted/corrected; no application restrictions were relaxed. The expanded final
browser and regression runs passed.

## Real Keycloak browser evidence

The opt-in runner creates a fresh `tutti-oidc-browser-*` provider project with
random private credentials. It refuses an occupied provider port, uses the
existing integration runner's disposable PostgreSQL/Redis and migrations, and
provisions only synthetic `.example` identities. Trusted internal provisioning
links five pre-existing Tutti fixture accounts. An unknown provider user remains
unlinked. No public account-binding capability is exposed.

The test starts the actual `bootstrap.Run` on an available loopback port. Only
this owned client's callback/origin are changed to that **exact** test address;
the committed #46 localhost:8080 contract remains unchanged. The browser navigates
to the backend initiation endpoint and completes the real Keycloak login form.
Its configured success destination is `/api/v1/auth/me`, allowing API-only browser
verification without implementing #68 UI.

| Real Chromium journey | Observed result |
| --- | --- |
| Approved tutor with an existing session | 303; fresh HttpOnly application cookie; old session gets 401; `/auth/me` and tutor-only semesters succeed; local logout revokes new session |
| Pending student, despite provider verified-email claim | Session created, `verified_student=false`, student role only, tutor/coordinator APIs 403 |
| Existing coordinator | Database faculty/role preserved, own queue accessible, other faculty's verification returns 404, tutor-only semester API 403 |
| Suspension after successful SSO | Existing application session immediately receives 403 after authorized home-faculty suspension through the real API |
| Unknown provider identity | Generic callback 401; no application cookie/session |
| Already suspended linked tutor | Generic callback 403; no application cookie/session |

Consumed temporary cookies clear, application localStorage/sessionStorage remain
empty, and no browser exceptions were observed. No password, callback URL, raw
identity, token, cookie or trace is printed by the browser helper. The wrapper
cleans its own provider volume/network/container and the integration runner cleans
its own PostgreSQL/Redis. Existing developer services remain running; no test
provider is left on port 8180. Synthetic identity binding is test setup, not a
new production migration/onboarding workflow or institutional approval.

## Acceptance matrix

| Issue #65 criterion | Evidence / status |
| --- | --- |
| Existing OIDC inspected/reused | Existing protocol/provider/binding components retained; new composition only |
| Environment-based validated configuration | Existing validation plus timeout/destination/bootstrap tests; enabled invalid settings fail |
| Authorization Code + PKCE S256 | Existing library/security tests and six real Keycloak browser flows |
| State/nonce/browser binding | Existing protocol/HTTP tests and real Redis wrong-browser tests |
| Short-lived, single-use transactions | Redis TTL/expiry and atomic concurrent consume/replay tests |
| Signature, issuer, audience, expiry | Existing mock signing-key/claims/algorithm failure suite passed |
| Signing key rotation | Existing rotated-kid provider test passed; no verification bypass |
| Trusted identity resolves eligible account | Exact issuer/subject PostgreSQL reader and real browser bindings |
| Unknown/conflicting identities rejected | Binding uniqueness/race tests; application conflict mapping; live unknown-user 401 |
| No claim roles, auto merge or registration | Reader-only callback, local-role assertions and restricted verified-email student |
| Opaque Redis application session | New workflow plus real HTTP/browser authenticated reads |
| Fixation and replay prevention | Atomic rotation/collision preservation, old cookie 401, one concurrent callback succeeds |
| Student/tutor/coordinator authorization | Full existing regression suite plus current-profile/API browser checks |
| Suspension cannot bypass restrictions | Resolver/current-Principal tests, live pre-suspended rejection and post-login suspension blocking |
| No provider tokens in browser storage | Request-local provider token handling, no JSON token response, empty app browser storage |
| Provider/infrastructure failures safe | Existing transport/timeout tests; real PostgreSQL lookup fault and closed Redis rotation return 503 with no session |
| Legacy authentication compatible | Unchanged routes and full legacy regression fixtures pass; explicit coexistence until #67 |
| Mock provider security tests | Race-enabled Go suite passed, including invalid claims/signatures/types and key rotation |
| PostgreSQL/Redis integration | Full 30-test suite plus final focused suite passed |
| Local Keycloak verification | Six real Chromium journeys passed; no fake API or issuer/TLS verification bypass |
| API/development documentation | Updated Identity/demo guides, environment example, README, OpenAPI and this handoff |
| No official university claims | Simulated-provider scope explicit; formal requirement changes remain #70 |

Implementation and local verification satisfy this backend slice. Hosted CI,
human review and related issue rollout are not implied by these local results;
issue #65 was not closed by the implementing agent.

## Handoff / next agent

- **#66:** establish controlled ownership/migration and recovery for existing
  accounts, using the existing trusted writer and migration-16 uniqueness. Unknown
  identities currently fail 401. Never bind by email alone or expose the writer
  through an unrestricted callback/API; resetting Keycloak can change subjects.
- **#67:** retire password/registration/activation entry points only through the
  separately approved rollout. Coexistence is deliberate; it is not a fallback
  invoked by failed OIDC. Production SSO-only policy is not delivered by this flag.
- **#68:** wire the trusted backend initiation URL, serve frontend/API under the
  configured public origin, align the exact registered callback, and handle the
  fixed success destination and generic error/pending/suspended UX. Preserve
  exact-Origin local logout. Explain that local logout leaves the IdP browser
  session active. Do not collect provider passwords or persist tokens in React.
- **#69/#70:** complete the wider threat model, proxy/rate-limit deployment review,
  provider/global logout policy, production TLS/secret handling and formal
  requirements revision. Browser evidence here is Chromium/loopback HTTP only;
  multi-client SSO, real institutional approval and production readiness remain
  outside this task.

Start with [Identity Stage 3B](identity.md#oidc-stage-3b-authenticated-tutti-sessions),
`oidc_authentication.go`, the HTTP callback, Redis rotation/limiter, the existing
binding reader/writer, and `test_oidc_keycloak.py`. No new schema deployment is
needed beyond the existing migrations. The browser harness uses the existing
installed frontend Playwright; it adds no dependency or frontend product code.
