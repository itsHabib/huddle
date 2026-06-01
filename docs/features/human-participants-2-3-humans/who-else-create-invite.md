**Status**: draft
**Owner**: @itsHabib
**Date**: 2026-05-31
**Related**: dossier tasks `who-else-returns-humans` (`tsk_01KT0B3G1Z825DKC70HZJR963R`, Phase 2) + `create-with-humans-and-invite-verb` (`tsk_01KT0B3VX7NF76SAWDXSWYP0YM`, Phase 3); parent design doc [`docs/features/human-participants/spec.md`](../human-participants/spec.md) (v3 — design lock landed in PR #21). Phase 1 (decoder + adapter plumbing) shipped in PR #23 (`8dfd8d7`).

# Phases 2 + 3 — Humans in the verbs (`who_else`, `create`, `invite_human`)

> **Source of truth.** The TDD v3 (`docs/features/human-participants/spec.md`) governs. Where this spec is silent, follow the TDD §6 / §7.1 / §7.2 / §7.3. Phases 2 and 3 are independent (both depend only on Phase 1) but both edit `internal/types/types.go`, so they ship together in this one PR to avoid a merge collision and to land in the operator's preferred PR-sizing band.

> **House style.** This repo enforces the `## Engineering principles` section in `CLAUDE.md` (Dave Cheney lineage) via golangci-lint (`gocognit`, `nestif`, `cyclop`, `revive`). Write to it: **no `else`** (early-return / line-of-sight), **≤2 nesting levels per scope** (extract a function when deeper), errors lowercase + wrapped with `%w`, small sharp APIs (accept the narrowest input). `make check` runs the linters — they will fail the build on violations.

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `internal/types/types.go`, `internal/handlers/who_else.go`, `internal/handlers/create.go`, `internal/handlers/humans.go` (new), `internal/handlers/invite.go` (new), `internal/server/server.go` | ~150 raw | ~150 |
| Tests | `internal/handlers/who_else_test.go`, `internal/handlers/create_test.go`, `internal/handlers/invite_test.go` (new) | ~140 raw | ~70 |
| Docs | `README.md`, `docs/design.md` | ~30 | ~10 |
| **Total** | | | **~230** |

Band: **ideal** (sub-700). Combined Phase 2 (~50) + Phase 3 (~110) plus the polish-phase calibration drift (~1.3–3× the TDD estimate).

## Goal

Surface humans through the operator-facing verbs, building entirely on the Phase-1 adapter surface (`BotUserID`, `ListChannelMembers`, `LookupUser`, `InviteUserToChannel`):

- **Phase 2:** `huddle.who_else` returns a `humans[]` list of the non-bot, non-orchestrator people in the huddle's Slack channel, with real display names. Read-side only; no DB write. Tokenless `who_else` keeps working (humans just empty).
- **Phase 3:** `huddle.create` accepts an optional `humans` list and invites them to the new channel; a new `huddle.invite_human` verb adds humans to an existing huddle. Both are best-effort — failures land in a `skipped[]` list, never error the verb.

No changes to the Phase-1-frozen `slack.Adapter` interface. No new persisted state (the Slack channel membership IS the human registry — TDD §1, §8).

---

## Part A — Phase 2: `who_else` returns humans

### A1. `internal/types/types.go` — extend `WhoElseResult`

Add a `Humans` field. It is **always present** (no `omitempty`) so clients can rely on the key; emit `[]types.Human{}` (empty slice, not `nil`) when there are none.

```go
type WhoElseResult struct {
	Purpose      string  `json:"purpose"`
	Orchestrator Seat    `json:"orchestrator"`
	Seats        []Seat  `json:"seats"`
	Humans       []Human `json:"humans"` // always present; [] when none
}
```

`types.Human` already exists (Phase 1) with the correct shape — do not redefine it.

### A2. `internal/handlers/who_else.go` — enumerate humans

After the existing seats assembly (which stays unchanged), populate `out.Humans`. The huddle's Slack channel ID is `hdl.SlackChannelID` (already fetched via `deps.Store.LookupHuddle`).

Flow (extract a helper like `listChannelHumans(ctx, deps, channelID) ([]types.Human, error)` to keep the handler's nesting ≤2):

1. `members, err := deps.Slack.ListChannelMembers(ctx, hdl.SlackChannelID)`.
   - **`errors.Is(err, slack.ErrNoToken)` → return `[]types.Human{}, nil`** (graceful degrade — preserves tokenless `who_else` from PR #19; humans simply unavailable without a token). The handler then returns the result with an empty `Humans`.
   - Any other error → propagate so the handler returns `huddleerr.MCPError(jsonrpc.CodeInternalError, err)`.
2. `botID := deps.Slack.BotUserID()`; `orchID := deps.Cfg.OrchestratorSlackUserID`.
3. For each `m` in `members`:
   - Skip if `m == botID`.
   - Skip if `orchID != "" && m == orchID` (the orchestrator is reported in the `Orchestrator` field, never double-counted under `humans`).
   - `info, err := deps.Slack.LookupUser(ctx, m)`; on error, **log (warn) + skip** this member (the read path never blocks/fails on a single lookup — TDD §D5).
   - Skip if `info.IsBot` or `info.Deactivated`.
   - Else append `types.Human{SlackUserID: info.UserID, DisplayName: info.DisplayName, Kind: types.IdentityKindHuman}`.
4. Return the accumulated slice (initialize as `make([]types.Human, 0)` so the empty case marshals to `[]`).

Wire into the handler: assemble `out` as today, set `out.Humans = humans`, return.

> Note on display name: `info.DisplayName` is already resolved by Phase 1's `LookupUser` (prefers `profile.display_name`, falls back to `real_name`). If it is empty, still include the human (empty display name is acceptable v1 — TDD OQ2); do not synthesize `user-<id>` here (that synthetic is read-path-only).

### A3. Phase 2 tests — `internal/handlers/who_else_test.go`

Use the handler test harness (`newToolSession`, `store.OpenMemory`) and drive Slack via `slack.FakeAdapter` (set `deps.Slack = fake`). FakeAdapter fields: `BotUserIDValue`, `ChannelMembers map[string][]string`, `UsersByRef map[string]types.UserInfo`, `ListChannelMembersErr`, `LookupUserErr`. Seed the huddle so its `SlackChannelID` matches the `ChannelMembers` key.

Cases:
- **No humans**: channel members = `[bot]` only → `humans: []`.
- **One human**: members = `[bot, U_human]`, `UsersByRef[U_human] = {DisplayName:"Joe Smith"}` → one human with real name.
- **Mixed bots**: members include a second bot (`UserInfo.IsBot=true`) → bot dropped.
- **Deactivated dropped**: `UserInfo.Deactivated=true` → dropped.
- **Orchestrator not double-counted**: set `deps.Cfg.OrchestratorSlackUserID` to a member ID → that member absent from `humans`.
- **List error → CodeInternalError**: `ListChannelMembersErr = ErrRateLimited` (or any non-`ErrNoToken`) → handler returns `CodeInternalError`.
- **Tokenless → humans: []**: `ListChannelMembersErr = slack.ErrNoToken` → result returns with `humans: []` and no error (seats still present).
- **Lookup error skips member**: `LookupUserErr` for one member → that member skipped, others still listed (if the fake supports per-ref errors; otherwise drive via a member whose ref is absent from `UsersByRef` → `ErrUserNotFound` → skipped).

---

## Part B — Phase 3: create-with-humans + `huddle.invite_human`

### B1. `internal/types/types.go` — new fields + types

```go
// CreateArgs gains:
type CreateArgs struct {
	// ... existing fields ...
	Humans []string `json:"humans,omitempty"` // Slack user IDs or emails (TDD §D3)
}

// CreateResult gains:
type CreateResult struct {
	// ... existing fields ...
	Humans  []Human        `json:"humans"`                 // always present; [] when none
	Skipped []SkippedHuman `json:"skippedHumans,omitempty"`
}

// New verb arg/result:
type InviteHumanArgs struct {
	HuddleID string   `json:"huddleId"`
	Humans   []string `json:"humans"`
}

type InviteHumanResult struct {
	Invited []Human        `json:"invited"`           // always present; [] when none
	Skipped []SkippedHuman `json:"skipped,omitempty"`
}

// Skip record + reasons:
type SkippedHuman struct {
	Ref    string        `json:"ref"`
	Reason SkippedReason `json:"reason"`
}

type SkippedReason string

const (
	SkippedReasonAlreadyInChannel  SkippedReason = "already_in_channel"
	SkippedReasonUnknownUser       SkippedReason = "unknown_user"
	SkippedReasonInvalidRef        SkippedReason = "invalid_ref"
	SkippedReasonMissingEmailScope SkippedReason = "missing_email_scope"
	SkippedReasonInviteFailed      SkippedReason = "invite_failed"
)
```

### B2. `internal/handlers/humans.go` (new) — shared resolve-and-invite helper

One small, single-responsibility function used by both `create` and `invite_human`. Accept the narrowest input (the adapter + logger, not the whole `Deps`):

```go
// resolveAndInviteHumans resolves each ref to a Slack user and invites them to
// channelID. Best-effort: every ref yields either an Invited human or a Skipped
// record; the function never returns an error. invited/skipped are non-nil.
func resolveAndInviteHumans(
	ctx context.Context,
	adapter slack.Adapter,
	log *slog.Logger,
	channelID string,
	refs []string,
) (invited []types.Human, skipped []types.SkippedHuman)
```

Implementation:
1. `invited = make([]types.Human, 0)`, `skipped = make([]types.SkippedHuman, 0)`.
2. **Membership pre-check (already-in-channel labeling).** `members, merr := adapter.ListChannelMembers(ctx, channelID)`. Build a `map[string]struct{}` set. If `merr != nil`, log (warn) and use an empty set — degrade gracefully (we may then label an existing member as `invited` via the idempotent invite below rather than `already_in_channel`; acceptable v1).
3. For each `ref`:
   - `info, err := adapter.LookupUser(ctx, ref)`. On error map to a skip and `continue`:
     - `errors.Is(err, slack.ErrInvalidUserRef)` → `SkippedReasonInvalidRef`
     - `errors.Is(err, slack.ErrUserNotFound)` → `SkippedReasonUnknownUser`
     - `errors.Is(err, slack.ErrMissingEmailScope)` → `SkippedReasonMissingEmailScope`
     - any other error → `SkippedReasonInviteFailed` + warn log
   - If `info.UserID` is in the members set → append `SkippedHuman{Ref: ref, Reason: SkippedReasonAlreadyInChannel}`, `continue`.
   - `if err := adapter.InviteUserToChannel(ctx, channelID, info.UserID); err != nil` → append `SkippedHuman{Ref: ref, Reason: SkippedReasonInviteFailed}` + warn, `continue`. (`InviteUserToChannel` already treats `already_in_channel` as a nil success, so a race that slipped past the pre-check still converges — it just lands as `invited`.)
   - Else append `types.Human{SlackUserID: info.UserID, DisplayName: info.DisplayName, Kind: types.IdentityKindHuman}` to `invited`.
4. Return.

> **Reviewer confirm-point:** this reconciles the TDD's "`already_in_channel` → Skipped" with the existing `InviteUserToChannel` idempotency (which swallows `already_in_channel` as success) via the pre-check, rather than changing the frozen adapter interface or the orchestrator-invite path. Flag in the PR body for review against TDD §7.

Keep per-ref handling in its own helper if the loop body would otherwise exceed 2 nesting levels (e.g. `classifyLookupErr(err) SkippedReason`).

### B3. `internal/handlers/create.go` — invite humans post-commit

After seat keys are committed successfully (the existing happy path, *after* all compensation-guarded steps — human invites have **no compensation path**, TDD §7.1), and before building the final result:

```go
humans, skipped := resolveAndInviteHumans(ctx, deps.Slack, deps.Log, ch.ID, args.Humans)
```

Set `result.Humans = humans` and `result.Skipped = skipped`. When `args.Humans` is empty, `resolveAndInviteHumans` returns empty slices — `result.Humans` marshals to `[]`, `result.Skipped` is omitted (`omitempty`). Do **not** add a compensation path for invites; a failed invite is a `Skipped` entry, the create still succeeds.

### B4. `internal/handlers/invite.go` (new) — `huddle.invite_human`

```go
// RegisterInviteHuman registers the huddle.invite_human tool.
func RegisterInviteHuman(s *mcp.Server, deps Deps)
```

Handler (`func(ctx, _, args types.InviteHumanArgs) (*mcp.CallToolResult, types.InviteHumanResult, error)`):
1. Validate: `strings.TrimSpace(args.HuddleID) == ""` → `CodeInvalidParams` ("huddleId is required"). `len(args.Humans) == 0` → `CodeInvalidParams` ("at least one human ref is required").
2. `hdl, err := deps.Store.LookupHuddle(ctx, args.HuddleID)`. Not-found (`huddleerr.ErrHuddleNotFound` or whatever `LookupHuddle` returns for a missing row — match the pattern used by `close.go` / `post.go`) → `CodeInvalidParams`. Other store error → `CodeInternalError`.
3. `invited, skipped := resolveAndInviteHumans(ctx, deps.Slack, deps.Log, hdl.SlackChannelID, args.Humans)`.
4. Return `types.InviteHumanResult{Invited: invited, Skipped: skipped}`. No DB write.

Tool description: `"Invite one or more humans (Slack user IDs or emails) to an existing huddle's channel. Best-effort: unresolvable or un-invitable refs are returned under skipped."`

### B5. `internal/server/server.go` — register the verb

Add to `RegisterVerbStubs`, alongside the existing `handlers.Register*` calls:

```go
handlers.RegisterInviteHuman(s, hdep)
```

### B6. Docs — `README.md` + `docs/design.md`

- **Verb table** (both files where verbs are listed): add `huddle.invite_human` with its args/result; note `huddle.create` now accepts an optional `humans` array and `huddle.who_else` now returns `humans`.
- **Slack OAuth scopes**: document that **`users:read.email` is a NEW required scope** for resolving *email* human refs (used by `users.lookupByEmail` in Phase 1's `LookupUser`). Existing `channels:read` + `users:read` cover user-ID refs and channel membership. Without `users:read.email`, email refs return `skipped{reason: missing_email_scope}` — user-ID refs are unaffected.
- Keep edits tight; match the existing table/section style. Do not restructure the docs.

### B7. Phase 3 tests

**`internal/handlers/create_test.go`** (extend):
- **Create with humans, happy**: `args.Humans = ["U_alice"]`, FakeAdapter `UsersByRef[U_alice]={DisplayName:"Alice"}`, `ChannelMembers[ch]` excludes Alice → `result.Humans` has Alice, `fake.Invites` records the invite, `result.Skipped` empty.
- **Create with partial skip**: `["U_alice", "nope"]` where `nope` resolves to `ErrUserNotFound` → Alice invited, `Skipped[{Ref:"nope", Reason:unknown_user}]`.
- **Create with no humans**: existing happy-path tests still pass; `result.Humans == []` (assert non-nil empty).

**`internal/handlers/invite_test.go`** (new):
- **Happy**: existing huddle (seed store), `["U_bob"]` resolvable, not a member → `Invited:[Bob]`, invite recorded.
- **Missing huddle**: unknown `huddleId` → `CodeInvalidParams`.
- **Empty humans**: `Humans: []` → `CodeInvalidParams`.
- **Already in channel**: `ChannelMembers[ch]` already contains the resolved user ID → `Skipped[{already_in_channel}]`, no invite recorded.
- **Invite failed**: `fake.InviteErr` set → `Skipped[{invite_failed}]`.
- **Missing email scope**: ref is an email, `LookupUserErr = slack.ErrMissingEmailScope` (or `UsersByRef` miss that the fake maps to that error) → `Skipped[{missing_email_scope}]`.
- **Tokenless**: `LookupUserErr = slack.ErrNoToken` (or `ListChannelMembersErr`) → all refs skipped with a reasonable reason (the helper maps a non-classified error to `invite_failed`); the verb still returns 200 with everything under `skipped`. (Confirm behavior is sensible; if the TDD wants a distinct tokenless reason, add it.)

> If `FakeAdapter` cannot express "this ref errors with X", extend the fake minimally (e.g. a `LookupUserErrByRef map[string]error`) — keep additions small and consistent with the Phase-1 fake style. Add `var _ slack.Adapter = (*FakeAdapter)(nil)` stays satisfied.

---

## Acceptance

- `make check` green (vet + `go tool golangci-lint run` + `go test ./...` + build) — including the Cheney linters (`gocognit`/`nestif`/`cyclop`/`revive`).
- `WhoElseResult.Humans` present and `[]`-when-empty; `who_else` lists non-bot, non-orchestrator, non-deactivated channel members with real display names; tokenless → `humans: []`; non-`ErrNoToken` Slack error → `CodeInternalError`.
- `CreateArgs.Humans` accepted; `CreateResult.Humans` + `skippedHumans` populated; create still succeeds (no compensation) when an invite fails.
- `huddle.invite_human` registered and reachable; validates huddleId + non-empty humans; missing huddle → `CodeInvalidParams`; returns `{invited, skipped}` with every ref accounted for; no DB write.
- Every `SkippedReason` value is produced by at least one test.
- `slack.Adapter` interface unchanged; no new persisted state; `internal/config` not imported by the slack package.
- README + `docs/design.md` document the new verb, the `create.humans` arg, the `who_else.humans` field, and the new `users:read.email` scope.

## Out of scope

- `huddle.remove_human` / `conversations.kick` (TDD §D6 — operator does this in Slack).
- Pagination of `ListChannelMembers` beyond a single page (Phase-1 TODO; not needed at the < 10-humans NFR).
- Private-channel invite semantics (`not_in_channel`) — public channels are the v0 default (TDD OQ1).
- Persisting human participants / audit log (TDD §1, §8 — explicit non-goal; Slack channel is the registry).
- Any change to the Phase-1 `slack.Adapter` interface, the decoder, or `LookupUser`/cache behavior.
- Retry loops on Slack 5xx (TDD OQ3 — log + skip for v1).
