# Handler stream 2 — huddle.post + huddle.read

**Wave context:** one of 3 parallel handler streams. Replaces 2 of the 6 no-op stubs from the foundation (commit `0157160`). The other 2 streams handle `create`+`close` and `who_else`+`list`.

> **Read first:** [`docs/design.md`](../design.md) — authoritative impl plan. Handler pseudocode for both verbs is in § "Handler-level pseudocode" (`huddle.post` and `huddle.read` sections).

## What's already in place

The foundation (merged at `0157160` on main) wired up the full backend:

- `internal/types/` — `PostArgs`, `PostResult`, `ReadArgs`, `Message`, `Identity`. **Use these directly; don't redefine.**
- `internal/store/` — `Store.LookupKey(key)`, `LookupHuddle(huddleID)`. Returns typed errors (`ErrKeyInvalid`, `ErrHuddleNotFound`).
- `internal/slack/` — `Adapter` with `PostMessage(ctx, channelID, text, threadTS)` + `History(ctx, channelID, since, limit)`. `encoding.Encode(identity, body)` renders the prefix; `encoding.Decode(text)` parses it back.
- `internal/server/server.go` — registers all 6 verbs as no-op stubs. This stream replaces 2 (`huddle.post`, `huddle.read`).
- `internal/errors/` — typed errors + `MCPError`. Import as `huddleerr "github.com/itsHabib/huddle/internal/errors"`.

## What this stream builds

### 1. `internal/handlers/post.go`

Implements `huddle.post` per design doc § `huddle.post`. The identity-resolution branch is the key shape:

1. Decode `types.PostArgs` — fields `Key` (optional), `HuddleID` (optional), `Body` (required), `ReplyTo` (optional).
2. **Identity resolution:**
   - If `args.Key != ""` (seat path):
     - `deps.Store.LookupKey(ctx, args.Key)` — error → `MCPError(InvalidParams, ErrKeyInvalid)`.
     - Identity = `types.Identity{Kind: "seat", SeatID: k.SeatID, DisplayName: k.DisplayName}`.
     - huddleID = `k.HuddleID`.
   - Else (orchestrator path):
     - **v0 trust model:** any key-less call is treated as orchestrator. (Will tighten later via `HUDDLE_ADMIN_TOKEN` env check.)
     - `args.HuddleID` is required here → `MCPError(InvalidParams, ...)` if empty.
     - `deps.Store.LookupHuddle(ctx, args.HuddleID)` — error → `MCPError(InvalidParams, ErrHuddleNotFound)`.
     - Identity = `types.Identity{Kind: "orchestrator", DisplayName: huddle.OrchestratorDisplayName}`.
3. Final huddle lookup + closed check: `deps.Store.LookupHuddle(ctx, huddleID)` — if `ClosedAt != nil` → `MCPError(InvalidParams, ErrHuddleClosed)`.
4. Render: `text := slack.Encode(identity, args.Body)` — produces `*[name] body` for orchestrator, `[name] body` for seat.
5. `ts, err := deps.Slack.PostMessage(ctx, huddle.SlackChannelID, text, args.ReplyTo)`. Wrap errors.
6. Return `types.PostResult{MessageID: ts, PostedAt: time.Now().UTC(), Identity: identity}`.

### 2. `internal/handlers/read.go`

Implements `huddle.read` per design doc § `huddle.read`:

1. Decode `types.ReadArgs` — `Key` (optional), `HuddleID` (optional), `Since` (optional), `Limit` (optional, default handled in adapter).
2. Resolve huddle: same key-or-huddleId pattern as `post`. If only `Key` given, look up huddle via key's `HuddleID`; if only `HuddleID` given (admin path), use it directly.
3. `msgs, err := deps.Slack.History(ctx, huddle.SlackChannelID, args.Since, args.Limit)` — adapter already clamps limit and decodes via `slack.Decode`.
4. Return the messages slice. Foundation's `History` already populates `types.Message{Identity, Body, ...}` so handler just passes through.

### 3. Wire into `internal/server/server.go`

Same pattern as stream 1: remove the two stub entries for `huddle.post` and `huddle.read` from the loop in `RegisterVerbStubs`, add:

```go
handlers.RegisterPost(s, deps)
handlers.RegisterRead(s, deps)
```

The other 4 stubs stay alone in this stream.

### 4. Unit tests

`internal/handlers/post_test.go`:
- Seat path happy: valid key + body → PostMessage called with `[<displayName>] body` text + correct channel + thread ts.
- Orchestrator path happy: huddleId + admin (no key) → PostMessage with `*[<orchestratorDisplayName>] body`.
- Revoked key → `MCPError(InvalidParams, ErrKeyInvalid)`.
- Closed huddle → `MCPError(InvalidParams, ErrHuddleClosed)`.
- Unknown huddleId (admin path) → `MCPError(InvalidParams, ErrHuddleNotFound)`.
- Slack post failure → `MCPError(InternalError, ...)`.

`internal/handlers/read_test.go`:
- Happy: seed FakeAdapter with a Slack history; read returns decoded messages with correct identities.
- Mix of seat / orchestrator / human / unknown identities in the history → all round-trip.
- System subtype messages (channel_join, etc.) filtered by adapter — handler sees only user messages.
- Slack history failure → `MCPError(InternalError, ...)`.

Use `slack.FakeAdapter` + `store.OpenMemory(ctx)`.

## Out of scope

- The other 4 handlers (other streams).
- E2E against real Slack.
- v1 verbs.
- Tightening orchestrator trust beyond "any key-less call".

## Definition of done

- `internal/handlers/post.go` and `read.go` implemented per design doc.
- `internal/server/server.go` updated to register them.
- Tests cover both happy + error paths plus all identity kinds for `read`.
- `make lint` clean.
- `make test` green.
- `make build` green.

## Conventions

Same as stream 1: lowercase errors, happy-path-not-indented, no `//nolint`, context-as-first-arg, `store.OpenMemory` is parallel-safe.
