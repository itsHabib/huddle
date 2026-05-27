**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `ci-lint-workflow` (id: `tsk_01KSKPG4CG534QG76YVKM41WKJ`)

# CI: add lint job running `golangci-lint run` — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `.github/workflows/ci.yml` (modify; add `lint` job) | ~15 | 15 |
| Tests | — | 0 | 0 |
| **Total** | | | **~15** |

Band: **amazing**.

## Goal

Run the existing strict `golangci-lint` v2 config (`.golangci.yml`, 2741 bytes, ~30 linters) on every PR. The config is portfolio-matching and aggressive; style drift can land silently without a CI step running it.

## Behavior / fix

Add a `lint` job alongside `test` in `.github/workflows/ci.yml`:

```yaml
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest         # confirm v2 schema support; pin to v2-compatible release if needed
          args: --timeout=5m
```

Same triggers as the `test` job (`push` to main + `pull_request`).

**Action-version note for the implementer**: `.golangci.yml` declares `version: "2"` (v2 schema). The `golangci-lint-action` only supports the v2 schema with a sufficiently new `golangci-lint` binary. If `version: latest` doesn't work, pin to an explicit v2-compatible release (e.g. `v1.62.0`+ or whichever is current at impl time) — surface this in the PR description so the operator can adjust if a newer release is available.

## Acceptance

- `.github/workflows/ci.yml` contains a `lint` job alongside `test`.
- Lint passes green on the PR shipping this task.
- Introducing a deliberate lint violation (e.g., unused variable) shows the job going red.

## Test plan

1. Locally: `make lint` from the repo root. Should be clean (the user has been running this locally).
2. On PR: `gh pr checks <N>` shows both `test` and `lint` passing.

## Non-goals

- Tweaking `.golangci.yml` — the config is opinionated to portfolio standard; don't soften it.
- Adding lint-staged / pre-commit hooks — separate concern.

## Notes for the implementer

This is **batch 3**, depends on `ci-test-workflow` (`tsk_01KSKPFMXSP3R6C2N4X27YQJZC`). Rebase on `origin/main` after `ci-test-workflow` merges — the file you're editing didn't exist before that PR.
