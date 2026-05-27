**Status**: draft
**Owner**: @michael
**Date**: 2026-05-18
**Related**: dossier task `ci-govulncheck-workflow` (id: `tsk_01KSKPGJBS238NFCHXMA9WFBXA`)

# CI: add govulncheck job — design spec

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `.github/workflows/ci.yml` (modify; add `govulncheck` job) | ~15 | 15 |
| Tests | — | 0 | 0 |
| **Total** | | | **~15** |

Band: **amazing**.

## Goal

Catch CVE-style advisories against the module graph (`slack-go/slack`, `modelcontextprotocol/go-sdk`, `modernc.org/sqlite`, `google/uuid`, stdlib) before they sit silently.

## Behavior / fix

Add a `govulncheck` job to `.github/workflows/ci.yml`:

```yaml
  govulncheck:
    runs-on: ubuntu-latest
    continue-on-error: true       # non-blocking initially; flip to blocking after 1-2 green runs
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go install golang.org/x/vuln/cmd/govulncheck@latest
      - run: govulncheck ./...
```

`continue-on-error: true` initially — the goal is to surface advisories without the first run gating merges. Once the inventory is clean, a follow-up PR removes that line.

## Acceptance

- `.github/workflows/ci.yml` contains a `govulncheck` job.
- Job runs on PR. Reports advisories if any, otherwise green.
- After 1-2 green runs, the `continue-on-error: true` line gets removed in a follow-up PR.

## Test plan

1. Locally: `go install golang.org/x/vuln/cmd/govulncheck@latest && govulncheck ./...`. Should report zero advisories at impl time (verify before merging).
2. On PR: workflow lights up; check the job log for the govulncheck output.

## Non-goals

- `gosec` static analysis.
- Automating dep updates on advisory.
- Removing `continue-on-error: true` in this same PR — separate follow-up after the inventory's clean.

## Notes for the implementer

This is **batch 4**, depends on `ci-lint-workflow` (`tsk_01KSKPG4CG534QG76YVKM41WKJ`) due to textual file overlap in `.github/workflows/ci.yml`. Rebase on `origin/main` after `ci-lint-workflow` merges; conflict will be limited to the new job's location in the `jobs:` block.
