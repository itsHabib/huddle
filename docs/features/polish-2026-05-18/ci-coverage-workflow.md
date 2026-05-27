**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `ci-coverage-workflow` (id: `tsk_01KSKPH36G7NJ10DFB94EZ3G53`)

# CI: add coverage workflow_dispatch — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `.github/workflows/coverage.yml` (new) | ~40 | 40 |
| Tests | — | 0 | 0 |
| **Total** | | | **~40** |

Band: **amazing**.

## Goal

Make `go test -coverprofile` easy to run on-demand and produce a downloadable HTML report + a job-summary line with the total %. Not a merge gate — coverage threshold games aren't worth the candle for a personal repo. Just visibility.

## Behavior / fix

Create `.github/workflows/coverage.yml`:

```yaml
name: Coverage

on:
  workflow_dispatch:

jobs:
  coverage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go test -coverprofile=coverage.out -covermode=atomic ./...
      - run: go tool cover -html=coverage.out -o coverage.html
      - run: |
          echo "## Coverage" >> $GITHUB_STEP_SUMMARY
          go tool cover -func=coverage.out | tail -n 1 >> $GITHUB_STEP_SUMMARY
      - uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: |
            coverage.out
            coverage.html
```

`workflow_dispatch` only — does NOT trigger on PR. The operator runs it deliberately when they want to measure gaps (e.g. before deciding whether to invest in compensation-path failure-injection tests).

## Acceptance

- `.github/workflows/coverage.yml` exists.
- `gh workflow run coverage.yml` produces a job-summary line like `total: (statements) 78.4%`.
- The artifact bundle contains `coverage.out` + `coverage.html`; the HTML renders source-line annotations when opened locally.
- Workflow does NOT trigger on PR or push (verified by checking the Actions tab after the PR merges).

## Test plan

1. After merge: `gh workflow run coverage.yml` from a checkout of `main`.
2. `gh run download <id>` → unzip → open `coverage.html` → confirm rendering.
3. Job summary on the Actions tab shows the total percentage.

## Non-goals

- Coverage-threshold gate. Explicitly not doing this.
- Codecov / Coveralls integration.
- Branch-level / function-level annotations beyond what `go tool cover` produces.

## Notes for the implementer

This is **batch 2**, parallel-safe with the other batch-2 streams. Separate file from `ci.yml` so no conflict with the test/lint/govulncheck PRs.
