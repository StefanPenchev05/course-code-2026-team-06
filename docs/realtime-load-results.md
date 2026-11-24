# Issue #89 verification measurements

Measured 2026-11-24 on branch `feat/realtime-websocket-foundation`, base commit
`5b43e96aee15e556c2c6a0188bf59f4e48a81972` plus the uncommitted #89 changes. No
commit was created. These results describe one local workload, not a production
capacity limit, availability SLA or browser benchmark.

## Environment and method

- Windows development host, Docker Desktop/WSL 2 Linux x86_64 containers.
- Docker reported 32 CPUs and 16,565,542,912 bytes memory; no additional CPU/memory
  quota was set on the test/API container. The host also ran its existing healthy
  development services; those services were not changed or used as fixtures.
- Existing `backend/Dockerfile`, `test` target; Go `go1.26.11`, `-race`,
  `GOMAXPROCS=32`; PostgreSQL 17.12 and Redis 8.2-alpine disposable services.
- Two independent OS processes execute complete production API bootstrap using
  the race-instrumented test executable. Shared fresh PostgreSQL outbox, dedicated
  Redis instance, native WebSocket clients and real network HTTP requests.
- 200 distinct synthetic approved students, ten extra tabs, ten concurrent
  handshakes during ramp-up. Valid opaque sessions were seeded through the
  existing session store; every handshake/delivery uses production Identity.
  Password hashing and browser/TLS/proxy latency are excluded.
- Ten real HTTP availability transactions broadcast to 210 simultaneous sockets.
  Delivery latency is database `occurred_at` to the timestamp captured by the
  client read goroutine, including capture/commit, polling, fanout and authorization.
  It is not UI rendering latency. The test and database share the Docker host clock.
- Mixed phase adds booking creation/acceptance and linked issue creation, two
  fixture participant/coordinator sockets, 12 seeded disconnect/reconnect cycles,
  authorized HTTP refetch and watch renewal. FIFO per-account markers verify that
  the 200 public viewers receive no private booking/coordination identifiers.
- Failure phases follow normal measurements: drop both Redis subscriptions with
  all 212 sockets present, recover, evict two non-reading clients, restart Redis
  and verify zero registered sockets and bounded API shutdown. Default limits
  and timers are unchanged.

## Recorded run

Command: the [PowerShell disposable runner](realtime-operations.md#exact-windows-powershell-commands)
with `TUTTI_REALTIME_LOAD=1`, `-race -timeout 300s` and
`-run '^TestRealtime(IndependentProcessesRecovery|NamespaceIsolation|200Users)$'`.
All three tests passed; the load test took 39.94 seconds.

| Measurement | Observed result |
|---|---:|
| Concurrent load users / sockets | 200 / 210 |
| Additional mixed-phase fixture sockets | 2 |
| Handshake failures | 0 |
| Handshake p50 / p95 / p99 | 1.70 / 26.25 / 46.37 ms |
| Measured availability deliveries | 2,100 |
| Capture-to-client p50 / p95 / p99 | 685.94 / 721.15 / 723.94 ms |
| Burst elapsed / observed throughput | 0.757 s / 2,772 deliveries per second |
| Duplicate browser events in measured burst | 0 |
| Normal queue drops / slow-client closures / processing or authorization errors | 0 / 0 / 0 |
| Churn reconnect/refetch/watch renewals | 12 |
| Non-reading-client eviction | 25.002 s (20-second ping + five-second pong budget) |
| Subscription recovery after Redis start returned | 0.602 s |
| Total API process CPU user / system time | 3.776 / 1.774 CPU-seconds |
| Sampled maximum Go heap / goroutines per API | 34,555,440 bytes / 336 |
| Sampled maximum pending rows | 200 |
| Connections after cleanup | 0 on both APIs |
| Goroutines before load / after cleanup | 11 / 12 per API |
| Heap before load, APIs A / B | 21,648,176 / 21,642,936 bytes |
| Heap after cleanup, APIs A / B | 13,611,408 / 15,803,816 bytes |

CPU times cover both API process lifetimes, including startup, mixed traffic,
failure phases and shutdown; they are not utilization percentages. Heap excludes
race detector shadow memory/native allocations and is not RSS. The monitoring
sampler ran every 100 ms, but outbox snapshots refresh every five seconds: peaks
are observed samples, not guaranteed instantaneous maxima. The 200-row backlog
sample includes the synthetic FIFO markers used to prove isolation, so it must
not be presented as a sustained business-traffic backlog. Backlog drained to zero.
The one-goroutine difference after connection cleanup includes warmed transport
state; both processes exited normally within the bounded shutdown check. This
single run is not a long-duration leak or soak test.

Normal measurements recorded no rejected handshakes, queue drops, write failures,
auth failures, fanout failures or resynchronizations. Failure phases recorded one
heartbeat failure per API, two subscription interruptions/recoveries per API and
four resynchronization operations per API (gap plus acknowledgment recovery for
each interruption). These fault-induced counts are not mixed into normal latency.

The separate process recovery test completed Redis stop/start plus backlog recovery
in 5.670 seconds in this run (6.678 seconds in the final focused rerun). It additionally verifies durable cancellation while
Redis is stopped, volatile session loss/re-login, lost dispatch acknowledgment,
expired-lease reclaim/stale-token rejection, repeated and out-of-order IDs, forced
refetch, API crash/graceful restart and independent-worker expiry while APIs are
offline. It uses fewer connections to keep ordinary CI deterministic and short.

## Coverage boundaries

The test proves this workload completed with production authorization and default
limits on this machine. It does not establish maximum throughput, a 200-user SLA,
WAN behavior, actual React reconnect/cache handling, production TLS/proxy/ACL
configuration, hours-long memory stability or failover of PostgreSQL itself.
Existing deterministic unit tests cover blocked writes, full queues, connection
limits, concurrent cleanup/shutdown and authentication/access-change races.

Early development runs failed on two fixture mistakes: a synthetic cancellation
clock preceded its creation timestamp, and a seeded student's UUID/text SQL
parameter lacked explicit casts. Both fixture errors were corrected. These were
not gateway, publisher or transaction defects. The successful results above were
collected after those corrections; no production limits were relaxed to pass.
