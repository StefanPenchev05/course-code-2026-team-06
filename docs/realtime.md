# Real-time updates and WebSockets

> **Status:** version-1 contract, authenticated gateway and scheduling delivery implemented for #85–#87 on `feat/realtime-websocket-foundation`, 2026-11-23. Booking/availability publication and worker-to-API delivery are described in [realtime delivery](realtime-delivery.md); other module publishers and React integration remain future work. Backend/frontend owner review of the contract remains required; automated tests do not constitute that agreement.
>
> **Tracking:** #84; backend #85–#89; frontend #90–#94. Relevant application modules: Identity, Scheduling, Tutoring, Coordination and Feedback.

## Goal and scope

Tutti's existing REST/HTTP API and PostgreSQL database remain authoritative. An authenticated WebSocket channel sends **small change notifications** after committed writes so React can invalidate and refetch its active data instead of reloading the page. Redis-backed sessions are reused for authentication; sockets do not replace the email notification outbox, workers, domain authorization or database conflict protection.

**Recommended implementation order:** #85 protocol and tests → #86 authenticated Go gateway → #87 booking/availability publication → #90 shared React client → #91/#92 live booking and discovery → #88 coordinator/tutor/feedback publication → #93 UI → #89/#94 delivery and browser integration hardening. Add meaningful tests within *each* issue, not only at the end.

The initial planning review described `frontend/.gitkeep`; the current checked-out branch now includes the React client under `frontend/src`. This gateway task does not modify that client. Frontend owners must reuse its existing authentication and HTTP infrastructure when implementing #90–#94. The Go backend and worker are separate executables; data changed by the worker must become visible to API-hosted WebSocket clients.

## Protocol contract (version 1)

- **Endpoint:** `GET /ws` (browser uses `ws://` for local HTTP or `wss://` for HTTPS), same public origin and existing opaque session cookie. Do not pass secrets, session IDs, or JWTs via URL query parameters or browser local storage.
- **Handshake:** Authenticate using the existing Identity session service **before upgrading**, verify the incoming `Origin` exactly against the configured trusted public origin, and reject missing/mismatched Origin; use current account/role/faculty permissions, not client claims. Fail closed on expired/revoked/suspended sessions.
- **Envelope:** A server-to-client JSON object with `version` (integer, starting at 1), `id` (opaque event UUID), `type` (documented event name), `entityId` (UUID only when required for scoped invalidation), and `occurredAt` (RFC3339 UTC). No private DTO bodies.
- **Example:**

```json
{
  "version": 1,
  "id": "00000000-0000-0000-0000-000000000851",
  "type": "booking.changed",
  "entityId": "00000000-0000-0000-0000-000000000314",
  "occurredAt": "2026-11-23T16:00:00Z"
}
```

The example IDs are illustrative; do not infer their existence in the database. Event IDs enable local deduplication, not guaranteed persistent replay. On a supported version with an unknown type, ignore safely and trigger a conservative visible-query refresh where needed; malformed/unsupported-version messages must not crash the browser. The exact field limits, stable-ID rules and executable fixtures are specified below.

## Event and authorization matrix

All six types below are defined and validated by the version-1 contract. Scheduling
currently publishes **only `booking.changed` and `availability.changed`** through
the implemented outbox and Redis fanout (#87). The other four categories require
producer integration (#88); defining their types does not make those events fire.
The frontend invalidations in this matrix are requirements for #90–#94, not
implemented React behavior.

| Event | Expected committed changes | Intended recipients / authorization | Frontend data to invalidate |
| --- | --- | --- | --- |
| `booking.changed` | Request, accept/decline, cancel, expiry, completion, attendance correction, policy/suspension cancellation | Session's student and tutor; coordinator only when currently authorized for the applicable session/faculty workflow | Student/tutor session lists, request queues, session detail, available actions |
| `availability.changed` | Tutor availability edits, slot changes, reservation creation/release | Owning tutor, and only validated subscribers permitted to view that tutor's public availability | Tutor editor, free slots, tutor profile/discovery |
| `tutor_application.changed` | Application submission, decision, offering eligibility changes | Applicant and coordinator currently authorized for the relevant application/faculty | Application status, review queue, active tutor offerings |
| `coordination.changed` | Issue reports/resolutions, adjudication, flags and authorized disciplinary actions | Faculty-scoped coordinators and affected participants **only when authorized to the corresponding view** | Authorized issue/attendance queues and coordinator summaries |
| `activity.changed` | Completion, feedback, attendance correction affecting derived totals | Tutor and validated subscribers to the relevant publicly visible activity projection | Tutor activity counts, completed hours, ratings |
| `account.access_changed` | Verification, role/faculty/permission change, suspension/reinstatement | Affected account, and authorized administration views as applicable | Current profile, auth state, protected queries; close forbidden views |

All recipients must be resolved **server-side** from authoritative records/permissions. No global broadcast of booking IDs, private grades, feedback comments, email addresses, disciplinary reasons or evidence. A socket may receive more than one event type; the client must invalidate only views it is authorized to refetch. Server-side subscription requests (if implemented) must validate access to **every requested topic**; never trust user/faculty IDs supplied by the browser.

## Delivery and transaction semantics

1. A domain application service validates permissions and runs a PostgreSQL transaction (existing constraints remain authoritative).
2. Scheduling persistence triggers capture the two implemented event categories in the dedicated PostgreSQL `realtime_events` outbox. These rows commit atomically with business changes, history and email notifications as required by the existing workflow.
3. API dispatchers claim committed rows and publish their stable event IDs through Redis pub/sub. Each API subscriber reloads the event, resolves current recipients and queues it through its local hub with current resource authorization. The separate expiration worker's existing PostgreSQL writes use the same capture triggers.
4. The frontend must receive an invalidation and refetch the authoritative HTTP endpoint, preserving forms, filters and focus. This React integration remains unimplemented (#90–#94).

**Failure behavior:** A failed realtime outbox insert rolls back its business transaction; a rolled-back mutation leaves no deliverable event. After commit, an unavailable Redis subscriber or browser cannot undo the business change or its email notification. Dispatch failures retry with the same event ID. PostgreSQL capture is durable, while Redis pub/sub and browser delivery are best-effort: `dispatched_at` records accepted transient fanout, not receipt by every API or browser. There is no per-browser acknowledgement or offline replay. Duplicates are possible and global ordering is not promised. The realtime outbox is separate from the durable email queue. Cross-instance Redis fanout is implemented under #87; production reliability hardening remains #89. See [delivery](realtime-delivery.md) for leases, retries and recovery.

**Reconnect contract:** Best-effort live delivery, **not guaranteed offline replay**. The client reconnects with bounded exponential backoff plus jitter, then refetches visible queries on every successful connection and on auth/faculty transition. A missed event must never leave an indefinitely stale view. Repeated/unknown messages are safe. Logout or an unauthorized handshake stops reconnect attempts until the user authenticates again. Avoid uncontrolled polling and one socket per mounted React component.

## Implemented backend integration (#86)

`bootstrap/identity.go` creates the process-local hub and routes the exact,
unescaped `/ws` path directly to its handler. Every other path keeps the existing
10-second `http.TimeoutHandler`, including unknown paths and REST endpoints.
The server's header/read/write/idle timeout settings are unchanged. The native
HTTP hijack clears connection IO deadlines; socket liveness and write deadlines
then apply. Tests exercise both scaled deadlines and a real connection lasting
beyond the production ten-second REST deadline.

Identity's `Handler.SessionCheck` captures exactly one correctly named existing
session cookie (`tutti_session` locally, `__Host-tutti_session` on HTTPS). Every
invocation calls the existing `Service.Authenticate`: the Redis session key must
exist and PostgreSQL must return an active student or coordinator. No session
TTL is renewed by the socket. Repeated session cookies are refused. Origin must
appear exactly once and exactly equal validated `PUBLIC_ORIGIN`, including
scheme and port. Missing Origin, `null`, alternate schemes and other origins
are rejected even for native clients. No query string or subprotocol is accepted.
Only GET may upgrade; HEAD and other methods return 405.

Handshake failures return safe JSON: 401 for missing/invalid/expired/revoked
sessions, 403 for forbidden accounts or Origin, 400 for query/subprotocol claims,
429 for connection limits, and 503 for unavailable authentication infrastructure
or a stopped hub. Malformed protocol handshakes are rejected by the library.
Authentication and resource failures never expose backend errors or credentials.

The hub is implemented in `internal/platform/realtime`, with a narrow publisher
contract in `internal/realtime/application`. It uses pinned
[`github.com/coder/websocket` v1.8.15](https://github.com/coder/websocket/releases/tag/v1.8.15)
(native WebSocket, context-aware IO, compatible with Go 1.24); no Socket.IO,
compression, subprotocol or JSON control protocol is used.

## Frontend integration notes (for #90 onward)

The current branch contains the React HTTP client, but no application WebSocket client, event-driven query invalidation, browser event-ID deduplication or reconnect/refetch integration. For #90, place a singleton-per-auth-context socket manager under its existing shared infrastructure/provider code; component hooks subscribe to typed invalidation events, never instantiate uncoordinated independent sockets. Reuse the existing HTTP client and feature refresh patterns. The client must reconnect with bounded backoff and jitter, renew authorized availability watches and refetch visible HTTP data on every successful connection, even if no event arrives. State-changing API responses still determine whether a mutation succeeded; WebSocket messages must not substitute for an authoritative write response.

## Verification checklist

- [x] Contract fixtures/tests cover all six types, truncated/trailing JSON, incorrect scalar types, invalid/missing IDs, duplicate fields and event delivery, forbidden account identifiers, and unknown versions/types (#85). Unauthorized subscription messages are covered by gateway tests (#86).
- [x] Handshake rejects absent session, expired/revoked/suspended session, invalid Origin and untrusted scope (#86).
- [x] Normal REST requests retain timeouts while a persistent WebSocket survives the request timeout (#86).
- [x] Scoped gateway tests isolate users/coordinators/faculties and cover slow clients, heartbeat and shutdown (#86). Domain-specific recipient policies and production load remain #87–#89.
- [x] Backend commit/rollback, affected-socket delivery and reconnect/refetch are covered for Scheduling (#87). React tab behavior and other module publishers remain #88/#90–#94.
- [x] Worker-originated expiration, suspension/policy cancellation capture and attendance corrections generate scheduling events (#87). Broader coordination/account event publishers remain #88.
- [ ] Browser e2e uses independent student/tutor/coordinator contexts; no reload is needed; reconnect resync works (#90–#94).
- [ ] The relevant Go race/integration and frontend tests run in CI; document any environment limitations.

## Out of scope for foundation (#85–#86)

Do not implement chat, presence, typing indicators, websocket-based writes, public raw-event broadcasting, separate user auth, an unrelated queue system or a React scaffold during the Go gateway task. Do not claim the frontend is complete merely because the backend gateway works.

## Exact wire format and fixtures (#85)

`internal/realtime/application.Event` is the entire wire object. There is no
`payload`, free-form metadata, recipient list, user/faculty ID, sequence, token,
message body or private DTO. Only the five documented, case-sensitive keys are
allowed; `entityId` is omitted for `account.access_changed`.

| Type | `entityId` meaning |
| --- | --- |
| `booking.changed` | Session UUID |
| `availability.changed` | Tutor UUID, never private booking/slot participant details |
| `tutor_application.changed` | Application UUID |
| `coordination.changed` | Authorized case/report/flag UUID; refetch the permitted queue |
| `activity.changed` | Tutor UUID |
| `account.access_changed` | Omitted; recipient refetches their profile or permitted administration list |

Frames are UTF-8 JSON text, at most **1024 bytes**. Version is integer `1`.
IDs are non-nil, lowercase, hyphenated UUIDs. `NewEvent` uses cryptographic UUIDv4
randomness. `occurredAt` is a nonzero canonical UTC RFC3339 timestamp (`Z`, optional
fractional seconds as emitted by Go RFC3339Nano). Publisher validation rejects
unsupported versions/types and invalid IDs/timestamps. `Decode` additionally
rejects malformed/trailing JSON, duplicate or unknown object keys, wrong field
case, nulls and oversized input. There is no dynamic payload escape hatch.

Generate the ID **once per logical event** and retain it across recipients,
dispatch retries and fanout. The implemented scheduling outbox assigns and
persists this UUID at capture, coalescing the same event type/entity within a
transaction. Never regenerate an ID on each retry. Different invalidations from
one transaction may have separate IDs; timestamps do not establish a total order.
The hub deliberately accepts repeated event IDs and deduplicates candidate users
within one publication only. The Redis receiver additionally suppresses its
4,096 most recent event IDs in process-local memory. Neither mechanism provides
offline replay or replaces the required bounded browser deduplication cache.

Fixtures live in
`backend/internal/realtime/application/testdata/protocol.json`. `event_test.go`
also exercises every truncated prefix of a valid message and trailing JSON values
directly, since malformed JSON cannot be embedded as a raw JSON fixture value.
Together these cover all six event types, incorrect scalar types, invalid/missing
IDs, identical/conflicting duplicate `id` fields, forbidden account identifiers
and invalid/unknown fields. Duplicate fields within one object are rejected;
repeated delivery of a valid event in separate messages preserves its ID and is
accepted for consumer deduplication. That test models the client ID check; it does
not establish that React implements it. Gateway tests
exercise explicit user/faculty subscription attempts; all client application
messages are rejected, including binary messages, empty messages and JSON
`subscribe` commands. Only standard WebSocket ping/pong/close control frames are
supported. No WebSocket subscription-message protocol exists; #87 registers view watches through authenticated HTTP.

Future clients should keep a bounded recent-ID cache, ignore duplicates, safely
ignore unknown event types on version 1 and conservatively refetch visible
queries. Invalid frames and unsupported versions must be ignored/reported without
crashing or entering an immediate reconnect loop. Do not interpret IDs as proof
of authorization or render identifiers in place of authorized HTTP data.

## Lifecycle, limits and authorization (#86)

Defaults are code-owned (`realtime.DefaultConfig`) and validated at construction;
no new environment variables or migrations are required.

| Bound | Default |
| --- | --- |
| Total connections, including reserved upgrades | 1024 per API process |
| Concurrent connections per authenticated user | 4, across browser sessions/tabs |
| Per-connection outbound queue | 32 events |
| Message/read limit | 1024 bytes; compression disabled |
| Quiet-session revalidation interval | 15 seconds |
| Each authentication/resource-policy check | 3 seconds |
| Ping interval / pong timeout | 20 seconds / 5 seconds |
| Each application write | 5 seconds |
| Close-handshake grace | 250 milliseconds, then close hijacked network IO |
| API shutdown budget | Existing 10 seconds, including socket cleanup |

Sessions are checked before upgrade, immediately after upgrade, every 15 seconds
on quiet connections, and again before each event's authorization/write. The
quiet-socket revocation detection budget is approximately 18 seconds plus close
grace and scheduling overhead, not instantaneous logout propagation or a strict
wall-clock SLA. This is up to one 15-second interval before the next check, plus
up to three seconds for that check; context-aware adapters must stop when their
budget expires. The post-upgrade check closes a connection whose access changed
during the handshake, even if the browser has already received HTTP 101. Each event
uses a fresh principal. A verification, student/tutor role, program, coordinator
faculty, suspension or closure transition closes the socket so the client must
refresh its auth context. Revoking one browser session closes only sockets using
that session, while other valid sessions for the same account remain usable.
Redis or PostgreSQL authentication failures fail closed. Resource authorization
failure also fails closed; a negative policy result drops that event.

Authentication permits pending/rejected students to connect for their own access
updates; it does **not** grant them booking, tutor or faculty event access. The
mandatory event authorization callback must enforce the corresponding HTTP view's
rules, including verification, participation, tutor ownership, home/tutor faculty
and cross-program subject eligibility as applicable. Ordinary students, tutors,
and coordinators cannot subscribe to another user or faculty. Faculty delivery
uses explicit server-selected coordinator user IDs and current policy checks;
there is no faculty room or global broadcast API. Scheduling publishers now feed this gateway through the dedicated #87 outbox and Redis fanout; see [delivery](realtime-delivery.md).

Full queues disconnect the slow connection instead of blocking publishers. The
single writer serializes event writes and pings; a separate reader handles
control frames and a watchdog checks quiet sessions. Hub locks protect connection
maps/reservations and are never held across authentication, policy or network IO.
Cancellation joins handler-owned goroutines and releases connection counts.
Shutdown rejects new upgrades, attempts close, force-bounds unresponsive peers,
and waits for hijacked sockets explicitly: `http.Server.Shutdown` does not own
them. Close codes are best-effort: 1008 for access/policy violations, 1013 for
queue saturation, 1001 for shutdown/lifecycle end; network failures can appear as
abnormal closure. No close reason, log or message contains credential or private
business data. A close is a resync signal, not an authorization decision for UI.

Authorization is a point-in-time check; an account/resource change can race an
already authorized network write. This transport cannot revoke bytes already
sent. Event payload minimization, HTTP authorization on every refetch and periodic
revalidation remain necessary. Auth/policy implementations must honor context
cancellation and be safe for concurrent calls. Deployment rate limiting, metrics,
high-load sizing and distributed revocation/fanout hardening remain #89.

A reverse proxy must forward `/ws` with HTTP/1.1 Upgrade and Connection headers,
preserve the real browser Origin, support long-lived connections, and terminate
TLS for `wss://` where applicable. Its idle timeout must exceed the heartbeat
interval. Do not add wildcard origins, trust proxy-supplied user IDs, or move
session credentials to a URL. Reconnect/refetch remains mandatory behind proxies
and across API restarts. No frontend/proxy configuration is added in this task.

## Scheduling publication (#87)

[Scheduling delivery](realtime-delivery.md) is now implemented. Migration 17
captures invalidations transactionally; API dispatchers claim committed events
and publish their stable UUIDs through shared Redis fanout. Every API reloads the
event, resolves current recipients through Scheduling's published read port and
uses `Publisher.Publish(context.Context, Event, []string, Authorize)` with current
resource authorization. Expiration workers need only their existing PostgreSQL
write path. Email outbox rows remain independent.

Public availability viewers register authorized, expiring watches through HTTP;
these leases are shared across API instances. Client WebSocket application
messages remain unsupported. The delivery guide defines the new watch endpoint,
claim/retry semantics, migration ordering, rollback guard, tests and exact handoff
for frontend #90 and reliability #89. Other producers (#88) must preserve these
post-commit, minimal-payload and server-owned authorization boundaries.

## Verification and remaining review

Focused race tests cover strict Origin/handshake denial, versioned JSON fixtures,
recipient policy, multiple tabs, revoked/expired/suspended sessions, permission
changes, malformed/subscription input, limits, backpressure, heartbeat, concurrent
cleanup and bounded shutdown. Bootstrap tests prove REST 503 timeouts and sockets
surviving both HTTP IO deadlines and the actual ten-second REST duration.
`internal/platform/realtime/hub_races_test.go` uses explicit upgrade/policy
barriers to cover shutdown with a reserved handshake or in-flight authorization,
concurrent publication, repeated connection closures and client cleanup. It also
covers revocation, suspension and faculty changes between pre- and post-upgrade
checks (both quiet sockets and queued events), stale subscription attempts, and
user/faculty resource access lost while an event is queued. Handler-completion
signals and bounded shutdown assert that reservations are removed and the
handler-owned reader/watchdog goroutines are joined. FIFO sentinel events prove
denied events were dropped without relying on arbitrary negative sleep windows.
`tests/integration/realtime_test.go` uses native sockets, real Redis sessions and
real migrated PostgreSQL accounts: cookie handling, coordinator faculty isolation,
logout, real Redis TTL expiration, suspension, faculty change and unavailable
Redis/PostgreSQL are covered. It is included by the existing disposable runner:

```sh
(cd backend && go test -race ./... && go vet ./... && go build ./...)
bash backend/tests/integration/test_identity.sh -race
```

Tests use synthetic, explicitly scoped events; there is no public test-publish
endpoint. Scheduling publication, rollback/retry and worker fanout now have separate #87 tests described in [delivery](realtime-delivery.md). Production load and React/browser journeys remain separate work. The #85 human agreement criterion
still needs backend/frontend owner review. No GitHub issue is closed by these
changes.

Implementation file inventory for the original #85/#86 foundation (see [delivery](realtime-delivery.md) for #87 additions):

- Contract: `backend/internal/realtime/application/event.go`, `event_test.go`
  and `testdata/protocol.json` in that directory.
- Transport: `backend/internal/platform/realtime/hub.go`, `http.go` and
  `hub_test.go`.
- Identity bridge: `backend/internal/identity/interfaces/http/session_check.go`.
- Wiring and timeout regression tests: `backend/internal/bootstrap/identity.go`,
  `realtime.go` and `realtime_test.go`.
- Real-service tests: `backend/tests/integration/realtime_test.go`.
- Pinned dependency: `backend/go.mod` and `backend/go.sum`.
- Documentation: this guide and `README.md`. No migrations, environment changes,
  frontend implementation or business publishers are part of the patch.
