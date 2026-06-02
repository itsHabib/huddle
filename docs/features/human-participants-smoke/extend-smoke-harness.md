**Status**: draft
**Owner**: @itsHabib
**Date**: 2026-06-02
**Related**: dossier task `smoke-cover-human-verbs` (`tsk_01KT37T3X5JAFZ9D433CKM77J4`); validates the human-participants feature shipped in PR #23 (decoder) + PR #26 (who_else/create/invite_human).

# Extend `cmd/smoke` to cover the human-participant verbs

> **House style.** Cheney lineage, enforced by golangci-lint (`gocognit`, `nestif`, `cyclop`, `revive`): no `else` (early-return / line-of-sight), ≤2 nesting levels per scope (extract a helper when deeper), errors lowercase + wrapped (`%w`), small sharp helpers. `make check` runs the linters and must stay green — `cmd/smoke` is compiled + vetted + linted even though it is not part of `go test`.

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `cmd/smoke/main.go` | ~80 raw | ~80 |
| Docs | `cmd/smoke` package doc / `README.md` (1-line on the new env, optional) | ~5 | 0 |
| **Total** | | | **~80** |

Single file. Manual harness only — **not** added to `go test` or CI.

## Goal

`cmd/smoke/main.go` currently drives the original v0 tour: `create` → `who_else` (per seat) → `post`×3 → `read` → `close`, archiving the channel on any failure. It does **not** exercise the human-participant surface (Phases 2–3): `create.humans`, `who_else.humans`, or `huddle.invite_human`. Extend the harness so a single `go run ./cmd/smoke` dogfoods the whole feature against a real Slack workspace — while degrading gracefully when no real human is available to invite.

## Behavior

### 1. New optional env: `HUDDLE_SMOKE_HUMAN_REF`

A Slack user ID (`U…`) or email to drive the human path. Mirror the existing `HUDDLE_ORCHESTRATOR_SLACK_USER_ID` treatment in `run()`:

- When **unset**: `fmt.Fprintln(os.Stderr, "WARN: HUDDLE_SMOKE_HUMAN_REF is not set; skipping create-with-humans / invite_human steps. Export a Slack user ID or email to exercise the human-participant surface.")` and run the existing tour unchanged (plus the always-on `who_else.humans` presence check in step 3).
- When **set**: thread the ref through the create + invite steps below.

Read it once in `run()` (or `runScenario`) and pass it down — don't call `os.Getenv` in multiple helpers. Trim it with `strings.TrimSpace`.

### 2. `create` with humans (when ref set)

In `smokeCreate`, when the ref is non-empty, add `"humans": []string{ref}` to the `huddle.create` arguments. Plumb the ref into `smokeCreate` as a parameter (`func smokeCreate(ctx, sess, humanRef string)`). The existing seats/orchestrator/purpose stay unchanged.

After the call, `dump(createRes)` already prints the full result (which now includes `humans` + `skipped`). Add a light assertion that the `humans` key is present and is a list (it is non-`omitempty` in `CreateResult`):

```go
if _, ok := createRes["humans"].([]any); !ok {
    return nil, fmt.Errorf("create result missing humans array: %+v", createRes)
}
```

Do **not** hard-assert that the ref landed in `humans` vs `skipped` — an email ref without the `users:read.email` scope correctly lands in `skipped{missing_email_scope}`, and that is a valid, observable outcome, not a smoke failure.

### 3. `who_else.humans` presence (always on)

In `smokeWhoElse`, after the existing orchestrator-id check, assert the `humans` key is present and is a list for each seat's view, and `dump` it (the existing `dump(res)` already prints it; just add the presence check):

```go
if _, ok := res["humans"].([]any); !ok {
    return fmt.Errorf("who_else(%s) missing humans array: %+v", s.ID, res)
}
```

This runs regardless of the ref env, so even a no-ref smoke run verifies the new `who_else` wire shape.

### 4. New `smokeInviteHuman` step (when ref set)

Add a step after `smokeRead` (and before `smokeClose`), gated on the ref being set. Call `huddle.invite_human` with the same ref on the created huddle:

```go
func smokeInviteHuman(ctx context.Context, sess *mcp.ClientSession, huddleID, humanRef string) error {
    step("huddle.invite_human (" + humanRef + ")")
    res, err := callJSON(ctx, sess, "huddle.invite_human", map[string]any{
        "huddleId": huddleID,
        "humans":   []string{humanRef},
    })
    if err != nil {
        return fmt.Errorf("invite_human: %w", err)
    }
    dump(res)
    return nil
}
```

**Best-effort semantics:** the verb returns `{invited, skipped}` and never errors on an un-invitable ref. If the ref was already invited at create (step 2), this call returns it under `skipped{already_in_channel}` — a successful demonstration of the dedupe path, not a failure. Only a transport/RPC error (the `err` above) fails the run. Do not assert on the `invited` vs `skipped` split.

Wire it into `runScenario`:
- Thread `humanRef` into `runScenario` (read once in `run()`, pass to `runScenario`).
- After `smokeRead`, `if humanRef != "" { if err := smokeInviteHuman(ctx, sess, huddleID, humanRef); err != nil { return err } }`.

### 5. Keep cleanup intact

The deferred `huddle.close` (channel archive on any failure) and the `closeOnDefer` flag are unchanged. The new step sits inside the same defer's protection.

## Acceptance

- `make check` green (vet + `go tool golangci-lint run` + `go test ./...` + build) — `cmd/smoke` compiles, vets, and lints clean.
- `go run ./cmd/smoke` **without** `HUDDLE_SMOKE_HUMAN_REF`: runs the full original tour, WARNs that the human steps are skipped, and still asserts `who_else.humans` is present.
- `go run ./cmd/smoke` **with** `HUDDLE_SMOKE_HUMAN_REF` set: additionally passes `humans` to create, dumps `create.humans`/`skipped`, and runs `invite_human` (dumping `invited`/`skipped`).
- A `skipped` human (e.g. `missing_email_scope` on an email ref without the scope) does **not** fail the run — only an RPC/transport error does.
- Each new/edited helper stays within the cognitive-complexity gate (extract a helper if a function would exceed it).

## Out of scope

- Adding smoke to `go test` / CI — it stays a manual harness (needs a real Slack token + workspace).
- Asserting specific Slack-side state (that the invited user actually appears in `who_else.humans` on a second poll) — Slack membership propagation timing makes that flaky; dumping the results is sufficient for a manual eyeball.
- Any change to the verbs themselves or the `slack.Adapter` interface — this is harness-only.
- The `paginate-list-channel-members` follow-up (separate parked task).
