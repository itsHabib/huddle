**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `design-doc-drift` (id: `tsk_01KSKPKM9T87JKASXEZ3PEVNNY`)

# docs/design.md: fix TS-era residue + reconcile e2e plan with `cmd/smoke` — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `docs/design.md` (5 small edits) | ~15 | 15 |
| Maybe | `internal/config/config.go` (delete `HUDDLE_SLACK_WORKSPACE` if pruning) | ~5 | 5 |
| Tests | — | 0 | 0 |
| **Total** | | | **~20** |

Band: **amazing**.

## Goal

Reconcile `docs/design.md` with shipped reality. It was authored TS-first and refit for Go; several leftover references (`src/config.ts`, `zod`, the `test/e2e/dogfood_test.go` path) and a stale `Status: design ... No code yet` line need cleanup.

## Behavior / fix

Five targeted edits in `docs/design.md`:

1. **Line 3** — replace `Status: design, 2026-05-17. No code yet.` with `Status: shipped v0 — 2026-05-17 design → 2026-05-18 last-major-change. Tracks v1 from here.`

2. **Line 367** — replace `Env vars loaded by \`src/config.ts\`:` with `Env vars loaded by \`internal/config/config.go\`:`.

3. **Line 378** — replace `` `config.ts` validates at startup `` with `` `internal/config/config.go` validates at startup ``.

4. **Line 382** — replace `**Input validation errors** (zod):` with `**Input validation errors** (handwritten per-verb validators in \`internal/handlers/\`):`.

5. **Lines 75-77 (layout sketch)** — remove the `test/e2e/dogfood_test.go` entry. Add under `cmd/`:
   ```
   ├── smoke/
   │   └── main.go              # MCP-client e2e harness driving the huddle binary as a subprocess (gated by HUDDLE_SLACK_BOT_TOKEN; manual runs, not CI)
   ```
   Also add a short paragraph after the layout sketch:
   > **e2e harness lives in `cmd/smoke/`, not `test/e2e/`.** The v0 build chose a CLI smoke harness over `go test`-gated e2e for better extensibility (smoke can be hand-invoked, replayed against a fresh huddle, scripted from outside Go). Design originally proposed `test/e2e/dogfood_test.go`; impl shipped `cmd/smoke/main.go` instead.

**Plus the `HUDDLE_SLACK_WORKSPACE` decision**: the env table at line 372 lists this variable, and `internal/config/config.go` loads it into `Config.SlackWorkspace`. But nothing reads `SlackWorkspace` anywhere in the codebase. Pick one option in this PR (no out-of-scope work):

- **Option A — Prune.** Delete the row from the env table (line 372). Delete the field from `Config` struct. Delete the `os.Getenv` call. Remove the constant. Run `go build ./...` to confirm clean.
- **Option B — Wire it.** Add it to the slog logger's default context (e.g. `slog.SetDefault(slog.With("workspace", cfg.SlackWorkspace))` in `cmd/huddle/main.go`), so it shows up in every log line. Update the env-table notes to reflect the actual use.

Recommendation: **Option A** (prune). The field has been dead since v0 shipped; if/when it's needed, it can be re-added with a real use site.

## Acceptance

- `grep -n "config\.ts\|src/config\|zod" docs/design.md` returns nothing.
- `grep -n "No code yet" docs/design.md` returns nothing.
- The layout sketch references `cmd/smoke/main.go`, not `test/e2e/dogfood_test.go`.
- The e2e-harness paragraph exists explaining the choice.
- `HUDDLE_SLACK_WORKSPACE` is either gone from the env table AND from `internal/config/config.go`, or has a real use site in code.
- `go build ./...` green.
- `make check` green.

## Test plan

1. `grep -rn "config\.ts\|zod" docs/` — empty.
2. `grep -n "test/e2e/dogfood_test.go" docs/design.md` — empty.
3. Render the doc on GitHub — layout sketch matches the actual `cmd/` + `internal/` tree.
4. If Option A: `grep -rn "HUDDLE_SLACK_WORKSPACE\|SlackWorkspace" .` returns nothing (or only the historical commit messages, no live code).

## Non-goals

- Re-architecting env handling.
- Adding new env vars.
- Rewriting any other doc — README drift is `readme-rewrite`'s job.

## Notes for the implementer

This is **batch 2**, parallel-safe with the other batch-2 streams. Touches `docs/design.md` and possibly `internal/config/config.go`; no other in-flight task touches either file.

If you pick Option B (wire the workspace), make sure the field is set BEFORE `slog.New(...)` is called in `run()` — otherwise the first log lines after config-load won't have the context.
