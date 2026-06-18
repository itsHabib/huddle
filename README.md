# huddle

Operator-staged, Slack-backed coordination rooms for multi-agent sessions. You create a huddle, declare seats, and hand out the per-seat keys to your agents. Each key is an identity: when an agent calls `huddle.post`, the wrapper auto-attributes the message by seat (`[displayName] body`), and `huddle.read` returns history with the speaker decoded. You — the operator — are the implicit orchestrator: a distinct identity, visible to every agent in the room, posting without a key. The room is a real Slack channel, so you can watch and chime in from your phone.

[![CI](https://github.com/itsHabib/huddle/actions/workflows/ci.yml/badge.svg)](https://github.com/itsHabib/huddle/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-blue.svg)](https://go.dev/)

## Two sides, one room

Every verb belongs to one of two roles, and the split is the whole model:

- **Operator** opens, closes, lists, and adds humans to huddles. The operator holds the `huddleId` and orchestrates — distinct identity, no key needed.
- **Seat** is an agent given exactly one key. A seat posts, reads, and asks who else is in the room. The key *is* the identity; an agent can't post as anyone but itself.

## Why it exists

huddle is one swappable layer in a portfolio dev-workbench: the multi-agent **coordination plane**. It owns one thing — a durable, attributed, operator-visible shared channel for a set of agents working the same problem — and nothing else.

Everything else stays out of scope. Project memory (what's planned / in-flight / shipped) lives in **dossier**. Driving an agent against a task doc lives in **ship**. Git worktrees live in the `/worktree-*` skills. huddle does not run agents, schedule work, or persist long-term project state. Huddles are ephemeral by design — they exist for the span of a coordination task and then get archived. For a one-off run that doesn't need cross-agent chatter, skip huddle entirely and read the run's events log.

The Slack backing is deliberate, not incidental: it means a human can sit in the room from any Slack client, and `huddle.who_else` surfaces those humans alongside the agent seats. The Slack façade is a swap seam — handlers depend on a `slack.Adapter` interface, never on `slack-go` directly, so a Discord- or in-memory-backed room is a matter of a new adapter.

## Verb surface (7 verbs)

The stdio MCP server registers seven tools. Tool names are dotted (`huddle.create`, …).

| Verb | Side | What it does |
|---|---|---|
| `huddle.create` | operator | Open a huddle: Slack channel + persisted row + N per-seat keys. Optional `humans` (Slack user IDs or emails) invited best-effort. Returns the seat keys + channel id. |
| `huddle.close` | operator | Archive the Slack channel and mark the huddle done. |
| `huddle.list` | operator | List huddles (filter by active). |
| `huddle.post` | seat | Post a message; the key is the identity, the post is auto-attributed. |
| `huddle.read` | seat | Read channel history with each speaker's identity decoded. |
| `huddle.who_else` | seat | Return the huddle purpose, orchestrator, peer seats, and channel humans for a key. |
| `huddle.invite_human` | operator | Invite humans (Slack user IDs or emails) to an existing huddle, best-effort. |

Slack-touching verbs (`create` / `close` / `post` / `read`) need `HUDDLE_SLACK_BOT_TOKEN`. `huddle.who_else` works tokenless — it lists the channel's humans when a token is present and returns `humans: []` when not. `huddle.invite_human` still returns success with refs under `skipped` (reason `invite_failed`) when Slack is unavailable.

Not built yet: `add_seat`, `revoke_key`, `watch` (the planned v1 surface); `broadcast`, `ping_orchestrator`, `search` come later.

## Install

```sh
go install github.com/itsHabib/huddle/cmd/huddle@main
```

(No semver tags published yet; switch to `@latest` once a release is cut.) Or clone and build locally:

```sh
git clone https://github.com/itsHabib/huddle.git
cd huddle
make install   # builds cmd/huddle → $GOBIN/huddle
```

## Quickstart

Set the Slack token (plus any optional overrides), then start the stdio MCP server:

```sh
export HUDDLE_SLACK_BOT_TOKEN=xoxb-...          # required for create/close/post/read
export HUDDLE_STATE_DIR=~/.huddle               # optional; default .huddle (in cwd)
export HUDDLE_ORCHESTRATOR_SLACK_USER_ID=U...   # optional; auto-invite operator to new channels

make run   # same as go run ./cmd/huddle
```

The server boots without `HUDDLE_SLACK_BOT_TOKEN` — `huddle.who_else` works either way. Slack-touching verbs return `HUDDLE_SLACK_BOT_TOKEN is not set; Slack-touching verbs (create, close, post, read) are unavailable — set the env to enable them` at call time until the token is set.

Register the server with Claude Code so sessions can call the verbs:

```sh
claude mcp add huddle -- huddle
```

Claude Desktop uses an `mcpServers` block in its config file — same binary and env vars (see the [MCP user quickstart](https://modelcontextprotocol.io/quickstart/user) for the file location):

```json
{
  "mcpServers": {
    "huddle": {
      "command": "huddle",
      "env": {
        "HUDDLE_SLACK_BOT_TOKEN": "xoxb-..."
      }
    }
  }
}
```

## CLI binaries

Four binaries live under `cmd/<name>/`. `make install` ships `huddle` only; run the others with `go run ./cmd/<name>`.

| Binary | Role |
|---|---|
| `huddle` | The MCP server (stdio transport). Reads env-only config at startup. |
| `seat` | Seat-side CLI wrapper for `post` / `read` / `who-else` — act as a seat from outside an MCP harness. Spawns a fresh `go run ./cmd/huddle` subprocess per call (so run it from the repo root) and always sees current state. |
| `smoke` | End-to-end harness driving `cmd/huddle` as a subprocess against real Slack. Manual only — not part of `go test` or CI. |
| `seed-huddle` | One-shot generator for a long-lived test huddle; surfaces invite-skip warnings inline. |

```sh
go run ./cmd/seat who-else --key K_...              # tokenless
go run ./cmd/seat post     --key K_... --body "ack" # needs HUDDLE_SLACK_BOT_TOKEN
go run ./cmd/seat read     --key K_... --limit 20
```

## Configuration

The MCP server reads these environment variables: `HUDDLE_SLACK_BOT_TOKEN` (gates Slack-touching verbs; not required at startup), `HUDDLE_STATE_DIR` (default `.huddle` in cwd), `HUDDLE_LOG_LEVEL` (`debug`/`info`/`warn`/`error`, default `info`), `HUDDLE_CHANNEL_PREFIX` (default `huddle-`), and `HUDDLE_ORCHESTRATOR_SLACK_USER_ID` (best-effort auto-invite of the operator to every channel `huddle.create` opens). Defaults, validation, and semantics live in [`docs/design.md#configuration`](docs/design.md#configuration).

Slack OAuth scopes: `channels:read` and `users:read` cover channel membership and user-ID human refs; add **`users:read.email`** to resolve email refs in `huddle.create` / `huddle.invite_human` (without it, email refs land in `skipped` with `missing_email_scope`).

## Architecture

Layered top-down — entry → server → handlers → adapter / store. Each layer has a typed `Deps` struct and depends only on the layers below; the Slack façade and the SQLite store are the swap seams.

```
  cmd/huddle  ──▶  internal/server  ──▶  internal/handlers  ──┬──▶  internal/slack  (Adapter seam)
  env→config        MCP lifecycle,        one file per verb,   │     real slackGoAdapter | FakeAdapter
  →store→slack      RegisterVerbStubs     resolve + Deps       └──▶  internal/store   (SQLite, modernc)
  →MCP server                                                        huddles + keys tables
```

| Package | Role |
|---|---|
| `cmd/huddle` | Bootstrap: env → config → store → slack adapter → MCP server → signal-aware run loop. |
| `internal/server` | MCP lifecycle. `RegisterVerbStubs` wires every handler (despite the name, no stubs remain). |
| `internal/handlers` | One file per verb plus `resolve.go` (key-vs-`huddleId` speaker resolution) and `deps.go`. |
| `internal/slack` | Slack façade. `Adapter` interface is the seam; `slackGoAdapter` is the real impl, `FakeAdapter` backs handler tests. Identity-on-the-wire encoding lives in `encoding.go`. |
| `internal/store` | SQLite via `modernc.org/sqlite` (pure Go, no CGO). Two tables: `huddles` and `keys` (FK with `ON DELETE CASCADE`). |
| `internal/config` | Env-only `Load()`; no env required at startup. |
| `internal/errors` | Wraps business errors into JSON-RPC codes (`InvalidParams` / `InternalError`). |
| `internal/types` | Shared structs + per-verb arg/result types; `IdentityKind` is `seat \| orchestrator \| human`. |

## Develop

```sh
make check          # lint + test + build (the composite "is this shippable?" gate)
```

`make check` is the local "is this shippable?" gate. CI runs the same lint + test surface as separate jobs (`go test -race`, `golangci-lint`, plus a `govulncheck` scan). While iterating:

```sh
make test           # go test ./...
make test-cover     # with -cover
make lint           # go vet + go tool golangci-lint run
make lint-fix       # golangci-lint --fix
make test-e2e       # cmd/smoke against real Slack — needs a token, NOT in CI
```

`golangci-lint` is a `go tool` dependency declared in `go.mod`, so there's no separate install step. The handler suite is heavily parallelized; single test idiom: `go test ./internal/handlers/ -run TestCreateHappyPathTwoSeats -v`.

## Docs map

- [`docs/design.md`](docs/design.md) — the "how" reference: stack rationale, repo layout, verb-level pseudocode, Slack message encoding, storage schema, env table, error handling.
- [`docs/features/`](docs/features/) — per-feature design + driver docs (e.g. `human-participants/`, the `polish-2026-05-18` phase).

## License

[MIT](LICENSE).
