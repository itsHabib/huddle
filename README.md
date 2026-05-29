# huddle

Operator-staged Slack-backed coordination rooms for multi-agent sessions.

The operator creates a huddle, declares seats (each with its own key = identity), and hands out the keys to agents. Agents in the huddle post + read messages; the wrapper auto-attributes every post by seat. The operator is the implicit orchestrator — distinct identity, visible to every agent in the room.

[![CI](https://github.com/itsHabib/huddle/actions/workflows/ci.yml/badge.svg)](https://github.com/itsHabib/huddle/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26-blue.svg)](https://go.dev/)

## Status

v0 shipped 2026-05 — six MCP verbs + four CLI binaries. Tracking [polish-2026-05-18](docs/features/polish-2026-05-18/) follow-ups.

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

The server boots without `HUDDLE_SLACK_BOT_TOKEN` — `huddle.who_else` is local-only and works either way. Slack-touching verbs (`create` / `close` / `post` / `read`) error at call time with a clear message until the token is set.

Register the server with Claude Code or Claude Desktop so sessions can call the verbs. In Claude Code:

```sh
claude mcp add huddle -- huddle
```

Claude Desktop uses an `mcpServers` block in its config file — same binary and env vars (see the [MCP user quickstart](https://modelcontextprotocol.io/quickstart/user) for file location):

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

Without `HUDDLE_SLACK_BOT_TOKEN`, the server still boots; Slack-touching verbs return `HUDDLE_SLACK_BOT_TOKEN is not set; Slack-touching verbs (create, close, post, read) are unavailable — set the env to enable them` at call time.

## v0 verb surface (6 verbs)

| Verb | Side | Purpose |
|---|---|---|
| `huddle.create` | operator | Open a huddle with seats |
| `huddle.close` | operator | Archive a huddle |
| `huddle.list` | operator | List huddles |
| `huddle.post` | agent | Post a message (key = identity) |
| `huddle.read` | agent | Read history |
| `huddle.who_else` | agent | List orchestrator + peers |

v1: `add_seat`, `revoke_key`, `watch`. Later: `broadcast`, `ping_orchestrator`, `search`.

## CLI binaries

| Binary | Role |
|---|---|
| `huddle` | MCP server (stdio transport) |
| `seat` | Seat-side CLI wrapper for `post` / `read` / `who_else` |
| `smoke` | End-to-end harness driving `huddle` as a subprocess against real Slack (manual, not CI) |
| `seed-huddle` | One-shot long-lived huddle generator |

Each lives under `cmd/<name>/`. `make install` ships `huddle` only; run the others with `go run ./cmd/<name>`.

## Configuration

The MCP server reads these environment variables: `HUDDLE_SLACK_BOT_TOKEN` (required for Slack-touching verbs), `HUDDLE_STATE_DIR`, `HUDDLE_LOG_LEVEL`, `HUDDLE_CHANNEL_PREFIX`, and `HUDDLE_ORCHESTRATOR_SLACK_USER_ID`. Defaults, validation, and semantics live in [`docs/design.md#configuration`](docs/design.md#configuration).

## Stack

Go. Single module, `cmd/` + `internal/` layout (matches tower + sense). Slack via `slack-go/slack`. MCP via the official `github.com/modelcontextprotocol/go-sdk`. SQLite via `modernc.org/sqlite` (pure Go, no CGO). Strict `golangci-lint v2` matching tower / orchestra / sense.

## Follow-ups

- [`docs/design.md`](docs/design.md) — architecture, handler pseudocode, schema, build sequencing.
- [`docs/features/polish-2026-05-18/`](docs/features/polish-2026-05-18/) — current polish phase tracking CI, coverage, README, and design-doc drift.
