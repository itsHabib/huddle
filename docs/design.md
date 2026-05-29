# huddle — design doc

Status: shipped v0 — 2026-05-17 design → 2026-05-18 last-major-change. Tracks v1 from here.
Owner: itsHabib
Catalog entry: [`pers/mcp-workstation/huddle.md`](../../mcp-workstation/huddle.md) — the "what." This doc is the "how."

## What this doc is

The catalog entry covers the **what**: problem, MCP surface, identity model, Slack-as-backend story, risks, POC scope. This doc covers the **how**: stack choice, file layout, dependencies, verb-handler-level pseudocode, Slack encoding format, storage schema, test plan, build sequencing, definition of done.

If the catalog entry and this doc disagree, the catalog entry wins on intent; this doc wins on implementation.

## Stack

**Go**, single module, layered as `cmd/` + `internal/` (matches tower + sense).

Rationale:
- Primary language for the operator; maintenance velocity highest.
- Matches sense as the reference scaffold (external-API-wrapper shape: `client.go` / `messages.go` / `cache.go` / etc.).
- Matches tower as the deployment reference (single static binary, `Taskfile.yml` for dev tasks, `cmd/<name>/main.go` entry).
- Single-binary distribution is straightforward — relevant when ship-going-remote lands and huddle needs to deploy as a service rather than a local stdio process.
- Official MCP Go SDK exists and is production-ready: `github.com/modelcontextprotocol/go-sdk` v1.6.0 (May 2026), maintained by the modelcontextprotocol org in collaboration with Google. Stdio transport supported out of the box.
- `slack-go/slack` is the well-trodden Slack client.

Trade-offs accepted: ~20% more code than TS (explicit `if err != nil`, small handwritten validators per verb). Worth it for the language fit.

## Repo layout

Standard Go cmd + internal layout. Tests live alongside the code they test (Go convention); e2e in a separate package.

```
pers/huddle/
├── README.md                    # what this is, link to design + catalog
├── CLAUDE.md                    # project-specific agent guidance
├── LICENSE
├── go.mod
├── go.sum
├── Makefile                     # dev tasks (mirrors ship + dossier)
├── .golangci.yml                # strict lint (mirrors tower + orchestra + sense)
├── .gitignore
├── docs/
│   └── design.md                # this file
├── cmd/
│   ├── huddle/
│   │   └── main.go              # MCP server entry; wires handlers, starts stdio loop
│   └── smoke/
│       └── main.go              # MCP-client e2e harness driving the huddle binary as a subprocess (gated by HUDDLE_SLACK_BOT_TOKEN; manual runs, not CI)
├── internal/
│   ├── server/                  # MCP server setup (modelcontextprotocol/go-sdk) + tool registration
│   │   └── server.go
│   ├── handlers/                # one file per verb
│   │   ├── create.go
│   │   ├── close.go
│   │   ├── list.go
│   │   ├── post.go
│   │   ├── read.go
│   │   └── who_else.go
│   ├── slack/                   # Slack adapter (encapsulates slack-go/slack)
│   │   ├── client.go            # *slack.Client construction, retry policy
│   │   ├── channels.go          # create / archive / lookup
│   │   ├── messages.go          # post / history
│   │   ├── encoding.go          # prefix encode / decode (pure funcs)
│   │   └── encoding_test.go
│   ├── store/                   # SQLite key + huddle store
│   │   ├── schema.sql           # initial schema (//go:embed-ed in db.go)
│   │   ├── db.go                # connection, schema apply
│   │   ├── huddles.go           # huddle CRUD
│   │   ├── huddles_test.go
│   │   ├── keys.go              # key CRUD + lookup
│   │   └── keys_test.go
│   ├── types/                   # shared types
│   │   └── types.go             # Message, Seat, Identity, etc.
│   ├── config/
│   │   └── config.go            # env var loading + validation
│   └── errors/
│       └── errors.go            # typed errors + MCP error-code mapping
```

**e2e harness lives in `cmd/smoke/`, not `test/e2e/`.** The v0 build chose a CLI smoke harness over `go test`-gated e2e for better extensibility (smoke can be hand-invoked, replayed against a fresh huddle, scripted from outside Go). Design originally proposed `test/e2e/dogfood_test.go`; impl shipped `cmd/smoke/main.go` instead.

Notable Go-isms:
- `internal/` makes packages private to the module; nothing imports `huddle/internal/...` from outside.
- `cmd/huddle/main.go` is the MCP server entry; `cmd/smoke/main.go` is the manual e2e harness — both thin wiring layers.
- `//go:embed schema.sql` in `internal/store/db.go` bundles the schema into the binary.

## Dependencies

Stay minimal. Stdlib first; reach for a module only when stdlib costs >20 lines of boilerplate.

| Module | Purpose | Notes |
|---|---|---|
| `github.com/modelcontextprotocol/go-sdk` | Official MCP SDK (stdio transport, tool registration) | v1.6.0+ — pin in `go.mod`; production-ready as of May 2026 |
| `github.com/slack-go/slack` | Slack Web API client | Well-trodden; supports rate-limit headers |
| `modernc.org/sqlite` | SQLite driver (pure Go, no CGO) | Cross-compiles cleanly; slightly slower than mattn/go-sqlite3 but worth the no-CGO trade |
| `github.com/google/uuid` | UUIDs for huddleId | Stdlib `crypto/rand` would do; uuid is one line, clearer |

Stdlib-only for:
- Config: `os.Getenv` + a small `config.Load()` function
- Validation: handwritten validators per verb (~10 lines each)
- Logging: `log/slog`
- HTTP retry: built into slack-go's client options

Test deps:
- `github.com/stretchr/testify` (`require` / `assert`) — keeps assertions concise without much ceremony

No env-var-loading library; a local `.env` is read via a tiny helper or just `direnv` on the operator's machine.

## v0 verb surface (recap)

Six verbs. Three operator-side, three agent-side.

| Verb | Side | Returns |
|---|---|---|
| `huddle.create` | operator | `{ huddleId, channel, orchestrator, seats: [{ id, key, displayName }] }` |
| `huddle.close` | operator | `{ closed: true, archivedChannel }` |
| `huddle.list` | operator | `Huddle[]` |
| `huddle.post` | agent | `{ messageId, postedAt, identity }` |
| `huddle.read` | agent | `Message[]` |
| `huddle.who_else` | agent | `{ purpose, orchestrator, seats }` |

v1 deferred: `add_seat`, `revoke_key`, `watch`.

## Handler-level pseudocode

Signatures use the official Go SDK's tool-handler shape (`github.com/modelcontextprotocol/go-sdk/mcp`): `(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error)`. Args are decoded from `req.Params.Arguments` (a `json.RawMessage`) via a small per-verb decoder using stdlib `encoding/json`.

### `huddle.create`

```go
func HandleCreate(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodeCreateArgs(req.Params.Arguments)        // validates purpose, seats; orchestrator default "orchestrator"
    if err != nil { return mcpErr(InvalidParams, err) }

    huddleID := "hud_" + uuid.New().String()                   // prefixed UUID
    channelName := slugifyChannel(args.Purpose, huddleID)      // "huddle-<purpose>-<short>"

    ch, err := deps.Slack.CreateChannel(ctx, channelName)      // conversations.create
    if err != nil { return mcpErr(InternalError, fmt.Errorf("slack create channel: %w", err)) }

    h := store.Huddle{
        ID: huddleID, Purpose: args.Purpose,
        OrchestratorDisplayName: args.Orchestrator.DisplayName,
        SlackChannelID: ch.ID, SlackChannelName: ch.Name,
        CreatedAt: time.Now().UTC(),
        TTLHours: args.TTLHours,
    }
    if err := deps.Store.InsertHuddle(ctx, h); err != nil { ... }

    seats := make([]CreatedSeat, 0, len(args.Seats))
    for _, s := range args.Seats {
        key := generateKey(huddleID, s.ID)                     // K_<huddleId>_<seatId>_<rand>
        if err := deps.Store.InsertKey(ctx, store.Key{
            Key: key, HuddleID: huddleID, SeatID: s.ID,
            DisplayName: s.DisplayName, CreatedAt: time.Now().UTC(),
        }); err != nil { ... }
        seats = append(seats, CreatedSeat{ID: s.ID, Key: key, DisplayName: s.DisplayName})
    }

    // Optional starter message in Slack (orchestrator-rendered):
    _ = deps.Slack.PostMessage(ctx, ch.ID, slack.Encode(slack.Identity{Kind: "orchestrator", DisplayName: h.OrchestratorDisplayName}, fmt.Sprintf("huddle created: %s", args.Purpose)), "")

    return mcpOK(CreateResult{HuddleID: huddleID, Channel: ch.Name, Orchestrator: Orch{DisplayName: h.OrchestratorDisplayName}, Seats: seats})
}
```

### `huddle.post`

```go
func HandlePost(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodePostArgs(req.Params.Arguments)
    if err != nil { return mcpErr(InvalidParams, err) }

    var identity slack.Identity
    var huddleID string

    if args.Key != "" {
        // seat path: key resolves identity + huddle
        k, err := deps.Store.LookupKey(ctx, args.Key)
        if err != nil { return mcpErr(InvalidParams, ErrKeyInvalid) }
        if k.RevokedAt != nil { return mcpErr(InvalidParams, ErrKeyInvalid) }
        identity = slack.Identity{Kind: "seat", SeatID: k.SeatID, DisplayName: k.DisplayName}
        huddleID = k.HuddleID
    } else {
        // orchestrator path: admin creds + explicit huddleId
        if args.HuddleID == "" { return mcpErr(InvalidParams, ErrMissingHuddleID) }
        h, err := deps.Store.LookupHuddle(ctx, args.HuddleID)
        if err != nil { return mcpErr(InvalidParams, ErrHuddleNotFound) }
        identity = slack.Identity{Kind: "orchestrator", DisplayName: h.OrchestratorDisplayName}
        huddleID = h.ID
    }

    h, err := deps.Store.LookupHuddle(ctx, huddleID)
    if err != nil { ... }
    if h.ClosedAt != nil { return mcpErr(InvalidParams, ErrHuddleClosed) }

    rendered := slack.Encode(identity, args.Body)              // "*[name] body" or "[name] body"
    ts, err := deps.Slack.PostMessage(ctx, h.SlackChannelID, rendered, args.ReplyTo)
    if err != nil { return mcpErr(InternalError, fmt.Errorf("slack post: %w", err)) }

    return mcpOK(PostResult{MessageID: ts, PostedAt: time.Now().UTC(), Identity: identity})
}
```

### `huddle.read`

```go
func HandleRead(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodeReadArgs(req.Params.Arguments)
    if err != nil { return mcpErr(InvalidParams, err) }

    h, err := resolveHuddle(ctx, deps, args.Key, args.HuddleID)
    if err != nil { return mcpErr(InvalidParams, err) }

    msgs, err := deps.Slack.History(ctx, h.SlackChannelID, args.Since, args.Limit)
    if err != nil { return mcpErr(InternalError, fmt.Errorf("slack history: %w", err)) }

    out := make([]Message, 0, len(msgs))
    for _, m := range msgs {
        if m.SubType != "" { continue }                        // skip joins, channel-create, etc.
        identity, body := slack.Decode(m.Text)                 // {seat|orchestrator|human|unknown}
        out = append(out, Message{
            ID: m.Timestamp, PostedAt: m.TimestampToTime(),
            Identity: identity, Body: body, ReplyTo: m.ThreadTimestamp,
        })
    }
    return mcpOK(out)
}
```

### `huddle.who_else`

```go
func HandleWhoElse(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodeWhoElseArgs(req.Params.Arguments)
    if err != nil { return mcpErr(InvalidParams, err) }

    h, err := resolveHuddle(ctx, deps, args.Key, "")           // key path only; operator can pass huddleId via admin route
    if err != nil { return mcpErr(InvalidParams, err) }

    seats, err := deps.Store.ListSeats(ctx, h.ID)              // active keys only
    if err != nil { return mcpErr(InternalError, err) }

    return mcpOK(WhoElseResult{
        Purpose: h.Purpose,
        Orchestrator: Orch{DisplayName: h.OrchestratorDisplayName},
        Seats: seats,                                          // omit the key value; only id + displayName
    })
}
```

### `huddle.close`

```go
func HandleClose(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodeCloseArgs(req.Params.Arguments)
    if err != nil { return mcpErr(InvalidParams, err) }

    h, err := deps.Store.LookupHuddle(ctx, args.HuddleID)
    if err != nil { return mcpErr(InvalidParams, ErrHuddleNotFound) }

    if h.ClosedAt != nil {                                     // idempotent
        return mcpOK(CloseResult{Closed: true, ArchivedChannel: h.SlackChannelName})
    }

    if err := deps.Slack.ArchiveChannel(ctx, h.SlackChannelID); err != nil {
        return mcpErr(InternalError, fmt.Errorf("slack archive: %w", err))
    }
    if err := deps.Store.MarkClosed(ctx, h.ID, time.Now().UTC()); err != nil { ... }

    return mcpOK(CloseResult{Closed: true, ArchivedChannel: h.SlackChannelName})
}
```

### `huddle.list`

```go
func HandleList(ctx context.Context, req mcp.CallToolRequest, deps *Deps) (*mcp.CallToolResult, error) {
    args, err := decodeListArgs(req.Params.Arguments)
    if err != nil { return mcpErr(InvalidParams, err) }

    huddles, err := deps.Store.ListHuddles(ctx, args.Active)
    if err != nil { return mcpErr(InternalError, err) }

    return mcpOK(huddles)                                      // omit keys; only metadata
}
```

### Shared shape — `Deps`

```go
type Deps struct {
    Slack  *slack.Adapter      // wraps slack-go/slack.Client; channels, messages, encoding
    Store  *store.Store        // SQLite operations
    Cfg    config.Config       // env-loaded settings
    Log    *slog.Logger
}
```

Constructed once in `cmd/huddle/main.go`, passed to every handler as a closure capture.

## Slack message encoding

**Format (v1):**

```
*[<displayName>] <body>     ← orchestrator
[<displayName>] <body>      ← seat
```

**Parsing rules:**

- Leading `*[` (asterisk + bracket, no space) → orchestrator
- Leading `[` (bracket only) → seat
- Read first `]` after the opening → terminates displayName
- Single space after `]` is consumed; rest is body
- displayName cannot contain `]`. Validated on `create` / `add_seat`.

**Why prefix-in-text, not Slack metadata:**

- Slack metadata fields (`username`, `icon_url` on `chat.postMessage`) require `chat:write.customize` scope and don't survive `conversations.history` cleanly.
- Text prefix survives search, mobile push notifications render correctly, and round-trips losslessly through Slack's history API.
- Block Kit is V1 cosmetic upgrade only — same parsing semantics maintained.

**Failure modes:**

- Message without a prefix (e.g., human posting directly in Slack channel): decoded as `kind: "human"`, `displayName: "user-<slack-user-id>"`. Agent reading sees these and treats them as human-typed.
- Malformed prefix (`[unclosed`): decoded as `kind: "unknown"`, full message as body. Log warning.

## Storage schema (SQLite)

```sql
-- src/store/schema.sql

CREATE TABLE IF NOT EXISTS huddles (
  id                          TEXT PRIMARY KEY,
  purpose                     TEXT NOT NULL,
  orchestrator_display_name   TEXT NOT NULL DEFAULT 'orchestrator',
  slack_channel_id            TEXT NOT NULL UNIQUE,
  slack_channel_name          TEXT NOT NULL UNIQUE,
  created_at                  TEXT NOT NULL,                       -- ISO8601
  closed_at                   TEXT,                                -- ISO8601 or NULL
  ttl_hours                   INTEGER
);

CREATE INDEX IF NOT EXISTS idx_huddles_open ON huddles(closed_at) WHERE closed_at IS NULL;

CREATE TABLE IF NOT EXISTS keys (
  key                         TEXT PRIMARY KEY,
  huddle_id                   TEXT NOT NULL,
  seat_id                     TEXT NOT NULL,
  display_name                TEXT NOT NULL,
  created_at                  TEXT NOT NULL,
  revoked_at                  TEXT,                                -- ISO8601 or NULL
  FOREIGN KEY (huddle_id) REFERENCES huddles(id) ON DELETE CASCADE,
  UNIQUE (huddle_id, seat_id)
);

CREATE INDEX IF NOT EXISTS idx_keys_huddle  ON keys(huddle_id);
CREATE INDEX IF NOT EXISTS idx_keys_active  ON keys(key) WHERE revoked_at IS NULL;
```

DB location: `${HUDDLE_STATE_DIR:-./.huddle-state}/huddle.sqlite` (the code default). The canonical deployment sets `HUDDLE_STATE_DIR` to an absolute path in the MCP server config.

Migrations: schema file applied idempotently at startup via `CREATE ... IF NOT EXISTS`. No migration tool for v0; if schema evolves, write `0002_*.sql` files and an applied-migrations table.

## Configuration

Env vars loaded by `internal/config/config.go`:

| Variable | Required | Default | Notes |
|---|---|---|---|
| `HUDDLE_SLACK_BOT_TOKEN` | per-verb | — | Slack bot token (`xoxb-...`). Required for `huddle.create` / `.close` / `.post` / `.read`. `huddle.who_else` is local-only and works without it; the server boots regardless. |
| `HUDDLE_STATE_DIR` | no | `./.huddle-state` | Where `huddle.sqlite` lives |
| `HUDDLE_LOG_LEVEL` | no | `info` | `debug` \| `info` \| `warn` \| `error` |
| `HUDDLE_CHANNEL_PREFIX` | no | `huddle-` | Prefix for created Slack channels |
| `HUDDLE_ORCHESTRATOR_SLACK_USER_ID` | no | — | If set, auto-invites this Slack user (`U…`) to every channel `huddle.create` opens. Best-effort: invite failure is logged, not propagated. |

`internal/config/config.go` reads env vars at startup but no value is strictly required for the process to boot. Slack-touching verbs surface `slack.ErrNoToken` at call time when `HUDDLE_SLACK_BOT_TOKEN` is unset; local-only verbs (`huddle.who_else`) work either way.

## Error handling

- **Input validation errors** (handwritten per-verb validators in `internal/handlers/`): MCP error code `-32602` (invalid params), human-readable message.
- **Slack API errors**:
  - Rate limit (`429`): retry up to 3× with exponential backoff (1s, 2s, 4s) using the `Retry-After` header. If still failing, return MCP error `-32603` with `slack_rate_limited`.
  - Missing scope: return `-32603` with `slack_missing_scope` and the required scope name in the message.
  - Channel not found: return `-32602` with `huddle_not_resolvable`.
  - Other: return `-32603` with the raw Slack error code in the message.
- **Storage errors** (SQLite locked, disk full): return `-32603` with `storage_error`. Don't auto-retry; let the agent decide.
- **Key not found / revoked**: return `-32602` with `key_invalid`.

No global retry/queue for `post` — failures propagate to the agent for visibility (consistent with the design decision from the pre-build verb discussion).

## Testing

Go's `testing` package + `testify` for assertions. Unit tests live next to the code (`internal/slack/encoding_test.go`, `internal/store/keys_test.go`, etc.) per Go convention.

**Unit:**
- `internal/slack/encoding_test.go` — prefix round-trip for `seat`, `orchestrator`, malformed, human (no prefix).
- `internal/store/huddles_test.go`, `keys_test.go` — CRUD against an in-memory SQLite (`file::memory:?cache=shared`). Schema-apply idempotency.
- `internal/handlers/*_test.go` — per-verb logic with a fake Slack adapter (a struct satisfying the same interface, recording calls). Cover happy path + each error code.

**Interface seam for testability:**

```go
// internal/slack/iface.go
type Adapter interface {
    CreateChannel(ctx context.Context, name string) (Channel, error)
    ArchiveChannel(ctx context.Context, id string) error
    PostMessage(ctx context.Context, channelID, text, threadTS string) (ts string, err error)
    History(ctx context.Context, channelID string, since *time.Time, limit int) ([]Message, error)
}
```

Real impl wraps `*slack.Client`; tests use a `FakeAdapter` recording calls + returning canned responses. No HTTP mocking needed.

**E2E (gated):**
- `cmd/smoke/main.go` — manual smoke harness; requires `HUDDLE_SLACK_BOT_TOKEN`; drives the huddle binary as a subprocess against a real Slack workspace.
- Creates a huddle, posts as orchestrator + seat, reads back, closes, archives. Cleans up channel on completion.
- Invoked via `go run ./cmd/smoke` or `make test-e2e` (Makefile target).

**Coverage target:** 80% lines on `internal/handlers/`, `internal/slack/encoding.go`, `internal/store/`. Lower elsewhere is fine. `go test -cover ./...` for the headline number.

## Build sequencing (suggested order)

Smallest-cuts-first so each step is reviewable:

1. **Scaffold.** `go.mod`, `Makefile`, `.golangci.yml` (strict lint, mirrors tower/orchestra/sense), `.gitignore`, `README.md`, `LICENSE`. `cmd/huddle/main.go` prints "huddle 0.0.0" and exits. `make build` produces a binary; `make lint` is green on the empty project.
2. **Storage layer.** `internal/store/` — schema.sql (//go:embed), db.go, huddles.go, keys.go + unit tests against in-memory SQLite.
3. **Slack encoding.** `internal/slack/encoding.go` — pure encode/decode for the prefix format. Unit tests round-trip seat / orchestrator / malformed / human.
4. **Slack adapter.** `internal/slack/` — client.go, channels.go, messages.go. Adapter interface + real impl. FakeAdapter for tests in `internal/slack/fake.go` (under a build tag or just `_test.go`).
5. **MCP server skeleton.** `internal/server/server.go` wires `modelcontextprotocol/go-sdk`, registers all six verbs as no-op handlers returning a stub. `cmd/huddle/main.go` starts the stdio loop.
6. **Handlers: `create` + `close`.** End-to-end manual test against a real Slack workspace (create channel, archive channel). First proof that all layers wire together.
7. **Handler: `post`** (seat + orchestrator paths). Manual verify on Slack.
8. **Handler: `read`.** Round-trip a posted message through Slack history + prefix-decode.
9. **Handlers: `who_else` + `list`.** SQLite-only; fast.
10. **E2E smoke harness.** All six verbs in one run (`cmd/smoke/main.go`, manual; gated by `HUDDLE_SLACK_BOT_TOKEN`).
11. **Dogfood session.** Two real Claude Code sessions in a huddle. Validate day-to-day UX.

Each step is a separate commit (or PR if going through Ship). Don't burst.

**Makefile targets** (matches ship + dossier conventions):

```make
# Makefile (sketch)

.PHONY: build test test-cover test-e2e lint lint-fix fmt vet run install

build:
	go build -o bin/huddle ./cmd/huddle

test:
	go test ./...

test-cover:
	go test -cover ./...

test-e2e:
	go run ./cmd/smoke

lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

fmt:
	gofmt -w .
	goimports -w .

vet:
	go vet ./...

run:
	go run ./cmd/huddle

install:
	go install ./cmd/huddle
```

`make lint` enforces the strict `golangci-lint v2` config in `.golangci.yml` — same shape as tower / orchestra / sense. CI runs it; PRs must be clean.

## Definition of done — v0 POC

- All six v0 verbs implemented + unit-tested.
- E2E test passes against a real Slack workspace.
- One real two-session dogfood scenario complete: operator creates a huddle, two Claude Code sessions join with their keys, they post + read at least 3 messages each, operator closes the huddle, history is archived in Slack.
- Operator confirms mobile-Slack UX is acceptable.
- README documents: install, env-var setup, dev / test / dogfood commands.

**Not in v0 (will land in v1):**
- `add_seat`, `revoke_key`, `watch` verbs.
- Block Kit rendering.
- One-bot-per-seat attribution.
- `ping_orchestrator` / `broadcast` / `search` / cross-runtime auth.

## Open questions (implementation-specific)

1. **MCP transport.** Stdio only for v0 (matches Claude Code's expectation)? Or also HTTP/SSE so the same binary can serve cloud-runtime agents later? Lean: **stdio only v0.** HTTP added in v1 when ship-going-remote forces it. The official SDK supports both transports under the same handler API.
2. **`modelcontextprotocol/go-sdk` version pinning.** Official SDK is at v1.6.0 (May 2026) and considered stable. Pin to a specific minor in `go.mod` and bump deliberately. Semver guarantees apply post-1.0; breaking changes shouldn't surprise within a major.
3. **Channel name uniqueness on Slack.** Slack normalizes channel names (lowercases, replaces spaces, max 80 chars). If two huddles share a sanitized purpose, append a disambiguator. Already partly mitigated by including a slice of the huddleId in the channel name; validate uniqueness in `slack.CreateChannel` and retry with a longer suffix on collision.
4. **Concurrent writes to SQLite.** `modernc.org/sqlite` supports goroutine-safe access; default journal mode is fine for v0. If contention surfaces, enable WAL via `_pragma=journal_mode(WAL)` in the connection string.
5. **Operator admin auth.** How does the MCP know "this is the operator" vs "this is a key-less call that should fail"? Lean v0: trust that any call missing a `key` is the orchestrator (single-user, operator-machine-only assumption). v1 when multi-machine: add `HUDDLE_ADMIN_TOKEN` env var checked on operator-side verbs.
6. **Key generation entropy.** Format: `K_<huddleIdShort>_<seatId>_<base32rand>` with 16 bytes of `crypto/rand` data base32-encoded → 26 chars random suffix. Collision-resistant within billions of keys.
7. **Bot identity in Slack.** Single bot user (e.g., "huddle-bot") posts everything; the prefix carries attribution. Validated in the catalog spec; reaffirmed here.
8. **CLAUDE.md content.** The project's agent-guidance doc should describe: how the MCP is run locally (`make run`), common dev tasks (`make test`, `make build`, `make test-e2e`), env-var setup, the verb surface, and a pointer to this design doc. Drafted alongside scaffold step.
9. **CGO-free build.** `modernc.org/sqlite` is pure Go — `CGO_ENABLED=0 go build` produces a fully static binary. Useful when ship-going-remote lands and huddle deploys to a minimal container. Document this guarantee in the README.

## What this doc does not cover

- **Catalog-level "why."** That's in [`pers/mcp-workstation/huddle.md`](../../mcp-workstation/huddle.md).
- **The director / panel concept** (operator-orchestrated dialogue patterns). Different project. Could consume huddle later.
- **Ship-going-remote.** Tier 0 dependency for managed-agent integration. Out of scope for huddle.

## Outcome

*Populated after the POC ships: dogfood results, verb-surface adjustments, what the design got wrong vs. right, first signs of where v1 work is justified.*
