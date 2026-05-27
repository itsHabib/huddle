**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `ci-test-workflow` (id: `tsk_01KSKPFMXSP3R6C2N4X27YQJZC`)

# CI: add test workflow running `go test ./...` on PRs — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `.github/workflows/ci.yml` (new) | ~30 | 30 |
| Tests | — (CI itself IS the test surface) | 0 | 0 |
| **Total** | | | **~30** |

Band: **amazing**.

## Goal

Run the existing Go test suite on every PR + push to main. Today the only workflow in `.github/workflows/` is `claude.yml` (the @claude responder); broken tests can land silently because nothing exercises them at merge time.

## Behavior / fix

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go mod download
      - run: go test -race ./...
```

- `actions/setup-go@v5` caches the module cache automatically once `go-version-file: go.mod` is set.
- `-race` is on intentionally — modernc.org/sqlite + concurrent context cancellation in handler compensation paths (`archiveOrphanChannel`, `deleteOrphanHuddle`) are worth the race-detector.

## Acceptance

- `.github/workflows/ci.yml` exists with one `test` job.
- The PR shipping this task shows the workflow run green on GitHub.
- A deliberately broken test (revert a `require.NoError` to `require.Error` and re-push) shows the workflow going red.

## Test plan

1. Locally: `go test -race ./...` from the repo root. Should be green.
2. On PR: `gh pr checks <N>` shows `test / test (pull_request)` with status `pass`. < 3 min wall-clock.

## Non-goals

- Lint job — `ci-lint-workflow` task.
- govulncheck job — `ci-govulncheck-workflow` task.
- Coverage workflow — `ci-coverage-workflow` task.
- OS matrix (Windows/macOS) — defer unless a Windows-specific bug surfaces.

## Notes for the implementer

This is **batch 2**, parallel-safe with `ci-coverage-workflow`, `readme-rewrite`, `design-doc-drift`. The next two CI tasks (`ci-lint-workflow`, `ci-govulncheck-workflow`) layer onto the file you create — give them a clean `jobs:` block with the `test` job indented consistently so additional jobs slot in cleanly.
