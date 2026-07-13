# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

huddle is a Go MCP server that opens a Slack channel per "huddle," issues per-seat keys (each key = an identity), and lets agents post + read through MCP verbs with automatic attribution. The operator is the implicit orchestrator — distinct identity, visible to every agent in the room.

Seven v0 verbs (`huddle.create`, `huddle.close`, `huddle.list`, `huddle.post`, `huddle.read`, `huddle.who_else`, `huddle.invite_human`) all live behind the official `github.com/modelcontextprotocol/go-sdk` stdio transport. Storage is local SQLite (modernc, pure-Go, no CGO).

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

These MCPs, planes, and skills are available in any agent session on this machine; the harness injects each tool's signature, so this is the *map* — how they compose — not the per-verb manual. When the signal matches, call the verb; don't ask permission. Stuck on a *knowledge* question about another portfolio repo → `/consult` its steward; only *authority* questions (direction, spend, irreversible calls) go to the operator. **This is huddle — the optional multi-seat coordination tool**; note it is off the normal PR path and is not a Flare dependency.

**MCPs (in-session):**
- **dossier** — durable project memory: projects → phases → tasks → artifacts (markdown-on-disk).
- **ship** — the driver engine: dispatch a task to a cloud/local agent and persist the run (dispatch→poll→judgment→land→record); inspect/cancel/replay.
- **huddle** — *optional* multi-seat coordination (Slack-backed); off the normal PR path.
- **playwright** — browser automation when a task needs a real DOM.

**Planes (CLIs, composed via exit codes + JSONL — not MCPs):**
- **gate** — authorization: evaluates the *exact* PR head, emits governed-path merge authorization. Findings ≠ authorization; gate is the merge boundary.
- **flare** — notification: best-effort escalation sink over authoritative receipts → its own Slack app/channel. Pure sink; never gates; not built on huddle.

**Skills:**
- **/work-driver** [+ **/work-driver-prep**] — drive agent-led impl end-to-end; prep builds the specs + conflict-batched plan.
- **/pr-risk** — size how much review a PR needs (deterministic floor + agent advisory); upstream of the reviewers — it decides *how much*, they *do* it.
- **/review-coordinator** [+ **/review-digest**] — consolidate the AI PR reviewers into one verdict (the judge over the finders); digest pre-triages the bot pile locally.
- **/shipped** · **/status** · **/wip** — retrospective recap · in-flight update · cross-store live board.
- **/consult** — summon a sibling repo's steward for a same-turn answer; knowledge → peer, authority → operator.
- **/worktree-*** — add · list · remove · transfer · where, over `git worktree`.

### The loop

```
dossier task → /worktree-add → spec → ship driver (cloud-first: dispatch→poll→judgment→land→record)
   → PR + CI → /pr-risk tiers it → reviewers fire → /review-coordinator → one verdict
   → gate evaluates the exact head → governed-path authorization → merge
   → authoritative receipts → dossier close-out → /worktree-remove
        ↘ any attention/terminal receipt → best-effort flare sweep → Slack   (independent; never gates)
```

`/work-driver` coordinates dispatch→poll→land and runs its own review triage inline. `/pr-risk` and `/review-coordinator` are steps you *invoke* — the driver→pr-risk / driver→coordinator wiring is planned, not built, so nothing here auto-delegates.

### Why this shape

Each layer owns one responsibility and is swappable without rippling: dossier owns *what needs doing*; worktree skills own *where work happens*; ship owns *drive an agent + persist the run*; pr-risk owns *how much review*; review-coordinator owns *consolidate the finders* (the bots are swappable under it); **gate owns *authorization* — is this exact head allowed to merge — which is not the reviewers' findings**; **flare owns *notification* — a best-effort sink on authoritative receipts, its own Slack app, never blocking the driver, never depending on huddle**; consult owns the stuck path; huddle owns optional multi-seat; playwright owns browser. The workbench is a menu, not a checklist — skip what a flow doesn't need.

### The shape underneath

These tools instantiate the redesign's five contract planes — coupled only by typed artifacts (`evidence → verdict → action`), never call stacks:

- **State** (remembers) — dossier + run/verdict/grant/receipt artifacts; the append-only substrate.
- **Execution** (does) — ship's driver; emits evidence, never judges itself.
- **Verification** (judges) — the escalate-only ladder (deterministic floor → local → premium), monotone `worst`/`max`: gate's reducer, review-coordinator, sense/triage/tracelens.
- **Capability** (bounds) — scoped/timed grants; every effectful verb needs a live grant + a supporting verdict.
- **Observability** (explains) — read-only, storeless views from State: flare, /wip, /shipped, /status.

This section is the sixth — **Composition**: the agent + thin policy choosing which planes a task needs. The boundaries above *are* the plane laws, not conventions.
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
- **`internal/server/`** — MCP lifecycle. `RegisterVerbStubs` wires every handler. Despite the name, no stubs remain — all seven v0 verbs have real handlers.
- **`internal/handlers/`** — one file per verb (`create.go`, `close.go`, `list.go`, `post.go`, `read.go`, `who_else.go`), plus `resolve.go` (key-vs-huddleId speaker resolution shared by post + read) and `deps.go` (typed Deps struct). Each handler exports a `Register<Verb>(s, deps)` that calls `mcp.AddTool(...)`.
- **`internal/slack/`** — Slack façade. `Adapter` interface (`iface.go`) is the seam — handlers depend on it, never on `slack-go` directly. Real impl is `slackGoAdapter` (`impl.go` / `channels.go` / `messages.go`); `FakeAdapter` (`fake_adapter.go`) records calls + returns canned data for handler tests. Message-prefix encoding (`[displayName] body` for seats, `*[displayName] body` for orchestrator) lives in `encoding.go` and is the source of truth for identity-on-the-wire.
- **`internal/store/`** — SQLite via `modernc.org/sqlite`. Schema is `//go:embed`-ed from `schema.sql`. Two tables: `huddles` (one row per huddle) and `keys` (per-seat keys; FK to huddles with `ON DELETE CASCADE`). Constructor `store.New(stateDir)` opens / applies schema; `OpenMemory(ctx)` is the test fixture.
- **`internal/config/`** — env-only `Load()`. No required env vars at startup. `HUDDLE_SLACK_BOT_TOKEN` gates Slack-touching verbs (`create` / `close` / `post` / `read`) — the slack adapter's `noTokenAdapter` returns `slack.ErrNoToken` at call time when it's unset, so `huddle.who_else` still serves (it lists channel humans when a token is present; `humans: []` when not). Optional: `HUDDLE_STATE_DIR`, `HUDDLE_LOG_LEVEL`, `HUDDLE_CHANNEL_PREFIX`, `HUDDLE_ORCHESTRATOR_SLACK_USER_ID` (auto-invites a human to every channel `huddle.create` opens; best-effort).
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

<!-- local-offload:start -->
## Local-first offload

Before spending cloud tokens on a mechanical sub-step, check for a free local path (needs the `local` CLI / Ollama on this machine):

- Narrowing a big file list, extracting structure from noisy tool output, shallow classification -> `/offload`
- "Have we solved/decided this before?" questions about the operator's own work -> `/ask-portfolio`
- Triaging a PR's bot-comment pile -> `/review-digest <PR#>`

Deep judgment (code review, risk calls, dense-diff reasoning) stays with the primary model. If `local` is not on PATH, skip silently -- never block on this.
<!-- local-offload:end -->
