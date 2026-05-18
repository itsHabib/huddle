# Handler stream 3 — huddle.who_else + huddle.list

**Wave context:** one of 3 parallel handler streams. Replaces 2 of the 6 no-op stubs from the foundation (commit `0157160`). The other 2 streams handle `create`+`close` and `post`+`read`. **This is the easiest stream** — both handlers are pure storage reads with no Slack API calls.

> **Read first:** [`docs/design.md`](../design.md) — authoritative impl plan. Handler pseudocode for both verbs is in § "Handler-level pseudocode" (`huddle.who_else` and `huddle.list` sections).

## What's already in place

The foundation (merged at `0157160` on main) wired up the full backend:

- `internal/types/` — `WhoElseArgs`, `WhoElseResult`, `ListArgs`, `Huddle`, `Seat`. **Use these directly.**
- `internal/store/` — `Store.LookupKey(key)`, `LookupHuddle(huddleID)`, `ListSeats(ctx, huddleID)` (active keys only, returns `[]types.Seat`), `ListHuddles(ctx, activeOnly bool)`.
- `internal/server/server.go` — 6 no-op stubs; this stream replaces 2.
- `internal/errors/` — typed errors + `MCPError`. Import as `huddleerr`.

## What this stream builds

### 1. `internal/handlers/who_else.go`

Implements `huddle.who_else` per design doc § `huddle.who_else`:

1. Decode `types.WhoElseArgs` — just `Key` (required).
2. `k, err := deps.Store.LookupKey(ctx, args.Key)` — error → `MCPError(InvalidParams, ErrKeyInvalid)`.
3. `huddle, err := deps.Store.LookupHuddle(ctx, k.HuddleID)` — error → `MCPError(InternalError, ...)` (shouldn't happen — key references valid huddle by FK).
4. `seats, err := deps.Store.ListSeats(ctx, k.HuddleID)` — returns `[]types.Seat` (only active keys). Wrap on error.
5. Return `types.WhoElseResult{Purpose: huddle.Purpose, Orchestrator: {DisplayName: huddle.OrchestratorDisplayName}, Seats: seats}`.

Note: `seats` includes the caller's own seat. That's intentional — agents see themselves listed alongside peers.

### 2. `internal/handlers/list.go`

Implements `huddle.list` per design doc § `huddle.list`. This is an **operator-only verb** — no `Key` parameter:

1. Decode `types.ListArgs` — just `Active bool` (optional, default false = all huddles).
2. `huddles, err := deps.Store.ListHuddles(ctx, args.Active)` — returns `[]types.Huddle`. Wrap on error.
3. Return `huddles` directly. **Key material is never in `types.Huddle`** — `ListHuddles` only returns the huddle metadata (purpose, orchestrator, channel, timestamps).

### 3. Wire into `internal/server/server.go`

Same pattern as the other streams. Remove `huddle.who_else` and `huddle.list` from the stub loop, add:

```go
handlers.RegisterWhoElse(s, deps)
handlers.RegisterList(s, deps)
```

The other 4 stubs stay alone in this stream.

### 4. Unit tests

`internal/handlers/who_else_test.go`:
- Happy path: seed huddle with 3 seats, call with seat 1's key, response has `Purpose`, `Orchestrator.DisplayName`, and all 3 seats (including seat 1).
- Revoked key → `MCPError(InvalidParams, ErrKeyInvalid)`.
- Unknown key → same.
- Revoked seat absent from `Seats` list — verify only active keys round-trip.

`internal/handlers/list_test.go`:
- Empty store → empty slice.
- 3 huddles, 1 closed → `Active: false` returns all 3, `Active: true` returns 2 open.
- Order: most-recent-created first (or whatever `ListHuddles` already does; assert it).
- No key material leaks in response (`types.Huddle` doesn't expose keys, but assert the response shape directly).

Use `store.OpenMemory(ctx)` only — neither handler touches Slack. FakeAdapter isn't needed.

## Out of scope

- The other 4 handlers (other streams).
- E2E against real Slack.
- v1 verbs.
- Filtering `Seats` to exclude the caller (callers see themselves).

## Definition of done

- `internal/handlers/who_else.go` and `list.go` implemented per design doc.
- `internal/server/server.go` updated to register them.
- Tests cover happy + error paths, active/inactive filter, revoked-key edge cases.
- `make lint` clean.
- `make test` green.
- `make build` green.

## Conventions

Same as the other streams: lowercase errors, happy-path-not-indented, no `//nolint`, context-as-first-arg, `store.OpenMemory` is parallel-safe.

This is the smallest of the three streams (no Slack, fewer error paths) — should be the lowest-risk for the Cursor SDK opaque-error pattern hit during the foundation.
