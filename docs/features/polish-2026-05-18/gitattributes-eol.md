**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `gitattributes-eol` (id: `tsk_01KSKPM81X50TM6JS595X6SW9J`)

# Add `.gitattributes` enforcing LF for Go + YAML — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `.gitattributes` (new) | ~15 | 15 |
| Tests | — | 0 | 0 |
| Side effect | `git add --renormalize .` touches every file's line endings (no semantic content change) | autogen | — |
| **Total** | | | **~15** |

Band: **amazing** (sub-100 weighted).

## Goal

Stop the operator's Windows-local checkout from showing 36 spurious gofmt warnings on every `*.go` file by pinning blob storage to LF for source-text formats at the repo level.

## Behavior / fix

Add `.gitattributes` at repo root:

```
* text=auto eol=lf

*.go    text eol=lf
*.yml   text eol=lf
*.yaml  text eol=lf
*.toml  text eol=lf
*.md    text eol=lf
*.sql   text eol=lf
*.sh    text eol=lf

*.png   binary
*.jpg   binary
*.ico   binary
```

After committing the file, run `git add --renormalize .` to convert the working tree to LF. The renormalize commit is a separate commit (or amended into the same one) — it changes every file's line endings without altering semantic content.

## Acceptance

- `.gitattributes` exists at repo root.
- `file internal/config/config.go` reports `ASCII text` (no "with CRLF line terminators").
- `gofmt -l .` on Windows returns nothing (or only files with genuine formatting issues — not the 36-file CRLF noise).
- All existing tests still pass post-renormalize.

## Test plan

1. On Windows-local: `git checkout main && rm internal/config/config.go && git checkout -- internal/config/config.go && file internal/config/config.go` — reports LF.
2. `gofmt -l . | wc -l` ≤ 5 (down from 36).
3. `make check` green.

## Non-goals

- Changing `core.autocrlf` globally — that's a per-user git config decision; the `.gitattributes` is the repo-level fix.
- Adding a pre-commit hook running gofmt.
- Adding `.editorconfig` for editor-level enforcement.

## Notes for the implementer

This is **batch 1, solo, wide-blast** in the driver manifest. Shipping first means every later PR branches off the LF-normalized baseline; rebase pain is minimized.
