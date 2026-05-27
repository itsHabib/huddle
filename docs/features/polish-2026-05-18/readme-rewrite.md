**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `readme-rewrite` (id: `tsk_01KSKPJ8R62HS13CNDPDXQDNTQ`)

# README: replace stale "Design phase. No code yet." with v0-shipped walkthrough — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `README.md` (rewrite, ~60-80 LOC new) | ~60 | 60 |
| Tests | — | 0 | 0 |
| **Total** | | | **~60** |

Band: **amazing**.

## Goal

The README's first non-tagline section reads `## Status` → `Design phase. No code yet.` That's been wrong since 2026-05-17. v0 has shipped (5 merged PRs, 6 MCP verbs, 4 CLI binaries). Rewrite the README so a newcomer can install, run, and understand the layout in under 2 minutes.

## Behavior / fix

Rewrite `README.md` with these sections (mirror voice of sibling pers/ repos):

1. **Header + tagline** — keep existing lines 1-5; they're tight.
2. **Status badges row** — CI badge (links to `ci.yml`; renders "no status" until `ci-test-workflow` ships, then green), license badge (LICENSE exists in repo root, currently unbadged), Go version badge.
3. **Status section** — replace `Design phase. No code yet.` with `v0 shipped 2026-05 — six MCP verbs + four CLI binaries. Tracking polish-2026-05-18 follow-ups.`
4. **Install** — `go install github.com/itsHabib/huddle/cmd/huddle@latest` (or clone + `make install`).
5. **Quickstart** — minimal env-var-and-go-run snippet + a sentence on registering as an MCP server in Claude Code / Claude Desktop with a pointer to the relevant config example.
6. **Verb surface** — keep existing table (lines 14-25).
7. **CLI binaries** — short table:
   - `huddle` — MCP server (stdio transport)
   - `seat` — seat-side CLI wrapper for post / read / who_else
   - `smoke` — end-to-end harness driving the huddle binary as a subprocess against real Slack (manual, not CI)
   - `seed-huddle` — one-shot long-lived huddle generator
8. **Configuration** — link to `docs/design.md#configuration` (don't duplicate the env-var table).
9. **Stack** — keep existing line (the current `## Stack` paragraph is fine).
10. **Status / follow-ups** — link to `docs/design.md` and the dossier project (`pers/dossier-state/projects/huddle/`). Skip `docs/follow-ups.md` — that task was cancelled; if a follow-ups doc shows up later, add the link then.

Drop the stale link to `../mcp-workstation/huddle.md` from the existing Status section — catalog reference belongs in a footer if anywhere, not as a top-of-file bullet.

## Acceptance

- `README.md` no longer contains the literal string `No code yet`.
- README mentions every binary under `cmd/`.
- README mentions every env var the binary reads.
- CI badge renders (linked to `ci.yml`; may show "no status" if the workflow hasn't been added yet — acceptable, it'll go green once `ci-test-workflow` lands).
- License badge renders.
- `wc -l README.md` ≥ 60.

## Test plan

1. `gh repo view --web` on the merged PR — every section reads cleanly.
2. Copy-paste the install one-liner into a fresh shell — succeeds.
3. Copy-paste the quickstart — server starts (or fails with a clear "missing env var" message if the token isn't set).

## Non-goals

- Architecture diagrams — if needed, those live in `docs/design.md`.
- Tutorial-style "your first huddle" walkthrough — defer to a separate `docs/tutorial.md` if/when there's demand.

## Notes for the implementer

This is **batch 2**, parallel-safe with the other batch-2 streams. No file overlap.

The CI badge can link to `ci.yml` even if that workflow file doesn't exist yet (it'll show "no status" until `ci-test-workflow` ships). That's preferable to deferring the README update until CI lands.
