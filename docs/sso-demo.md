# Local SSO provider for the Tutti course demo

Issue [#46](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/46) provisions **Keycloak 26.8.2-3**, a pinned local development Identity Provider (IdP). Its synthetic accounts simulate institutional identities. We have no real Uni.lu IdP access and do not contact university IT for this demo. This setup cannot authenticate real university accounts, verify enrollment or prove course grades. An email suffix is not evidence of any of these.

The updated issue and course-demo instruction supersede the official-institution integration assumption **for the prototype only**. [D01/FR1/FR23](requirements-decisions.md) still record the earlier official Uni.lu target; their broader revision belongs to #70. This guide records the local contract without claiming university approval. Production/institutional integration would be separate future work. Existing password login remains implemented; #46 does not migrate it.

## Concepts and scope

- **OAuth 2.0** authorizes a client to access protected resources; it is not by itself a login/identity protocol.
- **OpenID Connect (OIDC)** adds authentication to OAuth 2.0, including discovery and a signed ID token identifying an authenticated subject.
- An **IdP** authenticates a user and issues assertions/tokens. Keycloak supplies that role locally; Tutti retains application authorization.
- A **realm** is an isolated identity/configuration boundary. `tutti-demo` is separate from the `master` administration realm.
- An **OIDC client** is an application registered with the IdP, including its credentials, allowed flows and callbacks. `tutti-backend` represents the future Go server.
- **SSO** means an existing IdP browser session can authenticate the user to multiple independent clients without another credential prompt. One provider login alone does not demonstrate this. A later demonstration should use two independent OIDC clients sharing the Keycloak browser session; no second application/client is supplied by #46; the Go callback is separate #65 work.

## Configuration contract

| Item | Local value / behavior |
| --- | --- |
| Image | `quay.io/keycloak/keycloak:26.8.2-3` (26.8.2 JVM security rebuild) |
| Compose extension | [`compose.sso.yaml`](../compose.sso.yaml), combined with the unchanged [`compose.yaml`](../compose.yaml) |
| Provider / admin console | `http://localhost:8180` / `http://localhost:8180/admin/` |
| Listener | Published `127.0.0.1:8180` only; no published management port |
| Realm | [`infra/keycloak/realm/tutti-demo-realm.json`](../infra/keycloak/realm/tutti-demo-realm.json) imported at first startup |
| Issuer | `http://localhost:8180/realms/tutti-demo` |
| Discovery | `http://localhost:8180/realms/tutti-demo/.well-known/openid-configuration` |
| Authorization endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/auth` |
| Token endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/token` |
| User information endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/userinfo` (Bearer token required) |
| Provider logout endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/logout` (does not revoke a Tutti Redis session) |
| JWKS endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/certs` |
| Client | `tutti-backend`, confidential OIDC, client-secret authentication |
| Allowed flow | Authorization Code with mandatory S256 PKCE; no implicit flow, direct/password grants, service accounts, device, CIBA, standard token exchange or JWT bearer grants |
| Exact callback | `http://localhost:8080/api/v1/auth/oidc/callback` — implemented for Stage 2 protocol verification only |
| Browser origin | Exactly `http://localhost:8080`; no wildcard origins or redirects |
| OIDC scopes | Request `openid profile email`; no application-role mapping |
| Secrets | Local `KEYCLOAK_ADMIN_USERNAME`, `KEYCLOAK_ADMIN_PASSWORD`, `TUTTI_OIDC_CLIENT_SECRET`; no defaults |
| Storage | Separate `keycloak_demo_data` named volume, Keycloak embedded H2 `dev-file`; no connection to Tutti PostgreSQL or Redis |
| Restart | `restart: "no"`; explicitly start/stop the optional demo. Health check probes management readiness internally. |

Use `localhost`, not `127.0.0.1`, in browser/provider URLs: they are different issuer/origin strings. The supported development layout is **Go running on the host** at localhost:8080 and Keycloak in Docker on localhost:8180. The configured hostname keeps discovery, browser redirects and future Go validation consistent.

Inside a normal bridged Go container, `localhost` refers to that container, not Keycloak. Do not replace the configured issuer with `http://keycloak:8080`, rewrite token issuers or skip validation. A future containerized Go setup must provide a shared reachable hostname/routing contract, or explicitly use host networking (Linux, or Docker Desktop with host networking enabled) so the same localhost issuer is reachable. That backend deployment is not provisioned by this issue. Other containers on the Compose network can access Keycloak internally; loopback publication is not isolation from those containers or other local users.

## Start locally

Requires Docker Compose v2 and Python 3 for generation/verification. Run commands from the repository root. Use a fixed project name so cleanup targets only this demo. Existing PostgreSQL/Redis/worker services are not started by the `keycloak`-only command.

Generate an ignored local environment file **once**, without overwriting an existing file. Both credentials are random and distinct; no passwords are printed:

```sh
python3 - <<'PY'
import os, secrets
fd = os.open('.env.sso', os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as f:
    f.write('KEYCLOAK_ADMIN_USERNAME=tutti-local-admin\n')
    f.write('KEYCLOAK_ADMIN_PASSWORD=' + secrets.token_hex(32) + '\n')
    f.write('TUTTI_OIDC_CLIENT_SECRET=' + secrets.token_hex(32) + '\n')
PY

docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml config --quiet
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml up -d --wait keycloak
```

Open the admin console. Read the local admin credentials privately from `.env.sso` with your editor/password manager; do not paste them into tickets, logs or Git. `.env.*` is already ignored. Alternatively, set the three variables in your shell; the exact issue command `docker compose -f compose.yaml -f compose.sso.yaml up -d keycloak` also works with exported values (and uses Compose's default project). Choose one project consistently. `.env.example` documents the variables but its empty credentials deliberately fail validation.

Compose's fully rendered `config` output includes resolved secrets: use `config --quiet` or inspect it privately, never attach the rendered configuration. Shell environment variables override env-file values; clear stale exports when changing the local file. Docker administrators can inspect container environment variables, so this is a local demo secret mechanism, not a production vault.

**Windows PowerShell:** use the same Docker commands. To create the file, send the Python block above via a PowerShell single-quoted here-string (`@'` on its own line, Python code, then `'@ | py -3 -`). Replace subsequent `python3` commands with `py -3` where appropriate. POSIX file mode 0600 is not a Windows ACL; restrict `.env.sso` to your user with Windows file permissions. No Bash dependency is needed on the host; the container health check uses its own Bash. For an alternative manual setup, create an ignored `.env.sso` with exactly those three variable names and distinct locally generated credentials.

## Create synthetic identities

No users or passwords are imported. In the console, select **tutti-demo**, then **Users → Add user**. Suggested fictional examples:

| Scenario label | Username | First / last name | Email |
| --- | --- | --- | --- |
| Student | `demo-student` | Demo Student | `student@tutti.example` |
| Tutor | `demo-tutor` | Demo Tutor | `tutor@tutti.example` |
| Faculty coordinator | `demo-coordinator` | Demo Coordinator | `coordinator@tutti.example` |

Enable each user and set a unique local password under **Credentials**; use the temporary-password option if you want first-login replacement. Supply name/email so the account profile can complete. Do not use real student numbers, real email addresses, real university passwords, or commit realm exports containing credential hashes. These are scenario names, **not Tutti roles**. Do not assign realm-admin privileges to demo users or map IdP roles to application coordinator/tutor privileges. Email verification in this synthetic realm is not university verification.

To try provider login before #65, visit `http://localhost:8180/realms/tutti-demo/account/` using a synthetic user. This logs into Keycloak's own account console, not Tutti. The account console does not authenticate Tutti. The integrated #65 backend can now establish a Tutti session only for an explicitly prelinked eligible identity; it never creates an account or grants privileges from claims. Student verification, tutor approval, coordinator/faculty access and suspension remain server-side Tutti decisions.

## Inspect and verify

```sh
python3 infra/keycloak/verify.py
python3 infra/keycloak/verify.py --live
curl --fail http://localhost:8180/realms/tutti-demo/.well-known/openid-configuration
```

PowerShell can use `Invoke-RestMethod 'http://localhost:8180/realms/tutti-demo/.well-known/openid-configuration'`. The provider-wide discovery grant list describes Keycloak capabilities, **not this client's permissions**; inspect the imported client or run the administrator check below.

To verify the persisted confidential client and secret without displaying them, load this specifically generated environment file and run the administrator check in a child process:

```sh
python3 - <<'PY'
import os, subprocess
from pathlib import Path
env = os.environ.copy()
for line in Path('.env.sso').read_text().splitlines():
    if line and not line.startswith('#'):
        key, value = line.split('=', 1)
        env[key] = value
subprocess.run(['python3', 'infra/keycloak/verify.py', '--live', '--admin'], env=env, check=True)
PY
```

The snippet reads the simple unquoted file generated above; it is not a general dotenv parser. In PowerShell use the same Python here-string convention and substitute `py -3` for `python3` in the subprocess list, or export the three variables with `$env:NAME` and run `py -3 infra/keycloak/verify.py --live --admin`.

The script checks the import file, exact callback/origin, confidential code flow, S256, disabled grants and absence of seeded credentials. Live checks validate issuer, all five endpoint URLs, JWKS, scopes, RS256 and client-secret authentication capabilities, a no-session code-flow preflight (`login_required`), and rejection of a disallowed callback, missing/plain PKCE and implicit response. Userinfo must reject a request without a bearer token; authorization-code exchange without client credentials must fail, and logout must be reachable (it may return a confirmation page or reject missing parameters). This is not a logout/session-revocation test.

With `--admin`, it checks persisted realm restrictions, client flags, exact allowlists, profile/email-only scopes and all explicitly disabled alternative grants. It compares the stored secret with the supplied environment value, then proves that secret authenticates at the token endpoint: an intentionally invalid synthetic code gets `invalid_grant`, while an intentionally wrong secret gets a 401 client-authentication rejection. No valid code or ID token is obtained. It also confirms password and service-account grants are rejected. The pinned image returns `400 unauthorized_client` for password and `401 unauthorized_client` for service accounts; an error response does not mean the grant succeeded. The built-in `admin-cli` master-realm password grant used only for maintenance is distinct from the disabled application password grant. Admin tokens remain in process memory and are never printed/saved.

Use the console's **Clients → tutti-backend** to inspect settings manually. With the API running and OIDC enabled, the integrated callback verifies the code/ID token, resolves a prelinked account and creates its opaque Redis session before a fixed 303 redirect. Unknown identities receive generic 401 and no session; no account is implicitly created. Without OIDC enabled the route is absent.

## Go/Redis integration — prelinked-account sessions implemented; migration/UI separate

1. Browser asks Tutti to sign in. Go creates single-use state, nonce and a PKCE verifier/challenge, retaining correlation securely server-side.
2. Browser redirects to the exact issuer's authorization endpoint with `response_type=code`, `scope=openid profile email` and S256 challenge. Keycloak handles credentials and the IdP browser session.
3. Keycloak redirects a short-lived authorization code to the exact proposed Go callback. Go checks state and exchanges the code server-to-server with the confidential client secret and PKCE verifier.
4. Go validates signature using JWKS, expected issuer/audience, nonce, expiration and other protocol checks using maintained OIDC libraries. Bind the stable `(issuer, sub)` to a Tutti account through the approved linking workflow; never merge or elevate accounts solely by email or IdP role.
5. Go checks current Tutti permissions, enrollment approval and suspension, then issues its existing opaque HttpOnly application cookie backed by Redis. Tokens must not go into browser storage or replace PostgreSQL authorization checks. Tutti logout revokes its Redis session; IdP-session logout is a separate behavior to define/test.

The [Identity guide](identity.md), [Identity service](../backend/internal/identity/application/service.go), [cookie handler](../backend/internal/identity/interfaces/http/handler.go) and [Redis store](../backend/internal/identity/infrastructure/redis/session_store.go) were unchanged by Issue #46. Issue #65 now adds optional Go login initiation and Stage 2 callback/exchange/ID-token verification; see [its configuration and transaction contract](identity.md#oidc-stage-1-login-initiation-issue-65). Stage 3A now provides internal persistent identity binding/resolution; Stage 3B now integrates callback resolution and Tutti application sessions. The frontend SSO button/UX remains #68 work. #66–#70 retain their separate account-linking, migration, UI, security and documentation work.

## Stop, restart and reset

Stop/start only Keycloak, preserving synthetic accounts, settings and keys:

```sh
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml stop keycloak
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml up -d --wait keycloak
```

Startup import **skips an existing realm**. Editing JSON or changing `.env.sso` will not overwrite an existing realm/client secret or bootstrap administrator. Update these deliberately through the admin console, or reset this disposable provider. After changing the secret, update any future Go configuration consistently. The live admin verifier detects a mismatch with the stored client secret. The
current import also explicitly sets `standard.token.exchange.enabled=false` and
`oauth2.jwt.authorization.grant.enabled=false`. Older persisted realms will not
receive those attributes just by restarting. Disable those capabilities deliberately
through the client admin settings/API, preserving all other client fields, or use
the isolated reset below **only for disposable demo data**; rerun the verifier.
Do not change unrelated realms or use a whole-project volume reset.

**Reset only the disposable Keycloak data** (deletes demo users, IdP sessions and keys; changes future subject identities):

```sh
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml rm -s -f keycloak
docker volume rm tutti-sso-demo_keycloak_demo_data
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml up -d --wait keycloak
```

These commands target the documented project/volume only. Do not use broad `down --volumes` against the combined project: it can also delete Tutti's PostgreSQL volume. If you chose another project name, confirm the correct dedicated Keycloak volume before deletion. Never reset a shared or production IdP this way.

## Limitations and troubleshooting

This is HTTP-only, `start-dev`, with local H2 storage and no production availability/TLS claims. Port loopback binding and exact redirect/origin restrictions reduce accidental exposure but do not make it production-ready. Use only synthetic identities. Future production work requires a trusted issuer, HTTPS, protected secret management and the separate authorization/security migration. Existing Go password authentication is intentionally unchanged.

- **8180 busy:** stop your own conflicting local service or choose a new consistent hostname/port, issuer and test contract; do not bypass issuer checks. Inspect `docker compose ... ps` first.
- **Startup/health failure:** inspect `docker compose ... logs --tail 80 keycloak` privately. The first startup may take a minute; `--wait` checks readiness. The image has no curl; the internal Bash health check uses port 9000, not a published management listener.
- **Missing variable:** generate/provide all three values; never add a real default to the import or committed Compose file.
- **Realm/client edits ignored:** an existing volume retains the previous import; use the targeted reset only for disposable data.
- **Issuer mismatch/unreachable Go container:** use the exact supported host layout; do not disable protocol validation or switch between localhost, 127.0.0.1 and Docker service names casually.
- **No Tutti session after Keycloak login:** enable OIDC and use an explicitly prelinked identity. Unknown pairs receive 401, ineligible accounts 403; never fix this by email matching or trusting roles. Check the configured origin/cookie layout. This login alone does not demonstrate multi-client SSO.

The pinned `26.8.2-3` rebuild follows the [Keycloak Java runtime advisory](https://www.keycloak.org/2026/11/java4shell-vulnerability-cve-2026-114938). The upstream `26.8.2` tag can move to rebuilt images; using the explicit rebuild tag avoids reusing a pre-fix cached image on another architecture. This verification also checks the running image's bundled Java is `21.0.13.1`. Future upgrades must recheck the image and runtime rather than inferring JVM fixes from the Keycloak application version alone.

Implementation follows Keycloak's official [container guide](https://www.keycloak.org/server/containers), [startup import/environment placeholder guide](https://www.keycloak.org/server/importExport) and [hostname guide](https://www.keycloak.org/server/hostname). The version is pinned for reproducibility; review upstream releases before future upgrades and retest imports/flows.

## Verification record — 2026-11-23

Verified with Docker on the development machine using an isolated `tutti-sso-issue46` project and randomly generated temporary credentials (no existing `.env` was read/overwritten):

- Combined Compose configuration passed; resolved PostgreSQL, Redis and worker service definitions matched the base configuration exactly. Missing Keycloak credentials rejected configuration.
- Keycloak 26.8.2 pulled and started healthy on loopback port 8180; realm and confidential client imported with the expected secret.
- `python3 infra/keycloak/verify.py --live --admin` passed discovery/issuer/endpoints/JWKS checks, exact callback/origin and persisted client configuration, S256 requirement, and negative redirect/implicit/password-grant checks.
- Stop/start preserved the realm/client. Removing only the disposable Keycloak volume and starting again successfully reproduced the import and passed the same checks.
- Local links, verifier syntax, generated-secret exclusion, ignored `.env.sso`, and `git diff --check` passed. No backend, migration, base Compose or existing session changes were made.
- Verification containers, dedicated volume/network and temporary credential file were removed afterward. The demo is not left running.

Not executed: a synthetic user's interactive login, multi-client browser SSO demonstration, Go callback/token validation/Redis-session creation, real Uni.lu authentication, or production security validation. These results establish local provider infrastructure only. Backend regression suites were not run because backend code/configuration was not changed.

## Stage 2 callback configuration

The Go API now supports the [Stage 2 callback and verification contract](identity.md#oidc-stage-2-callback-and-verified-provider-identity). In its private shell configuration, set `OIDC_CLIENT_SECRET` to **the same value** as this demo's `TUTTI_OIDC_CLIENT_SECRET`; do not generate a different API secret. Loading `.env.sso` into Compose does not export it into a separate Go process. On POSIX shells, after privately loading the local values, use `export OIDC_CLIENT_SECRET="$TUTTI_OIDC_CLIENT_SECRET"`; PowerShell uses `$env:OIDC_CLIENT_SECRET = $env:TUTTI_OIDC_CLIENT_SECRET`. Never print the secret or put it in client-side configuration.

The imported client uses RS256 ID tokens by default, which is the backend's explicit signing policy. Browser login must use the exact configured localhost issuer and callback; discovery, token and JWKS validation are not disabled for this HTTP loopback demo. Synthetic sign-in now uses Stage 3B: a prelinked eligible identity receives a fresh Tutti application cookie, temporary-cookie clearing and a configured same-origin 303 redirect. Tokens/claims are not returned. Unknown identities remain unlinked. The Issue #46 verification record above describes its earlier infrastructure-only scope; Stage 2 results are recorded separately in the task handoff.

### Stage 2 verification record — 2026-11-23

The task's mock RSA/JWKS provider tests and real Redis suite passed, including signed callback verification, wrong-browser/replay/expiry protection and concurrent single-use exchange. A separate disposable Compose project ran Keycloak 26.8.2, PostgreSQL and Redis plus the actual Go API. Because the normal API port was already in use, the isolated client was given an exact callback on a free localhost port without changing the committed realm. One synthetic user's HTML login-form flow completed confidential code exchange, original S256 verifier and RS256 ID-token verification, returned the safe success JSON and cleared the temporary cookie. `/api/v1/auth/me` remained 401 and PostgreSQL `users` remained empty. Test containers, volumes and the API were removed afterward; no real university identity was used.

This live probe used a Python cookie jar configured to model browsers' Secure-cookie exception **only for HTTP localhost**. Its initial ordinary-cookie-jar runs failed in Keycloak with “cookie not found,” before reaching the Go callback; the corrected probe passed. This is an HTTP/form smoke test, not a browser compatibility or multi-client SSO test. No issuer, token-validation or server cookie settings were weakened. Browsers/clients differ in localhost Secure-cookie handling; see the [cookie behavior reference](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Cookies). Use a compatible development browser or properly configured local HTTPS when a client does not implement that exception. Do not extend the loopback exception to remote hosts.

## Stage 3A: synthetic account provisioning and realm reset

The [persistent binding contract](identity.md#oidc-stage-3a-persistent-identity-binding) adds one exact `(issuer, subject)` mapping per existing Tutti user via migration 16. Internal resolution reads current Tutti permissions; an independent trusted provisioning capability creates mappings. Integration tests provision existing synthetic users and bind explicitly verified fixture identities. There is **no public linking endpoint or production provisioning command** in this stage. Ordinary Keycloak users cannot choose arbitrary Tutti IDs; neither email/name matching nor Keycloak roles establish ownership or university approval.

Changing a synthetic user's email or display name does not break its mapping. Resetting/reimporting Keycloak can change the subject even if the username/email is identical. Such an identity is unknown until a separately approved trusted recovery workflow proves ownership; existing bindings are never overwritten automatically. The mapping's restrictive user FK and guarded rollback protect against silently forgetting associations. It stores no tokens or passwords.

The current browser callback resolves the successfully verified identity through the reader capability and performs Stage 3B session establishment; it must not expose the provisioning writer or auto-register unknown users. Pending/rejected students stay restricted, active tutor and coordinator permissions come from Tutti's database, and suspended/closed users cannot resolve for login. No new environment variable, Keycloak role mapper or synthetic password is needed for this persistence stage.


## Issue #46 re-verification and handoff — 2026-11-26

### Task and existing implementation

Started on `main` at `98009df` with a clean working tree. Reviewed root AGENTS.md,
base/SSO Compose, environment/ignore rules, realm import, this guide, Identity's
existing OIDC provider/configuration and related issues #46 / #65–#70. The optional
provider, secret placeholders, health check, isolated H2 storage, exact callbacks,
PKCE and most documentation already worked. This task adds no Go authentication,
account linking, sessions, frontend changes or migrations; existing #65 stages
remain as described in the Identity guide.

### Changes made

- `compose.sso.yaml`: pin the explicit `26.8.2-3` Java security rebuild; retain the
  loopback binding, data volume, read-only import and health check.
- `infra/keycloak/realm/tutti-demo-realm.json`: explicitly disable standard token
  exchange and JWT bearer grants alongside the existing disabled flows.
- `infra/keycloak/verify.py`: reuse the smoke checker and verify userinfo/logout,
  discovery scopes/client authentication/RS256, plain-PKCE rejection, persisted
  realm restrictions, minimal client scopes, all alternative-grant flags, actual
  client-secret authentication and wrong-secret/service-account rejection.
- `.env.example`: clarify the separate Keycloak and Go client-secret variable names.
- This guide: complete the endpoint table, update/reimport guidance, evidence and
  acceptance matrix. Formal D01 / FR1 / FR23 / UC01 revision remains #70.

### Verification executed

Used Docker on aarch64 with a newly named `tutti-sso46-*` project and random
credentials in a mode-0600 temporary file outside Git. No existing `.env` was
changed; Keycloak credentials came from the isolated test environment. The API and existing developer databases were
not started, stopped or modified by this verification.

| Check / command | Result |
| --- | --- |
| `docker compose -f compose.yaml -f compose.sso.yaml up -d keycloak` with private exported credentials/project name | Passed; running/healthy on `127.0.0.1:8180` |
| Combined `config --quiet`; base config independently; compare resolved service definitions privately with `--profile workers` | Passed; PostgreSQL, Redis and worker definitions identical; only Keycloak added |
| Every required credential empty in turn | Compose rejected each case; no interpolated config/secrets printed |
| `python3 infra/keycloak/verify.py` and Python syntax check | Passed; no added dependencies |
| `python3 infra/keycloak/verify.py --live --admin` | Passed before/after restart and after fresh import |
| Discovery/JWKS | HTTP 200; exact issuer/endpoints, S256, RS256, identity scopes and confidential authentication advertised |
| Provider endpoint checks | Userinfo rejects no-token request (401); logout reachable; no-client code exchange rejected; valid code-flow preflight returns `login_required` |
| Security negative probes | Exact callback enforced; missing/plain PKCE and implicit flow rejected; password/service-account grants rejected; wrong client secret rejected |
| Runtime secret substitution | Admin-retrieved value equals generated environment secret; correct secret reaches invalid-code validation (`invalid_grant`), wrong secret fails client authentication |
| Persisted configuration | Realm/client enabled, registration/reset disabled, profile/email-only scopes, exact callback/origin, S256 and all disabled grant attributes confirmed |
| `docker exec <owned-keycloak-container> java -version` | OpenJDK `21.0.13.1`, matching the selected rebuild |
| `restart keycloak`, readiness and verifier again | Passed; realm/client UUIDs and client secret preserved |
| `stop keycloak` and discovery probe | Container stopped (SIGTERM exit 143); discovery no longer reachable |
| Remove only label-verified disposable Keycloak data volume, then start again | Fresh import passed every live/admin check and generated a new persisted client UUID |
| Existing Tutti PostgreSQL/Redis status | Both remained running and healthy; other developer containers preserved |
| Ignore/credential/log checks, local links and `git diff --check` | Passed; generated credentials absent from tracked files and container logs |

The first new negative probes assumed different OAuth error statuses. The selected
image returns `401 unauthorized_client` for service accounts and can use
`unauthorized_client` for a wrong secret. Assertions were corrected to verify
actual rejection at the right protocol boundary; successful token issuance is
never accepted. No provider restrictions were relaxed to make a probe pass.

### Acceptance matrix

| Issue #46 criterion | Evidence / status |
| --- | --- |
| Existing Compose reviewed/reused and starts with documented command | Passed; optional overlay retained |
| PostgreSQL/Redis/worker preserved | Base/combined configuration equality; existing services healthy |
| Reproducible `tutti-demo` import | Fresh-volume startup and repeated live/admin verification passed |
| Confidential `tutti-backend`, client authentication, code flow and S256 | Persisted configuration and preflight/negative probes passed |
| Unnecessary grants disabled | Explicit imported/persisted flags; implicit/password/service-account negative probes passed |
| Exact callback and restricted origin | Allowlist assertions and bad-redirect rejection passed |
| Credentials outside Git; missing values fail safely | Temporary random values, ignored local-file guidance and per-variable Compose rejection passed |
| Runtime secret substitution | Persisted equality and positive/negative client-authentication probes passed |
| Discovery with exact issuer | HTTP 200 and all five advertised endpoint comparisons passed |
| Synthetic user provisioning | Existing manual console instructions use reserved `.example` identities and locally supplied passwords |
| Startup, readiness, restart, troubleshooting and isolated cleanup | Documented and runtime lifecycle checks passed |
| #65 integration contract | Table above complete; Go consumes the same secret as `OIDC_CLIENT_SECRET` |
| Simulated-provider and authorization boundaries | Explicit throughout; no institutional approval/enrollment/role claims |
| Go authentication / multi-client SSO / production deployment | Outside this task; not newly implemented or verified |
| Repository/issue operations | Changes uncommitted on `main`; no push, merge or issue closure |

### Limits and next agent

No interactive synthetic-user login, successful code exchange/ID-token validation,
Go callback, Tutti session creation, userinfo claim contents, actual IdP logout or
two-client browser SSO was tested in this infrastructure run. Device/CIBA/token
exchange/JWT bearer restrictions are verified as persisted configuration, not
through successful signed-token flow tests. Existing prior Stage 2 evidence above
is separate from this run. Go/unit/race/build and PostgreSQL migration suites were
not rerun because no backend code, base Compose, application database or migration
changed; live provider configuration checks are the relevant verification here.
No official Uni.lu login, enrollment verification, production readiness or formal
SSO-only requirements migration is claimed.

For #65, use the exact issuer/callback/scopes in the table and supply the same
client secret privately to Go. Keep issuer verification enabled and resolve the
browser/container hostname contract before containerizing the API. Continue
existing Stage 3A binding into the separately scoped Stage 3B workflow without
email-only linking or automatic roles. #66–#70 retain their linking, legacy-login,
UI, threat-model and requirements responsibilities. Multi-client SSO remains a
separate demonstration. Inspect this guide, `verify.py` and `docs/identity.md`
first. The verification project, its dedicated volume/network and temporary
credentials are removed after testing; no demo provider is left running.


## Issue #65 — integrated login verification (2026-11-26)

[Stage 3B](identity.md#oidc-stage-3b-authenticated-tutti-sessions) now composes
protocol verification, trusted identity resolution and existing Redis sessions.
The historical Stage 2 records above describe earlier protocol-only behavior.
Run `python3 backend/tests/integration/test_oidc_keycloak.py` for the new isolated
real-browser test (requires backend test tools plus the installed frontend
Playwright/Chromium). It refuses an existing listener on port 8180, creates only
a new demo provider and reuses disposable PostgreSQL/Redis test infrastructure.
The committed callback stays localhost:8080; only the owned test client's callback
is temporarily configured to an exact available test port.

The tutor, pending-student, coordinator, unknown-identity and suspended-tutor
browser journeys passed against the actual Go bootstrap and Keycloak. The test
checks rotation, protected reads, local logout and empty application browser
storage. This is direct navigation to the backend login endpoint, not a React SSO
button or two-client SSO demonstration. Test credentials are random/in-memory,
roles come from Tutti and owned test resources are cleaned afterward. See the
[#65 acceptance/evidence handoff](oidc-authentication-verification.md) for the full
results and #66–#70 boundaries. No real university integration is claimed.
