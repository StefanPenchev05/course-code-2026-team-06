# Realtime operations and reliability (#89)

The API retains the #85–#88 contract, gateway, database capture and producers. #89
adds deployment namespacing, bounded operational measurements and independent
process recovery/load tests. No migration, frontend, SMTP or business policy changes
are required. See [delivery](realtime-delivery.md) for the six event categories and
their authorization policies, and [measured results](realtime-load-results.md).

## Configuration and coordinated upgrade

`REALTIME_NAMESPACE` is **required** for API startup: 1–32 lowercase ASCII letters,
digits, `_` or `-`, starting with a letter. Example: `tutti-dev`. Configure the same
value on every API consuming one PostgreSQL outbox. Different deployments require
different values **and separate databases/outboxes**. Different namespaces must
not compete for the same outbox: a dispatch acknowledgment belongs to one fanout.

The exact channel is `tutti:<namespace>:realtime:v1`; only event UUIDs travel on it.
Watch keys start with `tutti:{realtime:<namespace>}:watches:`. The hash tag keeps a
deployment's Lua watch indexes in one Redis slot. There are no wildcard
subscriptions or browser-selectable channels. The old constructors are retained
for existing isolated fixtures; bootstrap always uses validated scoped adapters.

Redis database numbers **do not isolate pub/sub**; this is a Redis property, not
a Tutti replay mechanism. [Redis documentation](https://redis.io/docs/latest/develop/pubsub/)
describes both database scope and transient delivery. Namespaces isolate realtime
keys/channels; Identity session keys remain unchanged. Use an isolated Redis
instance (preferred) or separately secured session storage for each deployment.
A different DB number without appropriate access restrictions is not a security
boundary. Keep Redis private and use TLS/credentials as appropriate to deployment.

ACLs must permit the deployment's exact pub/sub channel and watch key prefix,
Identity's existing session commands/keys, `PING`, `PUBSUB NUMSUB` (legacy startup
check), and the sorted-set, `TIME`, expiry, `EVALSHA`/`EVAL`/script commands used by
the existing watch adapter. Never grant browser access or log connection URLs.
Review the complete command set against the deployed Redis version; the example
namespace is not a ready-made least-privilege ACL policy.

**Mixed legacy/namespaced API instances are unsupported.** A new API refuses
startup if the old `tutti:realtime:v1` channel has subscribers; missing/invalid
namespace configuration also fails startup. This check is a rollout guard, not
a distributed deployment lock: an old binary can still be started afterward.
Prevent that operationally; do not rely on this check to police an old scheduler.

1. Apply migrations through 18 first, preserving all durable rows.
2. Remove old APIs from the load balancer, stop and drain them, and confirm no
   legacy channel subscribers remain. Stop automatic restarts of old replicas.
3. Set identical namespace configuration on all replacement APIs. Start them,
   wait for readiness and then restore traffic. Worker capture can continue
   while APIs are stopped; pending rows dispatch when APIs return.
4. Clients reconnect, refetch current HTTP data and renew availability watches.
5. For rollback to the old binary, drain/stop **all** namespaced APIs before
   restoring all legacy APIs. Do not roll back migrations merely to undo #89:
   no schema change was added. Keep producers/routers compatible with migration 18.

For subsequent upgrades using the same namespace and compatible contract, remove
one replica from new traffic, signal graceful shutdown, allow at least 15 seconds
termination grace for its existing 10-second shutdown budget, then replace it.
Existing sockets close; no sticky sessions or automatic historical replay are
required. Capacity must cover the remaining replicas during the rollout.

## Monitoring interface and semantics

`REALTIME_MONITOR_ADDR` optionally enables a **separate loopback-only** HTTP
listener, for example `127.0.0.1:9091`. Empty disables the listener; sampling and
aggregate operational logs continue. Literal IPv4/IPv6 loopback is required;
wildcards, hostnames and non-loopback addresses fail configuration validation.
Never proxy this listener to the public Internet. A local collector may scrape
it; container collectors/probes need the same network namespace. Each local API
process needs its own port. The public API has no metrics route.

| Endpoint | Meaning |
|---|---|
| `GET /livez` | Process monitoring HTTP handler is alive; no dependency check. |
| `GET /readyz` | 200 only with a current subscription and a successful PostgreSQL, Redis and outbox sample no older than 15 seconds; otherwise 503. |
| `GET /metrics` | JSON process snapshot; `Cache-Control: no-store`. |

Sample dependencies/outbox every five seconds with bounded operations (one
second per ping, three seconds for the outbox query). Readiness is sampled, not
instantaneous and not a complete proof that every routing query will succeed.
Do not wire dependency readiness to a liveness restart policy: a Redis outage
should trigger recovery, not an API restart loop. Backlog alone does not make a
process unready, so dispatch can recover while operators inspect the lag.

Metrics have fixed names and no domain identifiers or arbitrary label values:

| Group | Measurements and interpretation |
|---|---|
| Connections | Current/peak accepted upgraded sockets; accepted, rejected, disconnected totals; limit rejections. Pending handshakes still count toward hub limits but not the connected gauge. |
| Handshakes | Rejection categories `origin`, `request`, `session`, `dependency`, `upgrade`; limit failures separately. |
| Delivery | Dispatch claims, successful Redis publications, failed attempts, retry schedules, claim/ack/retry persistence failures; processed events and locally suppressed duplicates. Successful publication is **not** browser receipt. |
| Resource/security | Authentication failures, authorization errors and denials; write/heartbeat failures; full-queue drops and slow-client closures. A canceled operation can contribute to failure counters. |
| Recovery | Subscription interruptions, outer receive-loop reconnect attempts, recovered subscription acknowledgments, fanout failures, forced resynchronization operations and shutdown timeouts. Attempts exclude go-redis's internal socket retries. |
| Timings | `handshake`, `publish`, `capture_to_publish`, `process`: count, sum and maximum seconds. No percentile claim is made by these aggregates. Capture lag includes retry/backlog time. Processing ends at queueing, not browser receipt. |
| Outbox | Pending rows, oldest pending age, sum of attempts beyond the first on **pending** rows, expired leases, pending `fanout_failed` count and table/index bytes. |
| Runtime | Current Go heap allocation and goroutine count. Heap is not RSS; neither is a CPU measurement. |

Counters reset on API restart; scrape each replica separately. Outbox measurements
are shared, so do not sum identical backlog across replicas. The pending query
can use the existing partial index; no historical row scan runs on request paths.
Large pending backlogs can still make sampling expensive or time out; consult
`outbox_ready` and `sampled_at`. Aggregate counts cannot identify one failed event.

Every sampler cycle logs changed fixed failure/retry/resync categories at warning
level in one structured record. Successful events/heartbeats are not logged.
Application telemetry never includes cookies, tokens, payloads, email addresses,
grades, comments or disciplinary reasons. No unbounded error strings enter its
metric labels. Redis driver connection diagnostics may additionally report
transport endpoints; secure access to operational logs.

Suggested initial operator alerts (proposals, not measured SLAs): sustained
unready state, increasing oldest-pending age, persistent expired leases,
repeated fanout/authorization errors, queue drops or growing connection/heap
counts after clients disconnect. Tune thresholds from deployment measurements.

## Recovery guarantees and limits

PostgreSQL captures events with the business transaction. Redis failure cannot
undo an already committed business write. New HTTP requests still require the
existing Redis-backed authentication; an outage can prevent authentication.
The separate expiry worker can continue durable writes without Redis or SMTP
delivery being part of its transaction.

API dispatch polls every second, claims with a 30-second lease and retries with
bounded exponential delays (one second to five minutes). Claim tokens fence stale
acknowledgments/retries. Publication followed by a lost acknowledgment can repeat
the same UUID. Each receiver has a 4,096-ID bounded cache, which resets on process
restart/eviction. This is not exactly-once end-to-end delivery or a replay cursor.

A subscriber error/reconnection or processing failure closes local sockets for
resynchronization. Publication counts other Redis subscribers, not successful
browser consumers; another API may acknowledge an event while this API is offline.
Clients must refetch **after every successful connection**, including the first,
and after invalidations; renew authorized ten-minute availability watches. Apply
bounded reconnect backoff with jitter, deduplicate IDs, and use current HTTP data
for delayed/out-of-order invalidations. The tests demonstrate this with controlled
Go clients; React connection/cache behavior and browser E2E remain #90–#94 work.

Limits remain 1,024 connections/API, four/user/API and 32 queued events/socket.
Session revalidation remains every 15 seconds with a three-second check budget,
plus checks before delivery. Idle revocation detection can take approximately
18 seconds plus scheduling/close overhead; it is not instantaneous. Resource
authorization runs before each delivery. Ping every 20 seconds, five-second pong
and write budgets, and bounded cleanup remain unchanged. Multi-replica per-user
limits are local, not a cluster-wide four-tab limit.

## Reverse proxy and TLS

Use HTTPS and `wss://`; configure `PUBLIC_ORIGIN` to the exact public HTTPS origin
(scheme, hostname and port). Forward the browser's original Origin, never replace
it with a trusted constant. The session cookie remains Secure/HttpOnly. Only
`/ws` bypasses Go's ten-second REST timeout; normal API routes retain it.

Example nginx fragment for an existing TLS virtual host (supply the deployment's
certificate configuration separately):

```nginx
# http context
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}
upstream tutti_api {
    server 127.0.0.1:8080;
    server 127.0.0.1:8081;
}
# Inside the existing HTTPS server:
location = /ws {
    proxy_pass http://tutti_api;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;
    proxy_set_header Host $host;
    proxy_set_header Origin $http_origin;
    proxy_read_timeout 75s;
    proxy_send_timeout 75s;
    proxy_buffering off;
}
location /api/ {
    proxy_pass http://tutti_api;
    proxy_set_header Host $host;
    proxy_read_timeout 15s;
}
```

Explicit upgrade headers and suitable idle timeouts follow the [nginx WebSocket
documentation](https://nginx.org/en/docs/http/websocket.html). Seventy-five seconds
exceeds the normal heartbeat interval/budget, but is an operational example,
not a tested production proxy configuration. Test the real load balancer's TLS,
idle timeouts, upgrades, drain behavior and Origin preservation before deployment.
Do not expose the monitor or trust forwarding headers as Identity authority.

## Retention and safe diagnostics

No deletion job or approved retention period exists for realtime rows. Retaining
all dispatched rows is the current behavior. Read-only diagnostics are in
`backend/ops/realtime.sql`; run with an operations account and existing securely
provided `DATABASE_URL`. The optional historical daily aggregation has a
five-second statement timeout and can time out on a large table.

Proposal requiring owner approval: retain dispatched rows for a bounded incident
investigation window (for example 14 days), never delete pending/leased rows,
and only introduce bounded deletion batches after validating audit requirements,
replica lag and recovery assumptions. Monitor relation size, autovacuum and index
growth first. Realtime rows are not the disciplinary/account/session audit history.
No implementation in #89 deletes either history or outbox data.

## Exact Windows PowerShell commands

From the repository root, with Docker Desktop running Linux containers. These
tests create their own databases/services; they never use local `.env`, replace
Compose services or remove developer volumes. The optional SMTP worker need not
be running. Rebuild this existing Dockerfile test target after Go changes:

```powershell
docker build --target test -t tutti-realtime89-tests ./backend
if ($LASTEXITCODE -ne 0) { throw 'Test image build failed' }
$testNetwork = 'tutti-realtime89-' + [guid]::NewGuid().ToString('N')
docker network create $testNetwork
if ($LASTEXITCODE -ne 0) { throw 'Test network creation failed' }
try {
    docker run --rm --network $testNetwork `
        --mount type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock `
        -e "TUTTI_TEST_DOCKER_NETWORK=$testNetwork" `
        tutti-realtime89-tests bash tests/integration/test_identity.sh `
        -race -timeout 300s -run '^TestRealtime'
    if ($LASTEXITCODE -ne 0) { throw 'Realtime integration failed' }
} finally {
    docker network rm $testNetwork
}
```

For the **separate opt-in 200-user workload**, use the same block but add
`-e TUTTI_REALTIME_LOAD=1` before the image name and replace the Go test selector
with `-run '^TestRealtime200Users$'`. Capture the console's
`REALTIME_LOAD_RESULT` JSON. It measures ten-concurrent handshake ramp-up, 200
distinct active users plus ten extra tabs, a ten-transaction availability burst,
private booking/coordination traffic, seeded churn, subscription loss, non-reading
clients and Redis restart. Normal delivery and disruption timings are separate.
The API children are independent OS processes executing production bootstrap
inside the race-instrumented Go test executable. The test seeds valid synthetic
sessions; password-login throughput is not included in handshake measurements.

For the full integration suite omit `-run`; for schema/migration regression use
`python3 tests/integration/test_schema.py` as the container command. The existing
CI runner automatically includes the short process/namespace tests. The heavy
load test is opt-in; no duplicate CI job was introduced.

### Redis restart fixture and CI diagnostics

The host-mode CI runner and the Docker test-network mode both exercise the real
stop/start outage. Host mode selects a free loopback port and passes that **explicit
port** to Docker. An empty/random Docker host-port binding can select a different
port on the next `docker start`, leaving both API clients retrying an abandoned
address indefinitely. Container-name networking masked that fixture defect in
the earlier local run. No production fanout or monitoring change was needed.

The fixture checks that the published endpoint is unchanged, then waits up to ten
seconds for a bounded PING at the endpoint configured in the APIs. Only afterward
does the unchanged 15-second subscription-recovery deadline begin. Recovery
requires **two actual Redis subscribers**, both APIs reporting subscribed, and an
increased recovery counter in each API. A new HTTP booking/acceptance then proves
bidirectional delivery and user/faculty isolation after reconnect. A separate test
stops an API while Redis is still unavailable, checking bounded shutdown.

On failure, diagnostics include container state/published endpoint, Redis PING,
actual subscriber count, both API monitoring snapshots, and a sanitized summary
of the last 16 KiB of each API log. Only fixed numeric operational/transport
categories are emitted; arbitrary log text, credentials and payloads are omitted.

To repeat the CI host-port path on a Linux host with the existing test tools:

```bash
for attempt in 1 2 3 4 5; do
    bash backend/tests/integration/test_identity.sh -race -timeout 180s \
        -run '^TestRealtime(IndependentProcessesRecovery|ShutdownDuringRedisRecovery)$' || exit 1
done
```

Leave `TUTTI_TEST_DOCKER_NETWORK` unset for that host-port regression. Each runner
invocation owns and cleans up its disposable services. The runner deliberately
sets `-count=1`, so use repeated invocations rather than overriding `-count` there.

```powershell
Push-Location backend
try {
    go test -race ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go race tests failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go vet failed' }
    go build ./...
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
} finally { Pop-Location }
git diff --check
```

To run/inspect a local API after supplying its existing Identity/DB configuration
securely (do not print or overwrite `.env`):

```powershell
$env:REALTIME_NAMESPACE = 'tutti-dev'
$env:REALTIME_MONITOR_ADDR = '127.0.0.1:9091'
Push-Location backend
try { go run ./cmd/api } finally { Pop-Location }
# In another PowerShell window while that API is running:
Invoke-RestMethod http://127.0.0.1:9091/metrics
Invoke-WebRequest http://127.0.0.1:9091/readyz
Invoke-WebRequest http://127.0.0.1:9091/livez
psql -X -v ON_ERROR_STOP=1 "$env:DATABASE_URL" -f backend/ops/realtime.sql
```

The worker's existing build/start/inspection and safe local SMTP commands remain
in [workers](workers.md). Its realtime capture needs no namespace or monitor port;
only APIs subscribe/dispatch. No SMTP credentials are needed by these tests.
