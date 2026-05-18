# Handler stream 1 — huddle.create + huddle.close

**Wave context:** one of 3 parallel handler streams. Replaces 2 of the 6 no-op stubs from the foundation (commit `0157160`). The other 2 streams handle `post`+`read` and `who_else`+`list`.

> **Read first:** [`docs/design.md`](../design.md) — authoritative impl plan. Handler pseudocode for both verbs is in § "Handler-level pseudocode" (`huddle.create` and `huddle.close` sections).

## What's already in place

The foundation (merged at `0157160` on main) wired up the full backend:

- `internal/types/` — `CreateArgs`, `CreateResult`, `CreatedSeat`, `CloseArgs`, `CloseResult`, `Huddle`, `Identity`, etc. **Use these directly; don't redefine.**
- `internal/store/` — `Store.InsertHuddle`, `LookupHuddle`, `MarkClosed`, `InsertKey` etc. plus `OpenMemory(ctx)` for tests.
- `internal/slack/` — `Adapter` interface (`CreateChannel`, `ArchiveChannel`, ...), real impl, plus `FakeAdapter` in `_test.go` files for handler tests.
- `internal/server/server.go` — currently registers all 6 verbs as no-op stubs in a `for` loop (`RegisterVerbStubs`). This stream replaces 2 of those (`huddle.create`, `huddle.close`) with real registrations.
- `internal/config/` — `Config` with required `HUDDLE_SLACK_BOT_TOKEN` + state-dir / log-level / channel-prefix defaults.
- `internal/errors/` — `ErrKeyInvalid`, `ErrHuddleNotFound`, `ErrHuddleClosed`, `MCPError(code, err)`. Note: package `errors` (clashes with stdlib) — import as `huddleerr "github.com/itsHabib/huddle/internal/errors"`.

## What this stream builds

### 1. `internal/handlers/create.go`

Implements the real `huddle.create` handler per design doc § `huddle.create`:

1. Decode `types.CreateArgs` from request args (use `mcp.CallToolRequest.Params.Arguments` with `encoding/json`).
2. Generate `huddleID := "hud_" + uuid.New().String()`. Channel name = `slugifyChannel(args.Purpose, huddleID)` — sanitize purpose to lowercase ASCII + hyphens, append a short prefix of huddleID.
3. Call `deps.Slack.CreateChannel(ctx, channelName)` → get `slack.Channel{ID, Name}`. Wrap errors with context.
4. Persist via `deps.Store.InsertHuddle(ctx, store.Huddle{...})`. Set `OrchestratorDisplayName` from `args.Orchestrator.DisplayName` (default `"orchestrator"` if empty).
5. For each seat in `args.Seats`: generate a 16-byte random key via `crypto/rand`, base32-encode, prefix with `"K_" + huddleIDShort + "_" + seat.ID + "_"`. Persist via `deps.Store.InsertKey(ctx, store.Key{...})`.
6. Return `types.CreateResult{HuddleID, Channel, Orchestrator: {DisplayName}, Seats: [{ID, Key, DisplayName}]}`.

Error mapping:
- Invalid args (missing purpose, empty seats, bad seat IDs) → `huddleerr.MCPError(InvalidParams, ...)`.
- Slack channel-name collision after retry → `InternalError` with detail.
- Storage failure → `InternalError`.

### 2. `internal/handlers/close.go`

Implements the real `huddle.close` handler per design doc § `huddle.close`:

1. Decode `types.CloseArgs` (just `HuddleID`).
2. `deps.Store.LookupHuddle(ctx, huddleID)` → if `huddleerr.ErrHuddleNotFound`, return `MCPError(InvalidParams, ErrHuddleNotFound)`.
3. If `huddle.ClosedAt != nil` → **idempotent**: return `CloseResult{Closed: true, ArchivedChannel: huddle.SlackChannelName}` without re-calling Slack.
4. Otherwise: `deps.Slack.ArchiveChannel(ctx, huddle.SlackChannelID)`. Wrap errors.
5. `deps.Store.MarkClosed(ctx, huddle.ID, time.Now().UTC())`. Wrap errors.
6. Return `CloseResult{Closed: true, ArchivedChannel: huddle.SlackChannelName}`.

### 3. Wire into `internal/server/server.go`

The foundation registered all 6 verbs as no-op stubs in `RegisterVerbStubs`. **Remove the two entries for `huddle.create` and `huddle.close`** from that registration. Add explicit registrations for the real handlers:

```go
handlers.RegisterCreate(s, deps)
handlers.RegisterClose(s, deps)
```

(`RegisterCreate` / `RegisterClose` live in `internal/handlers/` and call `mcp.AddTool` themselves.)

The other 4 stubs (`huddle.list`, `huddle.post`, `huddle.read`, `huddle.who_else`) **stay as stubs in this stream** — they get replaced by the other two streams.

### 4. Unit tests

`internal/handlers/create_test.go`:
- Happy path: 2-seat huddle, verify channel created, huddle row persisted, key rows for both seats with unique key values, response shape correct.
- Slack `name_taken` → adapter retries, success on second attempt.
- Storage failure → maps to MCP error.
- Empty seats → InvalidParams.

`internal/handlers/close_test.go`:
- Happy path: open huddle → archive → store stamped → response.
- Already-closed huddle → idempotent, no Slack call.
- Unknown huddleID → InvalidParams.
- Slack archive failure → InternalError.

Use `slack.FakeAdapter` (already exists under `_test.go` in `internal/slack/`) + `store.OpenMemory(ctx)`. The FakeAdapter records calls for assertion.

## Out of scope

- The other 4 handlers (other streams will own them).
- E2E test against real Slack.
- v1 verbs (`add_seat`, `revoke_key`, `watch`).

## Definition of done

- `internal/handlers/create.go` and `close.go` implemented per design doc.
- `internal/server/server.go` updated to register them instead of stubs (other 4 stubs untouched).
- Tests for both happy + error paths.
- `make lint` clean (strict `golangci-lint v2` per `.golangci.yml`).
- `make test` green.
- `make build` produces a working binary.

## Conventions (read once)

- **Errors not capitalized.** `huddleerr.MCPError(...)` returns `*jsonrpc.Error`; build messages with lowercase first letter.
- **Happy path not indented.** `revive indent-error-flow` is on.
- **Don't `//nolint`.** Refactor when a linter fires.
- **Context as first arg** to package-exported I/O funcs.
- **Test isolation.** `store.OpenMemory` uniquifies its DSN — safe to call from `t.Parallel()` tests.

## After this lands

Other two handler streams will rebase onto your changes. Server.go conflicts on the stub-removal loop are expected and resolvable. Foundation owner (operator) handles merge sequencing per the parallel-driver "merge in dep order" pattern.
