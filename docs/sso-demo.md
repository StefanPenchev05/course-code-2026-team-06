# Local SSO provider for the Tutti course demo

Issue [#46](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/46) provisions **Keycloak 26.8.2**, a pinned local development Identity Provider (IdP). Its synthetic accounts simulate institutional identities. We have no real Uni.lu IdP access and do not contact university IT for this demo. This setup cannot authenticate real university accounts, verify enrollment or prove course grades. An email suffix is not evidence of any of these.

The updated issue and course-demo instruction supersede the official-institution integration assumption **for the prototype only**. [D01/FR1/FR23](requirements-decisions.md) still record the earlier official Uni.lu target; their broader revision belongs to #70. This guide records the local contract without claiming university approval. Production/institutional integration would be separate future work. Existing password login remains implemented; #46 does not migrate it.

## Concepts and scope

- **OAuth 2.0** authorizes a client to access protected resources; it is not by itself a login/identity protocol.
- **OpenID Connect (OIDC)** adds authentication to OAuth 2.0, including discovery and a signed ID token identifying an authenticated subject.
- An **IdP** authenticates a user and issues assertions/tokens. Keycloak supplies that role locally; Tutti retains application authorization.
- A **realm** is an isolated identity/configuration boundary. `tutti-demo` is separate from the `master` administration realm.
- An **OIDC client** is an application registered with the IdP, including its credentials, allowed flows and callbacks. `tutti-backend` represents the future Go server.
- **SSO** means an existing IdP browser session can authenticate the user to multiple independent clients without another credential prompt. One provider login alone does not demonstrate this. A later demonstration should use two independent OIDC clients sharing the Keycloak browser session; no second application/client or Go callback is implemented here.

## Configuration contract

| Item | Local value / behavior |
| --- | --- |
| Compose extension | [`compose.sso.yaml`](../compose.sso.yaml), combined with the unchanged [`compose.yaml`](../compose.yaml) |
| Provider / admin console | `http://localhost:8180` / `http://localhost:8180/admin/` |
| Listener | Published `127.0.0.1:8180` only; no published management port |
| Realm | [`infra/keycloak/realm/tutti-demo-realm.json`](../infra/keycloak/realm/tutti-demo-realm.json) imported at first startup |
| Issuer | `http://localhost:8180/realms/tutti-demo` |
| Discovery | `http://localhost:8180/realms/tutti-demo/.well-known/openid-configuration` |
| Authorization endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/auth` |
| Token endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/token` |
| JWKS endpoint | `http://localhost:8180/realms/tutti-demo/protocol/openid-connect/certs` |
| Client | `tutti-backend`, confidential OIDC, client-secret authentication |
| Allowed flow | Authorization Code with mandatory S256 PKCE; no implicit flow, direct/password grants, service accounts, device or CIBA grants |
| Exact callback | `http://localhost:8080/api/v1/auth/oidc/callback` — **proposed for #65, currently absent** |
| Browser origin | Exactly `http://localhost:8080`; no wildcard origins or redirects |
| OIDC scopes | Request `openid profile email`; no application-role mapping |
| Secrets | Local `KEYCLOAK_ADMIN_USERNAME`, `KEYCLOAK_ADMIN_PASSWORD`, `TUTTI_OIDC_CLIENT_SECRET`; no defaults |
| Storage | Separate `keycloak_demo_data` named volume, Keycloak embedded H2 `dev-file`; no connection to Tutti PostgreSQL or Redis |
| Restart | `restart: "no"`; explicitly start/stop the optional demo. Health check probes management readiness internally. |

Use `localhost`, not `127.0.0.1`, in browser/provider URLs: they are different issuer/origin strings. The supported future development layout is **Go running on the host** at localhost:8080 and Keycloak in Docker on localhost:8180. The configured hostname keeps discovery, browser redirects and future Go validation consistent.

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

To try provider login before #65, visit `http://localhost:8180/realms/tutti-demo/account/` using a synthetic user. This logs into Keycloak's own account console, not Tutti. No Keycloak account or claim currently creates a Tutti user or session. Future student enrollment, tutor approval, coordinator provisioning, faculty access and suspension checks must remain server-side Tutti decisions.

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

The script checks the import file, exact callback/origin, confidential code flow, S256, disabled grants and absence of seeded credentials. Live checks validate issuer, endpoints, JWKS, a no-session code-flow preflight (`login_required`), and rejection of a disallowed callback, missing PKCE and implicit response. With `--admin`, it checks persisted client flags and the injected client secret and confirms the backend client's password grant is rejected. The built-in `admin-cli` master-realm password grant used only for maintenance is distinct from the disabled application password grant. Admin tokens remain in process memory and are never printed/saved.

Use the console's **Clients → tutti-backend** to inspect settings manually. A callback attempt currently fails to connect to Tutti or returns 404 because #65 has not implemented it; this is not a provider import failure.

## Intended Go/Redis integration — Issue #65, not implemented here

1. Browser asks Tutti to sign in. Go creates single-use state, nonce and a PKCE verifier/challenge, retaining correlation securely server-side.
2. Browser redirects to the exact issuer's authorization endpoint with `response_type=code`, `scope=openid profile email` and S256 challenge. Keycloak handles credentials and the IdP browser session.
3. Keycloak redirects a short-lived authorization code to the exact proposed Go callback. Go checks state and exchanges the code server-to-server with the confidential client secret and PKCE verifier.
4. Go validates signature using JWKS, expected issuer/audience, nonce, expiration and other protocol checks using maintained OIDC libraries. Bind the stable `(issuer, sub)` to a Tutti account through the approved linking workflow; never merge or elevate accounts solely by email or IdP role.
5. Go checks current Tutti permissions, enrollment approval and suspension, then issues its existing opaque HttpOnly application cookie backed by Redis. Tokens must not go into browser storage or replace PostgreSQL authorization checks. Tutti logout revokes its Redis session; IdP-session logout is a separate behavior to define/test.

The [existing Identity guide](identity.md), [Identity service](../backend/internal/identity/application/service.go), [cookie handler](../backend/internal/identity/interfaces/http/handler.go) and [Redis store](../backend/internal/identity/infrastructure/redis/session_store.go) remain unchanged. No Go OIDC environment variables, routes, token verifier, database binding or frontend login are introduced here. #66–#70 retain their separate account-linking, migration, UI, security and documentation work.

## Stop, restart and reset

Stop/start only Keycloak, preserving synthetic accounts, settings and keys:

```sh
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml stop keycloak
docker compose --env-file .env.sso -p tutti-sso-demo -f compose.yaml -f compose.sso.yaml up -d --wait keycloak
```

Startup import **skips an existing realm**. Editing JSON or changing `.env.sso` will not overwrite an existing realm/client secret or bootstrap administrator. Update these deliberately through the admin console, or reset this disposable provider. After changing the secret, update any future Go configuration consistently. The live admin verifier detects a mismatch with the stored client secret.

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
- **No Tutti session after Keycloak login:** expected until #65. This task provisions an IdP, not end-to-end application authentication or a demonstrated multi-client SSO journey.

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
