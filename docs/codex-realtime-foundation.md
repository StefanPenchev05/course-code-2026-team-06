# Codex task: start Tutti real-time foundation (#85 then #86)

Repository: `StefanPenchev05/course-code-2026-team-06`  
Branch: `feat/realtime-websocket-foundation`  
Primary issue: [#86 — authenticated WebSocket gateway](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/86)  
Prerequisite: [#85 — event contract](https://github.com/StefanPenchev05/course-code-2026-team-06/issues/85)  
Planning document: [docs/realtime.md](realtime.md)

## Role

Implement a **small, production-conscious Go WebSocket foundation** in the existing modular monolith. Do not attempt all realtime issues in a single patch. Follow the root `AGENTS.md` and any nested agent instructions. Do not assume files or branches have been locally checked out; check working tree first.

## First actions

1. Run `git status --short --branch`, `git log -5 --oneline`, and find `AGENTS.md`. Checkout the target branch **only if** safe and the working tree is clean or unrelated work is preserved. Do not overwrite uncommitted work.
2. Inspect the current `backend/internal/bootstrap/identity.go`, Identity auth middleware/session service, `backend/cmd/api`, `backend/internal/platform`, current tests, `backend/go.mod`, `.env.example`, and `docs/realtime.md`. Confirm code matches the design; report divergences.
3. Finish/validate the #85 contract: keep the initial envelope versioned, minimal, and privacy safe. Add Go event schema/JSON validation tests or fixtures, update `docs/realtime.md` if the implementation design changes. Do not close #85 without agreed acceptance criteria.
4. Implement **only #86**: same-origin `GET /ws` upgrade with verified existing Redis session and *server-side* recipient authority; a bounded, concurrency-safe in-memory hub that supports user-scoped test events; liveness, connection cleanup, shutdown and tests.
5. **Crucial:** `bootstrap/identity.go` currently wraps the full mux with `http.TimeoutHandler(..., 10s, ...)`. Refactor handler composition so `/ws` bypasses the timeout wrapper but **all existing REST routes preserve their timeout behavior**. Add a test for both.
6. Keep authentication checks before upgrade, require exact trusted Origin, and avoid a second auth/session implementation. Unauthorized, expired, revoked or suspended sessions must fail safely. Revalidate long-lived sessions/permissions on an explicit documented schedule or other robust server-authoritative mechanism. Do not trust subscribe messages with arbitrary user/faculty IDs.
7. Choose a maintained Go WebSocket library compatible with the project's Go version only if necessary; justify and pin any new dependency. Keep domain modules independent from the hub implementation; use interfaces and bootstrap wiring. No Socket.IO server.
8. Add focused Go unit/integration tests for handshake auth/Origin, disconnect, slow client, race safety, duplicate browser sessions, session loss, isolation, and shutdown. Use disposable PostgreSQL/Redis for integration tests.
9. Update `docs/realtime.md`, `AGENTS.md` (only if instructions actually change), and `.env.example` (only for new configuration). Make the next stage #87 unambiguous.

## Boundaries

- **Do not** implement booking/availability event emission yet (#87), tutor/coordinator publishers (#88), React code (#90–#94), chat, presence, broad topics, new auth, microservices, or changes to core booking constraints.
- **Do not** put session tokens, private grades, student numbers, faculty evidence, personal messages or secrets into WebSocket payloads or logs.
- **Do not** publish before transaction commit or claim that in-process hub events cover the separate worker. For #87, plan a dedicated transaction-safe publisher/outbox and multi-process delivery strategy.
- Authenticated socket connection does not grant authority to arbitrary event categories. Use explicit server-owned recipient selection. For now demonstrate only isolated user-scoped test events if deeper domain publications require #87.
- Preserve the `main` branch. Do not commit/push/merge/close issues unless directly authorized by the human.

## Definition of done

- `GET /ws` upgrades for valid authenticated same-origin browser sessions and refuses invalid sessions/origins.
- Existing REST endpoints and response/timeouts remain unchanged.
- Bounded hub tracks multiple connections, cleans up correctly and stops when the API shuts down.
- Versioned event envelope is documented, tested and does not leak private data.
- Run `gofmt`, `go test -race ./...`, `go vet ./...`, `go build ./...` from `backend/`, plus suitable disposable-service integration checks. If a check cannot run, say exactly why.
- Provide a final report with files modified, contract decisions, tests actually run/results, remaining limitations, and what #87 must implement next.

## Suggested handoff

```markdown
### Issues and branch
#85 contract prerequisite; #86 gateway; feat/realtime-websocket-foundation

### Inspected
[files, auth rules, existing tests]

### Changes
[hub, route, tests, docs, dependencies]

### Verified
[commands and results; limits]

### Next: #87
[exact publish port / post-commit integration and worker fanout gaps]
```
