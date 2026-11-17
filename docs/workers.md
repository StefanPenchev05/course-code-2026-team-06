# Background workers and email delivery

The API writes notification records to PostgreSQL as part of business transactions. The separate `tutti-worker` process runs one expiration loop and one email loop. Both start immediately, process bounded batches, and wait between iterations. Worker instances coordinate through PostgreSQL. Redis is only needed by the API, and the API can queue notifications while SMTP or the worker is unavailable.

## Start and stop

Use [the database setup commands](database/implementation.md) to start PostgreSQL and apply all migrations before starting a worker. Copy `.env.example` to `.env` if needed. Set `SMTP_HOST`, `SMTP_FROM`, and the provider's port, TLS mode, and credentials. Use a test recipient and a local mail sink for local trials so test records do not go to real users.

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

Compose builds a backend image with both executables and runs `tutti-worker`. Set `WORKER_DATABASE_URL` to the Compose database address, using `postgres` as the host for the bundled database. For a host process use `DATABASE_URL`. Encode special characters in database credentials. The worker profile depends on healthy PostgreSQL and startup checks that the delivery migration is available.

Both SIGINT and SIGTERM stop new iterations. A running database expiration batch is cancellable and rolls back if interrupted. The email loop finishes or times out the message it already claimed, then has up to five seconds to persist the result. Compose allows 75 seconds for shutdown. After an abrupt stop, uncommitted database work rolls back and abandoned email claims become eligible when their leases expire.

## Configuration

| Variable | Default and behavior |
| --- | --- |
| `DATABASE_URL` | Required for the worker process. |
| `WORKER_DATABASE_URL` | Compose override for the worker container's `DATABASE_URL`. |
| `WORKER_EXPIRATION_INTERVAL` | `10s` between expiration batches. |
| `WORKER_EXPIRATION_BATCH_SIZE` | `50`, range 1–500. |
| `WORKER_EMAIL_INTERVAL` | `5s` between email batches. |
| `WORKER_EMAIL_BATCH_SIZE` | `20`, range 1–100. |
| `WORKER_EMAIL_MAX_ATTEMPTS` | `5`, range 1–20, including claimed attempts interrupted by an abrupt stop. |
| `WORKER_EMAIL_LEASE` | `90s`. Must exceed the SMTP send timeout plus ten seconds and be at most one hour. |
| `WORKER_EMAIL_RETRY_BASE` / `WORKER_EMAIL_RETRY_MAX` | `30s` / `30m`. Cap must be at least the base and at most 24 hours. |
| `SMTP_HOST` / `SMTP_FROM` | Required. Use the provider's hostname and permitted plain sender address. |
| `SMTP_PORT` | `587`. |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | Configure both for SMTP PLAIN authentication over TLS, or leave both empty for a trusted relay. |
| `SMTP_TLS_MODE` | `starttls` requires an upgrade. `implicit` starts TLS immediately, commonly on port 465. Both verify the certificate and host. `plain` is restricted to unauthenticated loopback tests. |
| `SMTP_CONNECT_TIMEOUT` | `5s`, at most the send timeout. |
| `SMTP_SEND_TIMEOUT` | `20s` for the whole SMTP operation, at most 60 seconds. |

Use a provider that supports the configured TLS and authentication modes, and keep credentials in your deployment's secret configuration. The worker validates settings at startup and retries runtime SMTP failures. Keep API and worker host clocks synchronized for session deadlines. Claim leases and retry schedules use PostgreSQL time.

## Expiration

The worker calls Scheduling's existing expiration transaction with the configured batch size. `FOR UPDATE SKIP LOCKED` lets multiple workers take different due requests. Only a still-`REQUESTED` row at or past `expires_at` can become `EXPIRED`. The status update, release of both participant blocks, one history entry, and one expiration notification per participant commit together. A failed history or outbox write rolls the batch back for a later poll. Acceptance and decline enforce the same deadline independently of worker progress. See the [booking lifecycle](scheduling.md#stage-2-session-booking).

## Email claims and retries

Migration 9 adds retry scheduling, attempts, a claim token and lease, and a sanitized last-error category to `notifications`. It also adds a partial index for pending delivery. Existing pending rows enter the queue in their original creation order. Stop workers before rolling this migration back.

A worker claims one due row at a time in a short statement using `FOR UPDATE SKIP LOCKED`. PostgreSQL records the attempt and a fresh lease token before SMTP I/O starts. Finalization checks the token so a worker with an older claim cannot overwrite a newer worker's result. Abandoned leases can be reclaimed. An abandoned final attempt becomes `FAILED` when the lease expires.

Temporary SMTP responses, transport failures, timeouts, and TLS or authentication problems retry after exponentially increasing delays up to the configured cap. A permanent SMTP rejection of the message or address becomes `FAILED` immediately. Exhausted deliveries retain their final timestamp and error category. An isolated message failure does not stop the loop.

1. **Queued** means a `PENDING` record exists in PostgreSQL.
2. **Submitted** means the worker sent the SMTP message data.
3. **Accepted** means the SMTP server returned a positive response after the end of message data. The worker then records `SENT` and the timestamp in PostgreSQL.
4. **Inbox delivery** depends on the receiving systems and may still fail after acceptance.

Recovery is at least once within the retry budget. If SMTP accepts the message and the worker stops before recording `SENT`, a later worker can send it again. Retries use the same SMTP Message-ID for the same notification, but SMTP offers no guaranteed deduplication. The notification's database deduplication key controls record creation, not remote inbox delivery.

Coordinator activation is a distinct direct send from trusted provisioning. The SMTP transport implements Identity's activation mail interface so provisioning can reuse it. Raw activation tokens never enter the notification outbox. See [Identity activation](identity.md#authorization-and-activation).

## Operations and tests

Check worker logs for expiration counts and email sent, retry, failure, or lost-claim counts. Logs omit message bodies, addresses, SMTP replies, credentials, and activation tokens. To investigate a backlog or failed attempts, use a restricted database session:

```sql
SELECT email_status, last_error, count(*)
FROM notifications
GROUP BY email_status, last_error
ORDER BY email_status, last_error;
```

Check the due queue's age, whether a lease is current, SMTP credentials, relay access, and certificate configuration before retrying a failed message operationally. A final failure may correspond to an ambiguous last attempt, so verify with the provider before resending if a duplicate would matter.

```sh
(cd backend && go test -race ./... && go vet ./...)
backend/tests/integration/test_identity.sh -race
python3 backend/tests/integration/test_schema.py
```

The integration suite uses disposable PostgreSQL and Redis and a loopback SMTP test server that accepts messages locally and simulates 4xx and 5xx replies. It does not contact a production relay. The worker also delivers the attendance notifications described in the [Coordination guide](coordination.md).
