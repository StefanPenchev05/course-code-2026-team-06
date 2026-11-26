# Background workers and email delivery

The API writes notification records to PostgreSQL as part of business transactions. The separate `tutti-worker` process runs one expiration loop and one email loop. Both start immediately, process bounded batches, and wait between iterations. Worker instances coordinate through PostgreSQL. Redis is only needed by the API, and the API can queue notifications while SMTP or the worker is unavailable.

This is the delivery side of [FR7 and NFR9](1.3-1.4_user-story_mapping--requirement_analysis.md): university-email notifications with durable pending/sent/failed state and sent timestamps. Session/application producers keep the transactional status history required by NFR12. The delivery worker does not rewrite that business history.

## Start and stop

Use [the database setup commands](database/implementation.md) to start PostgreSQL and apply all migrations before starting a worker. Copy `.env.example` to `.env` if needed. Set `SMTP_HOST`, `SMTP_FROM`, and the provider's port, TLS mode, and credentials. Use a test recipient and a local mail sink for local trials so test records do not go to real users.

An unconfigured worker exiting with an `SMTP_HOST` error is expected. PostgreSQL and Redis can remain healthy and the API can continue queueing mail. The `workers` Compose profile is the explicit enablement switch; the API does not start a second delivery loop. No university credentials are needed for the local tests below.

### Windows PowerShell and Docker Desktop (WSL 2, Linux containers)

Run these commands from the repository root. The test runner requires only Docker Desktop and PowerShell; it builds tools from the existing `backend/Dockerfile`, creates fresh PostgreSQL/Redis containers on a unique network, applies migrations, and captures mail in loopback SMTP fixtures. It never reads `.env`, uses the development database, exposes SMTP ports, or sends real email. The tool container gets the local Docker socket solely to manage its disposable test containers. Do not run it against a remote production Docker context.

```powershell
# Full format, vet, race tests, build, database suites, rollback/reapply,
# and a smoke test of the actual non-root worker runtime image.
./backend/tests/integration/test_docker.ps1

# Quicker outbox + runtime-container checks only (also used in CI).
./backend/tests/integration/test_docker.ps1 -NotificationsOnly
```

The scripts rebuild Go code before container verification and remove only their own test containers, temporary data, and unique network. Local build images remain cached. The development PostgreSQL at host port **5433** and Redis at **6379** stay untouched. `.env.example` still shows its portable default PostgreSQL port 5432; keep your existing local port override. Containers use `postgres:5432`, regardless of the Windows host port.

After applying migrations and configuring an authorized SMTP relay, build and operate the existing Compose worker:

```powershell
docker compose --profile workers config --quiet
docker compose --profile workers build worker
# PostgreSQL is already running; do not recreate dependency services.
docker compose --profile workers up -d --no-deps worker
docker compose --profile workers ps -a worker
docker compose --profile workers logs --tail 100 worker
docker compose --profile workers logs -f worker
# Ctrl+C exits log following. Then stop only the worker when needed:
docker compose --profile workers stop worker
```

Compose automatically reads the existing `.env`. Do not overwrite it, print expanded `docker compose config`, or copy credentials into source files or build arguments. Environment variables set in PowerShell override `.env`. The scripts never run `docker compose down` or remove development volumes.

Run from the repository root:

```sh
set -a
. ./.env
set +a
(cd backend && go run ./cmd/worker)
```

Or use Compose after applying the migrations:

```sh
docker compose --profile workers up -d --build worker
docker compose --profile workers logs -f worker
docker compose --profile workers stop worker
```

Compose builds a backend image with both executables and runs `tutti-worker` as UID 65532. The separate `test` Dockerfile target contains development tools; the default `runtime` target remains a scratch image with system CA certificates and no test tools. Set `WORKER_DATABASE_URL` to the Compose database address, using `postgres` as the host for the bundled database. For a host process use `DATABASE_URL`. Encode special characters in database credentials. The worker profile depends on healthy PostgreSQL and startup checks that the delivery migration is available. `on-failure:5` provides bounded automatic restarts; see [Compose service configuration](https://docs.docker.com/reference/compose-file/services/). After configuration or sustained database failure, fix the cause and explicitly recreate/start the worker. A running container is not proof that SMTP works; use delivery outcomes and queue age below.

Both SIGINT and SIGTERM stop new iterations. A running database expiration batch is cancellable and rolls back if interrupted. The email loop finishes or times out the message it already claimed, then has up to five seconds to persist the result. Compose allows 75 seconds for shutdown. After an abrupt stop, uncommitted database work rolls back and abandoned email claims become eligible when their leases expire.

## Configuration

| Variable | Default and behavior |
| --- | --- |
| `DATABASE_URL` | Required for the worker process. |
| `WORKER_DATABASE_URL` | Compose override for the worker container's `DATABASE_URL`. |
| `WORKER_EXPIRATION_INTERVAL` | `10s` between expiration batches; whole seconds, 1s–1h. |
| `WORKER_EXPIRATION_BATCH_SIZE` | `50`, range 1–500. |
| `WORKER_EMAIL_INTERVAL` | `5s` between email batches; whole seconds, 1s–1h. |
| `WORKER_EMAIL_BATCH_SIZE` | `20`, range 1–100. |
| `WORKER_EMAIL_MAX_ATTEMPTS` | `5`, range 1–20, including claimed attempts interrupted by an abrupt stop. |
| `WORKER_EMAIL_LEASE` | `90s`. Must exceed the SMTP send timeout plus ten seconds and be at most one hour. |
| `WORKER_EMAIL_RETRY_BASE` / `WORKER_EMAIL_RETRY_MAX` | `30s` / `30m`. Cap must be at least the base and at most 24 hours. |
| `SMTP_HOST` / `SMTP_FROM` | Required. Use the provider's hostname and permitted plain sender address. |
| `SMTP_PORT` | `587`. |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Configure both for SMTP PLAIN authentication over TLS, or leave both empty for a trusted relay. |
| `SMTP_PASSWORD_FILE` | Optional runtime-mounted secret file instead of `SMTP_PASSWORD`. At most 4096 bytes; one trailing LF/CRLF is removed. Both password sources together are rejected. The container user must be able to read it. |
| `SMTP_TLS_MODE` | `starttls` requires an upgrade. `implicit` starts TLS immediately, commonly on port 465. Both verify the certificate and host. `plain` is restricted to unauthenticated loopback tests. |
| `SMTP_CA_FILE` | Optional runtime-mounted PEM CA bundle appended to system roots, for a private university CA. It never disables hostname or certificate verification. Leave empty for publicly trusted providers. Not permitted with `plain`. |
| `SMTP_CONNECT_TIMEOUT` | `5s`, at most the send timeout. |
| `SMTP_SEND_TIMEOUT` | `20s` for the whole SMTP operation, at most 60 seconds. |

Use a provider that supports the configured TLS and authentication modes, and keep credentials in your deployment's secret configuration. The worker validates settings at startup and retries runtime SMTP failures. Keep API and worker host clocks synchronized for session deadlines. Claim leases and retry schedules use PostgreSQL time.

Get the hostname, submission port, STARTTLS/implicit mode, approved sender, authentication method and relay/network permissions from the university mail administrator. This adapter supports SMTP PLAIN after verified TLS; it does not implement OAuth2 or provider-specific sign-in. SPF/DKIM/DMARC and sender authorization are provider/deployment responsibilities. Never solve a certificate error by disabling verification. The optional CA file must come from a trusted administrator.

Environment credentials are supported. For runtime secret mounts, supply an operator-owned Compose override, for example:

```yaml
# compose.smtp-secrets.yaml (operator configuration, paths contain no secret values)
services:
  worker:
    environment:
      SMTP_PASSWORD: ""
      SMTP_PASSWORD_FILE: /run/secrets/smtp_password
    secrets:
      - smtp_password
secrets:
  smtp_password:
    file: ${SMTP_SECRET_HOST_FILE:?Set a protected absolute secret-file path}
```

```powershell
$env:SMTP_SECRET_HOST_FILE = 'C:/secure/tutti/smtp-password.txt'
docker compose -f compose.yaml -f compose.smtp-secrets.yaml --profile workers up -d --no-deps --build worker
```

Store the actual secret file outside the checkout with restrictive Windows ACLs. `SMTP_PASSWORD_FILE`/`SMTP_CA_FILE` are **container** paths: setting the variable alone does not mount a file. Mount a CA bundle read-only in an override if needed. For rotation, stop the worker, replace the protected secret, then run the same Compose files with `up -d --no-deps --force-recreate worker`; inspect safe logs and repeat the authorized probe. Do not put university credentials in CI; normal CI uses generated test certificates and fake credentials only.

## Expiration

The worker calls Scheduling's existing expiration transaction with the configured batch size. `FOR UPDATE SKIP LOCKED` lets multiple workers take different due requests. Only a still-`REQUESTED` row at or past `expires_at` can become `EXPIRED`. The status update, release of both participant blocks, one history entry, and one expiration notification per participant commit together. A failed history or outbox write rolls the batch back for a later poll. Acceptance and decline enforce the same deadline independently of worker progress. See the [booking lifecycle](scheduling.md#stage-2-session-booking).

## Email claims and retries

Migration 9 adds retry scheduling, attempts, a claim token and lease, and a sanitized last-error category to `notifications`. It also adds a partial index for pending delivery. Existing pending rows enter the queue in their original creation order. Stop workers before rolling this migration back.

A worker claims one due row at a time in a short statement using `FOR UPDATE SKIP LOCKED`. PostgreSQL records the attempt and a fresh lease token before SMTP I/O starts. Finalization checks the token, `PENDING` state, and an unexpired lease, so an expired owner cannot write even before another worker reclaims the record. Abandoned leases can be reclaimed. An abandoned final attempt becomes `FAILED` when the lease expires. Rows are not held locked during SMTP. Batch size controls how many sequential claims a loop processes, not how many leases are reserved in advance.

Temporary SMTP responses, transport failures, timeouts, and TLS or authentication problems retry after exponentially increasing delays up to the configured cap. A permanent SMTP rejection of the message or address becomes `FAILED` immediately. Exhausted deliveries retain their final timestamp and error category. An isolated message failure does not stop the loop.

Retry delay is `min(base × 2^(attempt−1), cap)`, calculated without overflow. Defaults are 30s, 60s, 120s, 240s between five total attempts (30m cap). There is no jitter. PostgreSQL persists `next_attempt_at` relative to finalization time; restarts do not reset it or the attempt count. Authentication/TLS failures retain the existing **bounded** retry policy to allow credential or certificate repair, and require operator attention. Invalid messages and SMTP 5xx envelope/DATA rejections are permanent; 4xx replies retry. Unrecognized transport errors retry within the same budget. Only sanitized categories, not enhanced-status text or raw provider replies, are retained.

The lease must exceed send timeout plus two five-second database budgets. The maximum send timeout is 60s; Compose's 75s grace period accommodates finishing one claimed send plus acknowledgment. Shutdown stops new claims, finishes that send under its deadline, and attempts finalization for at most five seconds. If that fails, the lease recovers normally. Do not reduce the shutdown grace below the configured send budget plus acknowledgment allowance.

1. **Queued** means a `PENDING` record exists in PostgreSQL.
2. **Submitted** means the worker sent the SMTP message data.
3. **Accepted** means the SMTP server returned a positive response after the end of message data. The worker then records `SENT` and the timestamp in PostgreSQL.
4. **Inbox delivery** depends on the receiving systems and may still fail after acceptance.

Recovery is at least once within the retry budget. If SMTP accepts the message and the worker stops before recording `SENT`, a later worker can send it again. Retries use the same SMTP Message-ID for the same notification, but SMTP offers no guaranteed deduplication. The notification's database deduplication key controls record creation, not remote inbox delivery.

| Termination point | Durable outcome and recovery |
| --- | --- |
| Before claim commit | The transaction rolls back; no committed attempt or lease. |
| After claim, before/during SMTP | The lease expires and work can be reclaimed; interrupted claims count as attempts. |
| After SMTP acceptance, before acknowledgment | Retry can duplicate delivery with the same Message-ID. |
| After SMTP failure, before retry persistence | The existing lease expires; another attempt can recover it. |
| Final abandoned attempt | The next exhaustion sweep records `FAILED` with `lease_expired`. |

Failed rows remain in PostgreSQL. There is no automatic terminal requeue or cleanup. The proposed one-year content retention needs the university DPO review described in [database retention](database/implementation.md#implementation-choices-and-retention). A failure after an ambiguous last attempt does not prove the recipient received nothing.

Coordinator activation is a distinct direct send from trusted provisioning. The SMTP transport implements Identity's activation mail interface so provisioning can reuse it. Raw activation tokens never enter the notification outbox. See [Identity activation](identity.md#authorization-and-activation).

## Operations and tests

Check worker JSON logs for per-message `notification_id`, `attempt`, `state`, `failure_category`, `retry_after`, `lease_until` (UTC), and `duration_ms`, plus batch totals. Docker's container identity identifies the worker instance. `sent` is logged only after database acknowledgment; `finalize_error` and `claim_lost` indicate delivery ambiguity. Logs omit message bodies, addresses, SMTP replies, claim tokens, credentials, and activation tokens. `retry_after` is the requested delay; query PostgreSQL for the authoritative next timestamp. No public diagnostics endpoint or new monitoring service is added.

Use the read-only [operator query](../backend/ops/notifications.sql) through a restricted database session. It reports pending, due, leased, delayed retries, expired leases, terminal failures, oldest pending age, latest successful acknowledgment and failure categories. For this local Compose instance:

```powershell
Get-Content -Raw backend/ops/notifications.sql | docker compose exec -T postgres sh -c 'exec psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"'
```

Use a least-privilege reporting role in production. For one notification, use a parameterized client or a psql variable supplied with `-v notification_id=<UUID>`:

```sql
SELECT id, email_status, attempt_count, last_error, created_at,
       next_attempt_at, lease_until, email_sent_at, email_failed_at
FROM notifications WHERE id = :'notification_id'::uuid;
```

Check the due queue's age, whether a lease is current, SMTP credentials, relay access, and certificate configuration before retrying a failed message operationally. A final failure may correspond to an ambiguous last attempt, so verify with the provider before resending if a duplicate would matter.

| Symptom/category | Operator action |
| --- | --- |
| Startup configuration error | Fix the named setting; keep credentials out of diagnostic output. Check migrations and DB role if the schema check fails. |
| `smtp_tls` | Confirm port/mode, certificate expiry, hostname and trusted CA. Check for a proxy intercepting TLS. No plaintext fallback is permitted. |
| `smtp_auth` | Check supported PLAIN authentication, credentials/app password, sender/relay policy. Stop the worker while repairing persistent auth failure to avoid exhausting the backlog. |
| `timeout` / `transport` | Check DNS, Docker egress, university VPN/firewall and SMTP submission access. On Windows use `Test-NetConnection $env:SMTP_HOST -Port ([int]$env:SMTP_PORT)`; host connectivity alone does not prove container access. |
| `smtp_temporary` | Inspect provider health/rate limits using provider-side logs and the stable Message-ID; observe persisted retry time. |
| `smtp_permanent` / `invalid_message` | Inspect the authorized source record or sender/recipient policy privately. Fix the cause; do not automatically resend terminal rows. |
| Repeated `claim_lost`, `finalize_error`, expired leases | Check DB connectivity, clocks, timeout/lease settings, process restarts and shutdown grace. Reclaim occurs automatically; do not delete leases or rows to unstick work. |
| Old pending queue with no progress | Confirm worker is running, schema/configuration is valid, due timestamps are reached, and logs show iterations. Start/recreate the worker after fixing the cause. |

There is no guaranteed inbox receipt metric: `SENT` is SMTP acceptance acknowledged in the database. Alert thresholds for queue age and failure counts belong to deployment monitoring; none is invented here.

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The integration suite uses disposable PostgreSQL and Redis and a loopback SMTP test server that accepts messages locally and simulates 4xx and 5xx replies. It does not contact a production relay. The worker also delivers the attendance notifications described in the [Coordination guide](coordination.md).

Unit/protocol tests generate a temporary CA and verify STARTTLS, implicit TLS, authentication, untrusted/expired/wrong-host certificates, header injection, Unicode formatting, final DATA rejection/disconnects, timeout and cancellation. PostgreSQL tests cover a booking API event through local SMTP, concurrent claims, future retry eligibility after restart, rollback, expired/stale ownership, maximum attempts, lost acknowledgment and stable Message-ID recovery. The Docker smoke test runs the real scratch worker image, verifies `SENT`, checks redacted logs and sends SIGTERM for a clean exit. CI runs these without external SMTP credentials.

## Authorized university delivery verification

This is external verification, separate from automated local tests. Get permission for a specific university recipient and confirm provider prerequisites first. The script below creates a **new disposable** `tutti_smtp_probe` database, queues one non-sensitive uniquely identified notification, and runs the normal worker bootstrap/outbox path with a single-attempt budget. It never drains the development or production queue. It requires an explicit switch and runtime SMTP configuration; it does not load `.env`.

In a dedicated PowerShell session, set actual provider settings (example hostname and sender below are placeholders):

```powershell
$env:SMTP_HOST = 'smtp.provider.example'
$env:SMTP_PORT = '587'
$env:SMTP_FROM = 'approved-sender@university.example'
$env:SMTP_TLS_MODE = 'starttls'
$smtpCredential = Get-Credential -Message 'Authorized university SMTP account'
$env:SMTP_USERNAME = $smtpCredential.UserName
$env:SMTP_PASSWORD = $smtpCredential.GetNetworkCredential().Password
try {
    ./backend/tests/integration/test_university_smtp.ps1 `
        -UniversityRecipient 'authorized-recipient@university.example' -SendLiveEmail
}
finally {
    Remove-Item Env:SMTP_PASSWORD -ErrorAction SilentlyContinue
    $smtpCredential = $null
}
```

For an approved unauthenticated TLS relay leave both auth fields unset. Alternatively set `SMTP_PASSWORD_FILE` to a protected **host** file and leave `SMTP_PASSWORD` unset; this probe script mounts it read-only automatically, as it does a host `SMTP_CA_FILE` when supplied. Normal Compose still needs the explicit mounts described above.

Record the printed notification ID, UTC test time, provider/mode, safe failure category or `SMTP accepted` result, and the recipient's confirmation of inbox/spam arrival with the matching subject ID. Do not attach credentials or private mail content. `SMTP accepted` alone is **not** inbox delivery. The probe database is removed on exit; retain the sanitized test output as evidence. Re-running creates a new test message; after an ambiguous timeout check with the provider/recipient before repeating. Real university delivery remains unverified until this evidence is collected.

## Scheduling realtime events (#87)

After migration 17, the worker's existing expiration transaction also creates dedicated realtime outbox rows through Scheduling persistence triggers. The worker still needs no Redis connection: each API process dispatches committed rows and subscribes to shared Redis fanout, so worker changes reach every API-hosted hub. If APIs are offline, rows remain pending. This does not change SMTP delivery or its outbox. See [realtime delivery](realtime-delivery.md).
