# Foundation stream — storage + Slack encoding + adapter + MCP server skeleton

This is the **first Ship stream** for huddle. It builds the four non-handler layers per [`docs/design.md`](../design.md) build-sequencing steps 2–5. Handler logic ships in the next wave (3 parallel streams) after this lands and is reviewed.

> **Read first:** [`docs/design.md`](../design.md) — the authoritative implementation plan. This task doc is the cut for *this stream only*; the design doc has the rationale, dependency table, error-handling philosophy, etc.

## What this stream builds

### 1. Storage layer (`internal/store/`)

- `schema.sql` — embedded via `//go:embed` in `db.go`. Schema is in design doc § Storage schema (huddles + keys tables, indices on `closed_at` and active keys, `ON DELETE CASCADE` from keys → huddles).
- `db.go` — connection, schema apply on startup, `modernc.org/sqlite` driver registration.
- `huddles.go` — `InsertHuddle`, `LookupHuddle`, `ListHuddles(activeOnly)`, `MarkClosed`.
- `keys.go` — `InsertKey`, `LookupKey` (returns error if `revoked_at` set), `ListSeats(huddleId)` (active only), `RevokeKey`.
- Unit tests against in-memory SQLite (`file::memory:?cache=shared`). Round-trip every operation. Idempotent schema apply.

### 2. Slack encoding (`internal/slack/encoding.go`)

Pure functions:

```go
func Encode(identity Identity, body string) string
func Decode(text string) (identity Identity, body string)
```

Identity is a discriminated union:

```go
type Identity struct {
    Kind        string // "seat" | "orchestrator" | "human" | "unknown"
    DisplayName string
    SeatID      string // only set when Kind == "seat"
}
```

Prefix rules per design doc § Slack message encoding:
- `*[name] body` → `Kind: "orchestrator"`
- `[name] body` → `Kind: "seat"` (resolve `SeatID` from displayName via the keys table later; encoding layer doesn't do that lookup)
- Anything else → `Kind: "human"` if it looks like normal text, `Kind: "unknown"` if `[unclosed` or otherwise malformed

Round-trip unit tests for every kind + malformed + edge cases (empty body, leading/trailing whitespace, brackets in body).

### 3. Slack adapter (`internal/slack/`)

Interface separation so handlers can test against a fake.

- `iface.go` — `Adapter` interface:
  ```go
  type Adapter interface {
      CreateChannel(ctx context.Context, name string) (Channel, error)
      ArchiveChannel(ctx context.Context, channelID string) error
      PostMessage(ctx context.Context, channelID, text, threadTS string) (ts string, err error)
      History(ctx context.Context, channelID string, since *time.Time, limit int) ([]Message, error)
  }
  ```
- `client.go` — real impl wrapping `*slack.Client` from `github.com/slack-go/slack`. Token from `config.Config`. Use the SDK's built-in retry on 429 (configure via `slack.OptionHTTPClient` or equivalent).
- `channels.go` — create + archive. Channel-name uniqueness on Slack: if Slack errors `name_taken`, append a short random suffix and retry once.
- `messages.go` — post (call `encoding.Encode` to render) + history (call `encoding.Decode` per message). Skip Slack system messages (`subtype` set: `channel_join`, `channel_create`, etc.).
- `fake.go` in `_test.go` files only — call-recording `FakeAdapter` returning canned responses. Lives in test files so it never ships in the binary.

### 4. MCP server skeleton (`internal/server/server.go` + `cmd/huddle/main.go`)

- Use `github.com/modelcontextprotocol/go-sdk/mcp`. Register **all 6 v0 verbs** as no-op handlers returning a stub `map[string]any{"ok": true, "verb": "<name>"}`. Actual logic ships in the handler streams that follow.
- Stdio transport for v0 (HTTP later when ship-going-remote arrives).
- `cmd/huddle/main.go` — replace the version-print stub:
  - Load `config.Config` from env (handle missing required vars with a clear error).
  - Open storage (`store.NewStore(cfg.StateDir)`).
  - Construct Slack client (`slack.NewClient(cfg.SlackBotToken)`).
  - Wire `Deps{Slack, Store, Cfg, Log}`.
  - Construct server, register the 6 handlers.
  - Start stdio loop.
  - Handle SIGINT/SIGTERM gracefully (close DB, flush logs).

### 5. Config + errors + types (`internal/{config,errors,types}/`)

- `config/config.go` — env loading per design doc § Configuration. Required: `HUDDLE_SLACK_BOT_TOKEN`. Optional with defaults: `HUDDLE_STATE_DIR` (default `.huddle`), `HUDDLE_LOG_LEVEL` (default `info`), `HUDDLE_CHANNEL_PREFIX` (default `huddle-`). Validation returns a typed error; main exits with a clear message on missing required vars.
- `errors/errors.go` — typed errors:
  - `ErrKeyInvalid`, `ErrHuddleNotFound`, `ErrHuddleClosed`, `ErrSlackRateLimited`, `ErrSlackMissingScope`, `ErrStorageFailure`.
  - Helper `MCPError(code, err) *mcp.CallToolResult` mapping to `-32602` / `-32603` per design doc § Error handling.
- `types/types.go` — shared exported types: `Identity`, `Message`, `Seat`, `Huddle`, plus the verb arg/result shapes. Anything used across packages.

### 6. Tooling

- `go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest` — install lint as a Go tool dep so `make lint` works (today's bootstrap doesn't have this yet; only `.golangci.yml` is in place).
- `go mod tidy` after adding deps.
- Verify the full local toolchain: `make lint` clean, `make test` green, `make build` produces a working binary, `make vet` clean.

## Out of scope

Don't ship in this stream:

- Handler logic (steps 6–9 in design doc; that's the next wave of 3 parallel streams).
- E2E test against real Slack (step 10; after handlers).
- Dogfood session (step 11; manual, post-merge).
- `add_seat` / `revoke_key` / `watch` verbs (v1).
- Block Kit rendering (v1 cosmetic upgrade).
- HTTP / SSE transport (v1, blocks on ship-going-remote).
- Per-bot-per-seat attribution (v2).

## Definition of done

- All four layers implemented with unit tests living alongside the code.
- `make lint` clean against the strict `golangci-lint v2` config in `.golangci.yml` (no `//nolint` without justification — refactor instead, per the convention shared with tower/orchestra/sense).
- `make test` green (Go `testing` + `testify/require`).
- `make build` produces a working binary.
- Coverage ≥ 80% on `internal/store/` and `internal/slack/encoding.go`. Handlers are no-ops in this stream, so handler coverage is exempt until the next wave.
- PR opened against `main`. Reviewers requested per the workbench convention:
  - Copilot via REST: `gh api -X POST repos/itsHabib/huddle/pulls/<n>/requested_reviewers -f 'reviewers[]=Copilot'`
  - `gh pr comment <n> -b "@codex review"`
  - `gh pr comment <n> -b "@claude review"`

## Conventions to follow

- **Strict lint from day one.** Don't disable linters; refactor when they fire. Same Dave-Cheney bar tower / orchestra / sense hold themselves to.
- **Errors aren't capitalized** (Go convention; `staticcheck ST1005`).
- **Happy path not indented** (`revive indent-error-flow`).
- **Context as first arg** to all package-exported functions that do I/O.
- **Atomic file ops** for any on-disk state (write `.tmp` → `os.Rename`). Tower's `internal/fsutil` is the pattern.
- **No CGO.** `modernc.org/sqlite` is pure Go specifically so `CGO_ENABLED=0 go build` produces a static binary — keep that guarantee.

## Followups (for the operator to track, not for this stream)

After this stream lands:
- Three handler streams in parallel: `handlers-create-close`, `handlers-post-read`, `handlers-who-else-list`.
- E2E test stream (or inline in the last handler stream).
- Dogfood session (manual): operator creates a huddle, two Claude Code sessions join with their keys, validates the day-to-day UX.
- Friction-log entries to `pers/parallel-driver.md` for anything surprising during this stream.
