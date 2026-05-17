# Foundation stream — complete the remaining layers

**Continuation of** [`docs/tasks/foundation.md`](foundation.md). The original foundation Ship run (`wf_01KRVTX843C5K823QQPH12T4YH`) failed with an opaque "Cursor SDK reported error without a message" ~2 min in. About 1/3 landed and was hand-cleaned + committed at `ed88f91`. This task picks up where that left off.

> **Read first:** [`docs/design.md`](../design.md) — authoritative impl plan.

## What's already in place (DO NOT rebuild)

Already on `tower/foundation` branch (commit `ed88f91`):

- `go.mod` / `go.sum` — all needed deps are fetched as `// indirect` (because nothing imports them yet beyond the linter tool). `go mod tidy` after adding imports will promote them to direct.
- `internal/types/types.go` — Identity (discriminated union: seat / orchestrator / human / unknown), Message, Seat, Huddle, all verb arg/result types. **Use these.**
- `internal/config/config.go` — `Config`, `ValidationError`, `Load()`. Loads `HUDDLE_SLACK_BOT_TOKEN` (required), `HUDDLE_STATE_DIR`, `HUDDLE_LOG_LEVEL`, `HUDDLE_CHANNEL_PREFIX`, `HUDDLE_SLACK_WORKSPACE`.
- `internal/errors/errors.go` — typed errors (`ErrKeyInvalid`, `ErrHuddleNotFound`, `ErrHuddleClosed`, `ErrSlackRateLimited`, `ErrSlackMissingScope`, `ErrStorageFailure`) + `MCPError(code int64, err error) *jsonrpc.Error` helper. **Package name is `errors` (clashes with stdlib — callers will need an alias like `huddleerr "github.com/itsHabib/huddle/internal/errors"`).**

`make lint` is currently clean. Keep it that way.

## What this stream builds

The remaining three layers + main wire-up. Build them in this order:

### 1. Storage layer (`internal/store/`)

- `schema.sql` — `//go:embed`-ed in `db.go`. Schema per [design doc § Storage schema](../design.md#storage-schema-sqlite): `huddles` + `keys` tables with indices on `closed_at` and active keys, `ON DELETE CASCADE` from keys → huddles.
- `db.go` — `*Store` wrapper around `*sql.DB`, connection construction using `modernc.org/sqlite` driver, schema apply on startup (idempotent via `CREATE ... IF NOT EXISTS`).
- `huddles.go` — `InsertHuddle`, `LookupHuddle`, `ListHuddles(activeOnly bool)`, `MarkClosed(id string, t time.Time)`. Use `types.Huddle`.
- `keys.go` — `InsertKey`, `LookupKey(key) (Key, error)` returning `ErrKeyInvalid` (from `internal/errors`) if revoked or missing, `ListSeats(huddleId) ([]types.Seat, error)` (active keys only), `RevokeKey(key string, t time.Time)`.
- Unit tests against in-memory SQLite (`file::memory:?cache=shared`). Round-trip every CRUD op. Verify schema idempotency.

### 2. Slack encoding (`internal/slack/encoding.go`)

Pure functions, no external deps:

```go
func Encode(identity types.Identity, body string) string
func Decode(text string) (types.Identity, body string)
```

Prefix rules per [design doc § Slack message encoding](../design.md#slack-message-encoding):

- `*[<displayName>] <body>` → `Kind: "orchestrator"`
- `[<displayName>] <body>` → `Kind: "seat"` (encoding layer does NOT resolve `SeatID`; that's a handler concern via the keys table)
- Anything else not matching either → `Kind: "human"` if it looks like plain text; `Kind: "unknown"` only when explicitly malformed (e.g. `[unclosed` opener with no closing `]`)

Round-trip unit tests for every kind + malformed + edge cases (empty body, leading/trailing whitespace, brackets in body text).

### 3. Slack adapter (`internal/slack/`)

Interface seam for testability:

- `iface.go` — `Adapter` interface:
  ```go
  type Adapter interface {
      CreateChannel(ctx context.Context, name string) (Channel, error)
      ArchiveChannel(ctx context.Context, channelID string) error
      PostMessage(ctx context.Context, channelID, text, threadTS string) (ts string, err error)
      History(ctx context.Context, channelID string, since *time.Time, limit int) ([]types.Message, error)
  }

  type Channel struct {
      ID   string
      Name string
  }
  ```
- `client.go` — real impl wrapping `*slack.Client` from `github.com/slack-go/slack`. Constructor takes a token (`config.Config.SlackBotToken`). Set retry on 429 via SDK options.
- `channels.go` — create + archive. On `name_taken`, append a short random suffix and retry once.
- `messages.go` — post (call `encoding.Encode` to render text) + history (call `encoding.Decode` per message, skip system messages with non-empty `subtype`).
- `fake.go` in `_test.go` files only — call-recording `FakeAdapter` returning canned responses. Lives in test files so it never ships in the binary.

### 4. MCP server skeleton (`internal/server/server.go` + `cmd/huddle/main.go`)

- `internal/server/server.go` — uses `github.com/modelcontextprotocol/go-sdk/mcp`. Register all 6 v0 verbs as **no-op handlers** returning a stub:
  ```go
  return mcp.NewToolResult(map[string]any{"ok": true, "verb": "<name>"})
  ```
  Verbs: `huddle.create`, `huddle.close`, `huddle.list`, `huddle.post`, `huddle.read`, `huddle.who_else`.
  Handler logic ships in the next wave of streams; this stream only proves the wiring.
- `cmd/huddle/main.go` — replace the version-print stub:
  - Call `config.Load()` (handle ValidationError with clear stderr message + exit code 2).
  - Open the store (`store.New(cfg.StateDir)`).
  - Construct Slack adapter (`slack.NewAdapter(cfg)`).
  - Wire `Deps{Slack, Store, Cfg, Log}` (define `Deps` either in `internal/server/` or `cmd/huddle/main.go`).
  - Construct the server, register the 6 no-op handlers.
  - Start stdio loop.
  - Handle SIGINT / SIGTERM gracefully (close DB, flush logs).

### 5. Tooling final pass

After all the code is in:

- `go mod tidy` — promote slack-go, modernc, modelcontextprotocol/go-sdk, etc. to direct deps.
- `make lint` clean
- `make test` green
- `make build` produces a working binary
- Print version on `./huddle --version` (optional v0.0.1 polish, not strictly required)

## Out of scope

- Handler logic (next wave: 3 parallel streams for create+close, post+read, who_else+list)
- E2E test against real Slack (after handlers)
- Dogfood session (manual, post-merge)
- `add_seat` / `revoke_key` / `watch` verbs (v1)
- HTTP/SSE transport (v1)
- Block Kit rendering (v1 cosmetic)

## Definition of done

- All three remaining layers (`internal/store/`, `internal/slack/`, `internal/server/`) implemented with unit tests living alongside the code.
- `cmd/huddle/main.go` wires everything end-to-end (Config → Store → Slack adapter → server → stdio).
- `make lint` clean against the strict `golangci-lint v2` config in `.golangci.yml`.
- `make test` green.
- `make build` produces a working binary.
- Coverage ≥ 80% on `internal/store/` and `internal/slack/encoding.go`. Handler coverage is exempt (they're no-ops in this stream).

## Conventions to follow

- **Strict lint from every commit.** No `//nolint` without justification — refactor instead.
- **Errors aren't capitalized** (`staticcheck ST1005`).
- **Happy path not indented** (`revive indent-error-flow`).
- **Context as first arg** to all package-exported functions doing I/O.
- **Atomic file ops** if writing on-disk state outside SQLite (tower's `internal/fsutil` is the pattern).
- **No CGO.** `modernc.org/sqlite` is pure Go; `CGO_ENABLED=0 go build` must produce a static binary.
- **Avoid `package errors` clash** in consumers: import as `huddleerr "github.com/itsHabib/huddle/internal/errors"` or similar alias.

## After this lands

Operator opens a PR for `tower/foundation` → `main`, requests reviewers (Copilot via REST + @codex / @claude comments). After merge, the three handler streams kick off in parallel against fresh worktrees off main.
