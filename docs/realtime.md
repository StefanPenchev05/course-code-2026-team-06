# Real-time updates and WebSockets (design baseline)

> **Status:** planned contract for #85; not an assertion that WebSockets are already implemented. Last reviewed against `main` on 2026-11-23. Implementation belongs on branch `feat/realtime-websocket-foundation`. Confirm the checked-out source before relying on paths or behavior.
>
> **Tracking:** #84; backend #85–#89; frontend #90–#94. Relevant application modules: Identity, Scheduling, Tutoring, Coordination and Feedback.

## Goal and scope

Tutti's existing REST/HTTP API and PostgreSQL database remain authoritative. An authenticated WebSocket channel sends **small change notifications** after committed writes so React can invalidate and refetch its active data instead of reloading the page. Redis-backed sessions are reused for authentication; sockets do not replace the email notification outbox, workers, domain authorization or database conflict protection.

**Recommended implementation order:** #85 protocol and tests → #86 authenticated Go gateway → #87 booking/availability publication → #90 shared React client → #91/#92 live booking and discovery → #88 coordinator/tutor/feedback publication → #93 UI → #89/#94 delivery and browser integration hardening. Add meaningful tests within *each* issue, not only at the end.

The repository currently has only `frontend/.gitkeep` on main. Frontend issue owners must inspect the tree and coordinate a React foundation rather than assuming `frontend/src` exists. The Go backend and worker are separate executables; data changed by the worker must become visible to API-hosted WebSocket clients.

## Protocol proposal (version 1; refine and agree in #85)

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

The example IDs are illustrative; do not infer their existence in the database. Event IDs enable local deduplication, not guaranteed persistent replay. On a supported version with an unknown type, ignore safely and trigger a conservative visible-query refresh where needed; malformed/unsupported-version messages must not crash the browser. Formalize limits, error strategy, and stable event ID generation in #85.

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

## Backend integration notes (inspect before #86)

- Current setup: `backend/internal/bootstrap/identity.go` creates `http.NewServeMux()`, registers Identity and the domain HTTP routes, and wraps **the entire mux in `http.TimeoutHandler(mux, 10*time.Second, ...)`**. A WebSocket handshake/hijacked connection must **not** pass through this timeout wrapper. Keep timeouts for normal HTTP requests via a separate outer router/handler arrangement; do not globally remove request protections.
- `identity/interfaces/http/middleware.go` supplies `RequireAuthentication`, `RequireSameOrigin`, and `CurrentPrincipal`. Reuse existing authenticated identity/session checks. `RequireSameOrigin` currently protects browser writes and must not be blindly applied to `GET` paths without confirming WebSocket handshake semantics.
- Use `PUBLIC_ORIGIN` as the existing trusted-browser-origin baseline, and check proxies/TLS carefully. WebSocket upgrades require suitable reverse-proxy `Upgrade`/connection handling and long-lived read/write limits independently of normal REST requests.
- Use bounded queues and read/write deadlines, ping/pong or equivalent liveness checks, per-user connection limits, shutdown cleanup and privacy-safe structured logs. Do not allow client-provided room identifiers to bypass faculty or ownership authorization.
- Keep the hub a **transport adapter**, not a new domain aggregate; module use cases publish via narrow application ports, and `bootstrap` wires concrete adapters. No new microservice is required.

## Frontend integration notes (for #90 onward)

The React client is planned but not yet implemented in the published tree inspected for this document. When available, place a singleton-per-auth-context socket manager under shared infrastructure/provider code; component hooks subscribe to typed invalidation events, never instantiate uncoordinated independent sockets. Refetch with the chosen HTTP query/cache library already present then. State-changing API responses still determine whether a mutation succeeded; WebSocket messages must not substitute for an authoritative write response.

## Verification checklist

- [ ] Contract fixtures cover allowed types, malformed JSON, unknown version/type, duplicates and unauthorized subscriptions (#85).
- [ ] Handshake rejects absent session, expired/revoked/suspended session, invalid Origin and untrusted scope (#86).
- [ ] Normal REST requests retain timeouts while a persistent WebSocket survives the request timeout (#86).
- [ ] No event leaks across session participants, coordinators or faculties; slow clients and shutdown free resources (#86/#89).
- [ ] After commit, every affected tab receives (or recovers) the new HTTP state; a rollback publishes nothing (#87/#88).
- [ ] Worker-originated expiration, account suspension cancellations and adjudication updates resynchronize (#87/#88).
- [ ] Browser e2e uses independent student/tutor/coordinator contexts; no reload is needed; reconnect resync works (#90–#94).
- [ ] The relevant Go race/integration and frontend tests run in CI; document any environment limitations.

## Out of scope for foundation (#85–#86)

Do not implement chat, presence, typing indicators, websocket-based writes, public raw-event broadcasting, separate user auth, an unrelated queue system or a React scaffold during the Go gateway task. Do not claim the frontend is complete merely because the backend gateway works.
