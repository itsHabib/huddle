# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

huddle is a Go MCP server that opens a Slack channel per "huddle," issues per-seat keys (each key = an identity), and lets agents post + read through MCP verbs with automatic attribution. The operator is the implicit orchestrator — distinct identity, visible to every agent in the room.

Six v0 verbs (`huddle.create`, `huddle.close`, `huddle.list`, `huddle.post`, `huddle.read`, `huddle.who_else`) all live behind the official `github.com/modelcontextprotocol/go-sdk` stdio transport. Storage is local SQLite (modernc, pure-Go, no CGO).

## Dev commands

Driven by `Makefile` — prefer these over raw `go` invocations so behavior stays consistent with CI / sibling repos.

```
make build         # build ./cmd/huddle → ./huddle
make install       # build then copy to $GOBIN
make test          # go test ./...
make test-cover    # with -cover
make test-e2e      # HUDDLE_E2E=1 against real Slack (NOT in `make test`)
make lint          # go vet + go tool golangci-lint run
make lint-fix      # same with --fix
make check         # lint + test + build  (the composite "is this shippable?" gate)
make run           # go run ./cmd/huddle  (foreground stdio MCP server)
```

`golangci-lint` is invoked via `go tool golangci-lint` (declared in `go.mod`), so there's no separate install step.

Single-test idiom: `go test ./internal/handlers/ -run TestCreateHappyPathTwoSeats -v`. The handler suite is heavily parallelized — most tests call `t.Parallel()`.

## Binaries under `cmd/`

- **`huddle`** — the MCP server. Stdio transport; reads env-only config at startup.
- **`seat`** — small CLI wrapper for seat-side verbs (post, read, who_else). Useful for hand-running without an MCP harness.
- **`smoke`** — end-to-end harness that spawns `cmd/huddle` as a subprocess and drives it through real Slack. Manual smoke runs only — NOT part of `go test`.
- **`seed-huddle`** — one-shot generator for long-lived test huddles. Surfaces invite-skip warnings inline.

The `test/e2e/` directory referenced in `docs/design.md` was superseded by `cmd/smoke` during v0 — design doc not yet updated (see polish phase `polish-2026-05-18`).

<!-- BEGIN dev-workbench (managed by /dev-workbench skill — re-run to refresh; hand-edits inside this block will be overwritten) -->
## Dev workbench

Several MCP servers + skills are wired into every Claude Code session on this machine. **This is huddle — the multi-agent coordination plane itself** — so when working in this repo, the huddle verbs are the most directly relevant; the rest of the workbench is the surrounding scaffolding.

### dossier — project memory plane

Markdown-on-disk corpus of projects → phases → tasks, exposed via an MCP server (Rust). The system-of-record for "what are we working on, where are we in it, what's parked." Every implementation decision lands as a task body; every phase rolls up to a project.

**Use proactively for:**

- *"start a new task"* → `mcp__dossier__task_create`
- *"what's open under huddle"* → `mcp__dossier__task_list { project: "huddle", status: ["todo"] }`
- *"what shipped recently"* → `mcp__dossier__task_list` with `completed_after`
- *"close this out"* → `mcp__dossier__task_complete` (after `task_claim` + `task_update status=in_progress`)
- *"add a polish phase"* → `mcp__dossier__phase_add`

**Don't use for:**

- Ad-hoc notes / scratch — those live next to the code.
- Cross-project source code search — use `Grep` / `Glob` directly.

### ship — workflow execution

TS MCP server that hands a task doc to cursor, persists run output (logs + result.json + events.ndjson), lets you inspect / cancel / replay. The "send this work to a worker and tell me when it's done" verb.

**Use proactively for:**

- *"ship this task"* → `mcp__ship__ship { taskDoc, runtime: "cloud" }`
- *"what runs are still going"* → `mcp__ship__list_workflow_runs`
- *"why did that run fail"* → `mcp__ship__get_workflow_run` + `mcp__ship__get_artifacts`
- *"cancel that run"* → `mcp__ship__cancel_workflow_run`

**Don't use for:**

- Interactive impl that needs back-and-forth with the operator.
- Multi-task batching — that's `/work-driver` + `/work-driver-prep`'s job.

### huddle — multi-agent coordination via Slack

**This is huddle.** Operator creates a huddle (= Slack channel + persisted row + N seat keys), hands out per-seat keys to agents. Each `huddle.post` is auto-attributed (`[displayName] body`); `huddle.read` returns history with decoded identity. The room is operator-visible by default via `HUDDLE_ORCHESTRATOR_SLACK_USER_ID`.

**Use proactively for:**

- *"set up a room where two agents can talk"* → `huddle_create { seats: [...] }`
- *"as seat X, say Y"* → `huddle_post { key, body }`
- *"what's in that room"* → `huddle_read { key | huddleId }`
- *"who else is in here"* → `huddle_who_else { key }`
- *"archive the room"* → `huddle_close { huddleId }`

**Don't use for:**

- DMs to a single human — use Slack directly.
- Persistent project chat that outlives a single coordination task — huddles are ephemeral by design.

### playwright — browser automation

DOM-aware browser-driving MCP plugin (navigation, click, fill, screenshot, network inspection). Use when the target is a web app and you need real-page interaction — much faster than computer-use pixel-clicking.

**Use proactively for:**

- *"check what this page renders"* → `browser_navigate` + `browser_snapshot`
- *"fill this form"* → `browser_fill_form`
- *"is the API call going through"* → `browser_network_requests`

**Don't use for:**

- Native desktop apps — that's computer-use's tier.
- Headless scraping where `curl` + `jq` works.

### `/work-driver` — drive agent-led impl end-to-end

Coordinates one or N parallel implementation streams. Pre-flights worktrees → fans out via `mcp__ship__ship` → polls terminal state → commits if cursor didn't auto-commit → opens PRs → coordinates review cycles → merges in dep order.

**Triggers:** *"drive this task through to merge"*, *"ship these in parallel"*, explicit `/work-driver`.

**Pair with:** `/work-driver-prep` when starting from a dossier phase rather than a hand-drafted spec.

### `/work-driver-prep` — spec docs + batched plan from a dossier phase

Resolves a phase slug (or a list of task IDs), generates a spec doc per task, detects file-overlap conflicts, groups into parallel-safe batches, and emits ready `/work-driver` invocations.

**Triggers:** *"prep these N tasks for parallel ship"*, *"build the work-driver plan from polish-YYYY-MM-DD"*, explicit `/work-driver-prep`.

**Pair with:** `/work-driver` immediately after — the output of prep is the input to driver.

### `/shipped` — retrospective recap after a chunk lands

Post-`/work-driver` recap: PRs merged + weighted LOC, dossier task closures, chips filed, friction-log delta, what's open, next moves. The "OK, what just happened and what's next" view.

**Triggers:** *"recap what shipped"*, *"what just happened"*, *"give me the after-action"*, explicit `/shipped`.

**Pair with:** runs naturally after `/work-driver` finishes; also useful at end-of-session for a clean handoff.

### `/status` — tight 4-section in-flight status

Mid-session status update: What happened / What's next / What I recommend / What I need from you. Four sections, terse. The counterpart to `/shipped` for live coordination.

**Triggers:** *"give me a status"*, *"where are we"*, *"what do you need from me"*, explicit `/status`.

**Pair with:** use mid-session; `/shipped` is the post-completion equivalent.

### `/worktree-*` — manage secondary git worktrees

Thin skill family over plain `git worktree`. Use these instead of reaching for an MCP — they cover the verbs that mattered (add, list, remove, transfer, where) without an external state store.

- **`/worktree-add`** — *"spin up a worktree for <ticket>"* → creates `.claude/worktrees/<branch>/`, copies untracked CLAUDE.md if present
- **`/worktree-list`** — *"what worktrees do I have"* → branch, dirty state, optional PR/CI from `gh`
- **`/worktree-remove`** — *"clean up the worktree"* → dirty-state aware (commit-WIP / stash / discard)
- **`/worktree-transfer`** — *"bring this work over to main"* → removes secondary, checks out branch in root
- **`/worktree-where`** — *"where am I"* → which worktree, branch, and cwd this session is pointing at

### The loop

```
dossier task                              ←─ project memory (source of truth)
   │
   ▼
/worktree-add                              ←─ isolated checkout
   │
   ▼
ship.ship  ──► cursor implements ──► run terminal
   │                                       │
   │             /work-driver coordinates ─┘
   ▼
PR opened ──► reviews (@codex, @claude, Copilot) ──► merge
   │
   ▼
dossier task_complete                      ←─ close-out
   │
   ▼
/worktree-remove (or /worktree-transfer)   ←─ cleanup
   │
   ▼
/shipped                                   ←─ recap
```

### Why this shape

Each layer is independently swappable: dossier could be Linear, the worktree skills could be hand-rolled `git worktree` calls, ship could be a different agent runner, huddle could be Discord-backed. The seams are deliberate — substituting one doesn't ripple into the others. dossier owns "what" but not "how"; ship owns "how" but not "what"; skills compose them without owning state of their own.

The opinionation is the value — every PR uses the same review set, every branch lives under the same path, every task body has the same shape. Resist making any of this configurable.
<!-- END dev-workbench -->

<!-- BEGIN eng-philo (managed by /eng-philo — re-run to refresh; hand-edits inside this block will be overwritten) -->
## Engineering principles

How code is written here — Dave Cheney lineage ([Practical Go](https://dave.cheney.net/practical-go)): simplicity, clarity, line-of-sight. Apply on every change; the lint below catches the slips.

1. **No `else` — line-of-sight.** Handle errors / edge cases with early returns and guard clauses; keep the happy path un-indented, flowing down the left margin. Reaching for `else` → return early instead.
2. **Shallow nesting — ≤2 levels *per scope*.** A `for` + an `if` is the ceiling in one scope. The budget is per-scope, not per-function — a closure / anon fn is its own scope, so a `for`+`if` inside a closure is fine. Deeper in one scope → extract a function.
3. **Policy vs mechanism.** Separate the decisions (policy: validation, state machines, business rules) from the plumbing (mechanism: persistence, transport, I/O). Mechanism is dumb and swappable; policy lives in a layer above it. Never let policy leak into a mechanism layer.
4. **Composition of single-responsibility layers.** Each layer / package owns ~one responsibility; the app is a *composition* of them; any piece is swappable without rippling into the others. Dependencies flow one direction.
5. **Small, sharp APIs.** Export the least callers need. Intention-revealing names. Accept the narrowest input, return concrete types. Make the zero value useful.
6. **Errors are values; simplicity over cleverness.** Handle or propagate errors explicitly — never swallow. Readable > clever > short. A little copying beats a premature abstraction or dependency.

### Go idioms + enforcement

Accept interfaces, return structs; small interfaces (1–2 methods); errors lowercase + wrapped (`%w`); early-return / line-of-sight.

*Enforce:* golangci-lint — `gocognit`, `nestif`, `cyclop`, `revive`.
<!-- END eng-philo -->

## Architecture

Layered top-down: **entry → server → handlers → adapter/store**. Each layer is small, has a typed Deps struct, and depends only on layers below.

- **`cmd/huddle/main.go`** — bootstrap: env → config → store → slack adapter → MCP server → signal-aware run loop.
- **`internal/server/`** — MCP lifecycle. `RegisterVerbStubs` wires every handler. Despite the name, no stubs remain — all six v0 verbs have real handlers.
- **`internal/handlers/`** — one file per verb (`create.go`, `close.go`, `list.go`, `post.go`, `read.go`, `who_else.go`), plus `resolve.go` (key-vs-huddleId speaker resolution shared by post + read) and `deps.go` (typed Deps struct). Each handler exports a `Register<Verb>(s, deps)` that calls `mcp.AddTool(...)`.
- **`internal/slack/`** — Slack façade. `Adapter` interface (`iface.go`) is the seam — handlers depend on it, never on `slack-go` directly. Real impl is `slackGoAdapter` (`impl.go` / `channels.go` / `messages.go`); `FakeAdapter` (`fake_adapter.go`) records calls + returns canned data for handler tests. Message-prefix encoding (`[displayName] body` for seats, `*[displayName] body` for orchestrator) lives in `encoding.go` and is the source of truth for identity-on-the-wire.
- **`internal/store/`** — SQLite via `modernc.org/sqlite`. Schema is `//go:embed`-ed from `schema.sql`. Two tables: `huddles` (one row per huddle) and `keys` (per-seat keys; FK to huddles with `ON DELETE CASCADE`). Constructor `store.New(stateDir)` opens / applies schema; `OpenMemory(ctx)` is the test fixture.
- **`internal/config/`** — env-only `Load()`. No required env vars at startup. `HUDDLE_SLACK_BOT_TOKEN` gates Slack-touching verbs (`create` / `close` / `post` / `read`) — the slack adapter's `noTokenAdapter` returns `slack.ErrNoToken` at call time when it's unset, so `huddle.who_else` (local-only) still serves. Optional: `HUDDLE_STATE_DIR`, `HUDDLE_LOG_LEVEL`, `HUDDLE_CHANNEL_PREFIX`, `HUDDLE_ORCHESTRATOR_SLACK_USER_ID` (auto-invites a human to every channel `huddle.create` opens; best-effort).
- **`internal/errors/`** — `huddleerr.MCPError(jsonrpc.Code*, err)` wraps business errors into JSON-RPC errors. Validation → `CodeInvalidParams` (-32602); runtime → `CodeInternalError` (-32603).
- **`internal/types/`** — shared structs (`Huddle`, `Seat`, `Identity`, `Message`, plus `CreateArgs` / `ReadArgs` / etc. per-verb arg + result types). `IdentityKind` is `seat | orchestrator | human`.

## Cross-cutting patterns

These are the patterns to mimic when adding new handlers or verbs.

**Compensation paths.** Multi-step operations (e.g. `huddle.create`: Slack channel + huddle row + N keys) must clean up partial state if a later step fails. Pattern: archive the Slack channel; cascade-delete the huddle row (FK takes care of keys). Cleanup uses `context.WithoutCancel(ctx) + WithTimeout` so it survives caller cancellation. Errors during cleanup are slog-warned, never propagated — the original error is the headline. See `archiveOrphanChannel` / `deleteOrphanHuddle` in `internal/handlers/create.go`.

**Adapter interface for testability.** Handlers depend on `slack.Adapter` (the seam), never on `slack-go` directly. New Slack calls go through new interface methods + `FakeAdapter` recording. Don't reach into the underlying client from handler code.

**Idempotent-on-retry Slack ops.** `ArchiveChannel` treats `already_archived` as success. `InviteUserToChannel` treats `already_in_channel` as success. Add this pattern to new Slack ops where a retry from a partial-success path should converge instead of erroring.

**Validation up front.** Handlers normalize + validate args before any side effect (see `validateAndNormalizeCreate`). Validation errors return `CodeInvalidParams` so MCP clients can render a clean prompt back.

**JSONSCHEMAGODEBUG quirk.** `cmd/huddle/main.go` sets `JSONSCHEMAGODEBUG=typeschemasnull=1` at startup as a workaround for Claude Code's MCP harness rejecting `"type": ["null", "T"]` unions on slice fields. Pointer/optional fields (`TTLHours`, `ListArgs.Active`, `ReadArgs.Since`) still publish unions but are optional, so clients omit them. Don't strip this until a per-tool `InputSchema` override is in place.

## Cursor subagents

`.cursor/agents/` ships a curated subagent set used during `ship.ship` implementation runs: `code-reviewer`, `scope-tracker`, `test-author`, `validator`, `ci-checker`. Steering rule in `.cursor/rules/use-subagents.mdc`. Dispatch from the parent agent at the natural seams (after writing new code → `test-author`; before final summary → `code-reviewer` + `scope-tracker` + `validator`).

If `make check` is red, run `validator` to get the diagnosis instead of guessing.

## Source of truth pointers

- `docs/design.md` — the "how" reference. Stack rationale, layout sketch, verb-level pseudocode, schema, env table. (Note: 2026-05-18 polish phase `polish-2026-05-18` queued for TS-era residue + e2e-dir-vs-cmd-smoke drift.)
- `../mcp-workstation/huddle.md` — the "what" catalog entry (problem framing, MCP surface, mental model).
- `~/pers/dossier-state/projects/huddle/` — task tracking; ongoing work lives here.
