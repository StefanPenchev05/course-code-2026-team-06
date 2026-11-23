# Real-time updates and WebSockets

> **Status:** version-1 contract and authenticated gateway implemented for #85/#86 on `feat/realtime-websocket-foundation`, 2026-11-23. Domain publishers, worker delivery and React integration remain future work. Backend/frontend owner review of the contract remains required; automated tests do not constitute that agreement.
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
2. The transaction commits changes, history and email outbox as required by existing business rules.
3. The realtime publisher makes a **post-commit** notification available to authorized connections. Do **not** emit from an uncommitted transaction, and do not make database success depend on a live browser connection.
4. React receives an invalidation and refetches the authoritative HTTP endpoint. It must preserve forms, filters and focus when refreshing visible results.

**Failure behavior:** No business mutation or email notification may roll back just because an optional realtime consumer is disconnected. A rolled-back mutation must not generate a visible change notification. Duplicates are acceptable if clients deduplicate and refetch idempotently; global ordering is not promised. Session expiry and email work may occur in the separate worker process, so an API-process-only callback will not cover every producer. Before #87, select and document a transaction-safe publication path (e.g., a dedicated PostgreSQL event outbox with dispatcher); do **not** repurpose the existing email queue without an explicit schema/contract review. Cross-instance delivery (if multiple API instances exist) requires a fanout mechanism such as Redis pub/sub (#89); Redis pub/sub is not durable replay.

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

The current branch contains the React HTTP client, but this task adds no WebSocket client. For #90, place a singleton-per-auth-context socket manager under its existing shared infrastructure/provider code; component hooks subscribe to typed invalidation events, never instantiate uncoordinated independent sockets. Refetch with the chosen HTTP query/cache library already present then. State-changing API responses still determine whether a mutation succeeded; WebSocket messages must not substitute for an authoritative write response.

## Verification checklist

- [x] Contract fixtures/tests cover allowed types, malformed JSON, unknown version/type, duplicates and unauthorized subscriptions (#85).
- [x] Handshake rejects absent session, expired/revoked/suspended session, invalid Origin and untrusted scope (#86).
- [x] Normal REST requests retain timeouts while a persistent WebSocket survives the request timeout (#86).
- [x] Scoped gateway tests isolate users/coordinators/faculties and cover slow clients, heartbeat and shutdown (#86). Domain-specific recipient policies and production load remain #87–#89.
- [ ] After commit, every affected tab receives (or recovers) the new HTTP state; a rollback publishes nothing (#87/#88).
- [ ] Worker-originated expiration, account suspension cancellations and adjudication updates resynchronize (#87/#88).
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

Generate the ID **once per logical event**, persist it with the future outbox,
and retain it across recipients, dispatch retries and fanout. Never regenerate
an ID on each retry. Multiple invalidations from one transaction may have separate
IDs; timestamps do not establish a total order. The hub deliberately accepts
repeated event IDs and deduplicates candidate users within one publication only.
It supplies neither replay nor cross-publication deduplication.

Fixtures live in
`backend/internal/realtime/application/testdata/protocol.json`, with tests for
all six event types, repeated event IDs and invalid/unknown fields. Gateway tests
exercise explicit user/faculty subscription attempts; all client application
messages are rejected, including binary messages, empty messages and JSON
`subscribe` commands. Only standard WebSocket ping/pong/close control frames are
supported. No subscription registry exists in this stage.

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
grace and scheduling overhead, not instantaneous logout propagation. Each event
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
there is no faculty room or global broadcast API. No production caller publishes
in this stage, so the endpoint remains quiet apart from control frames.

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

## Required integration for #87

1. Depend on `internal/realtime/application.Publisher`, not the hub or HTTP
   handler. The implemented method is:
   `Publish(context.Context, Event, []string, Authorize) error`.
   `Authorize` takes `(context.Context, identity/domain.Principal, Event)` and
   returns `(bool, error)`. Bootstrap injects the concrete hub only into a
   dispatcher/delivery adapter. No business module imports platform transport.
2. Add a **dedicated transactional event outbox** via a new reversible migration.
   Persist a stable event UUID, type, minimal entity ID, occurrence time and the
   routing information needed to resolve current recipients, atomically with
   each business mutation/history/email write. Rollback leaves no event. Do not
   publish directly inside a transaction or reuse the email delivery queue.
3. Dispatch committed rows asynchronously. Resolve current student/tutor and
   authorized coordinator candidate IDs server-side through owning application
   ports. Supply a context-aware, concurrency-safe resource policy that binds the
   exact event type/entity to each fresh principal's authorized HTTP projection.
   Candidate IDs alone are insufficient; generic `return true` policies are for
   isolated transport tests only. Missing/deleted/forbidden resources must fail
   safely. Avoid retaining a database transaction inside an asynchronous closure.
4. Booking transitions must invalidate the participants' session views and the
   permitted tutor availability projection; public availability watchers must
   never receive booking identifiers. No public-watcher subscription protocol
   exists yet. Add server-authorized watch registration/resource validation before
   promising watcher delivery, or explicitly limit #87 to owning users and record
   the remaining discovery subscription work.
5. Cover request, accept, decline, cancel, expiry, completion, attendance
   correction, policy/suspension cancellation, recurrence/exception/slot changes
   and reservation release. Mutations in Coordination and the separate worker
   need the same transaction-bound event port, without bypassing module locks.
6. Select a worker-to-API path before enabling publication: a durable outbox plus
   transient cross-process fanout (for example Redis pub/sub) with **every API
   instance** receiving relevant committed events. One global queue consumer or
   an in-process hub alone cannot deliver to sockets hosted on other processes.
   Keep stable IDs through retries. Multi-instance transport wiring/hardening is
   coordinated with #89; never claim durability from pub/sub or delivery receipts
   from this hub. `Publish` returning nil means best-effort queueing only.
7. Recheck tests for rollback/no publication, retries/stable IDs, concurrent
   booking, expiration in the worker executable, released reservations, stale
   recipients/faculty changes and disconnected/slow clients. Database commits
   must succeed independently of live consumers. React/browser acceptance still
   requires #90–#94, including mandatory reconnect refetch.

## Verification and remaining review

Focused race tests cover strict Origin/handshake denial, versioned JSON fixtures,
recipient policy, multiple tabs, revoked/expired/suspended sessions, permission
changes, malformed/subscription input, limits, backpressure, heartbeat, concurrent
cleanup and bounded shutdown. Bootstrap tests prove REST 503 timeouts and sockets
surviving both HTTP IO deadlines and the actual ten-second REST duration.
`tests/integration/realtime_test.go` uses native sockets, real Redis sessions and
real migrated PostgreSQL accounts: cookie handling, coordinator faculty isolation,
logout, real Redis TTL expiration, suspension, faculty change and unavailable
Redis/PostgreSQL are covered. It is included by the existing disposable runner:

```sh
(cd backend && go test -race ./... && go vet ./... && go build ./...)
bash backend/tests/integration/test_identity.sh -race
```

Tests use synthetic, explicitly scoped events; there is no public test-publish
endpoint. Domain mutation publishers, transaction rollback/retry publication,
worker fanout, distributed delivery/load tests and React/browser journeys cannot
be claimed from this gateway's test results. The #85 human agreement criterion
still needs backend/frontend owner review. No GitHub issue is closed by these
changes.

Implementation file inventory for this foundation:

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
