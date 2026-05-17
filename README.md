# huddle

Operator-staged Slack-backed coordination rooms for multi-agent sessions.

The operator creates a huddle, declares seats (each with its own key = identity), and hands out the keys to agents. Agents in the huddle post + read messages; the wrapper auto-attributes every post by seat. The operator is the implicit orchestrator — distinct identity, visible to every agent in the room.

## Status

Design phase. No code yet.

- [`docs/design.md`](docs/design.md) — implementation plan (stack, layout, deps, verb handlers, schema, sequencing, DoD).
- [`../mcp-workstation/huddle.md`](../mcp-workstation/huddle.md) — catalog entry (problem, MCP surface, risks, open questions).

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

## Stack

Go. Single module, `cmd/` + `internal/` layout (matches tower + sense). Slack via `slack-go/slack`. MCP via the official `github.com/modelcontextprotocol/go-sdk`. SQLite via `modernc.org/sqlite` (pure Go, no CGO). Strict `golangci-lint v2` matching tower / orchestra / sense.
