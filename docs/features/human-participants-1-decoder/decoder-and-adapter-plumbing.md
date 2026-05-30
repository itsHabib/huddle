**Status**: draft
**Owner**: @itsHabib
**Date**: 2026-05-30
**Related**: dossier task `decoder-and-adapter-plumbing` (id: `tsk_01KSVJ7S95M2ZXXJ9860JTY0QE`); parent design doc [`docs/features/human-participants/spec.md`](../human-participants/spec.md) (v3 — design lock landed in PR #21 @ commit `030548c`).

# Phase 1 — Decoder + Adapter plumbing for human identities

> **Source of truth.** The dossier task body for this task was written before the TDD's v2 + v3 review cycles. Where this spec disagrees with the dossier task body — `cfg.OrchestratorSlackUserID` reference, no `singleflight`, no D7 bot user ID source, `@handle` support, etc. — the TDD (v3) is correct. See `docs/features/human-participants/spec.md` §D7, §7.4, §7.5, §6.

## Scope

| Bucket | Files | Est. LOC | Weighted |
|---|---|---|---|
| Production source | `internal/types/types.go`, `internal/slack/iface.go`, `internal/slack/impl.go`, `internal/slack/fake_adapter.go`, `internal/slack/messages.go` | ~120 raw | ~120 |
| Tests | `internal/slack/impl_test.go`, `internal/slack/messages_test.go` (or extend existing), `internal/slack/encoding_test.go` (light — `Decode` itself doesn't change) | ~100 raw | ~50 |
| **Total** | | | **~170** |

Band: **amazing** (sub-500). Per the TDD's §9 sizing this is the intended Phase 1 footprint (`~140` was the TDD estimate; actuals routinely drift up to ~3× per the polish phase calibration, so plan for closer to ~170).

This phase ships solo as the first PR. Phases 2 (`who_else` returns humans) and 3 (create-with-humans + `invite_human` verb) both depend on the adapter methods landing here; they can't combine into one PR. Per `feedback_pr_sizing_bigger.md` larger PRs are preferred, but Phase 1 is constrained by the dep edge — it stays standalone.

## Goal

Make the read path emit `Identity{Kind: human, DisplayName: <real profile name>}` for posts from non-bot Slack users, instead of today's `{kind: human, DisplayName: "user-<rawUserID>"}` synthetic. Establish the adapter surface — `LookupUser`, `ListChannelMembers`, `BotUserID` — that Phase 2's `who_else` and Phase 3's `create + invite_human` will consume. **No operator-facing verb changes in this PR.** The existing wire shape of `huddle.read` is preserved: same `kind`, only the display name gets enriched (real name when available; existing synthetic when Slack lookup fails).

## Behavior / fix

Per TDD §3, §6, §7.4, §D7. Eight files touched (one new: none in Phase 1 — all are edits; the new `internal/handlers/invite.go` lands in Phase 3).

### 1. `internal/types/types.go` — additive

```go
// New: surfaced by Phase 2's WhoElseResult.Humans and Phase 3's
// CreateResult.Humans / InviteHumanResult.Invited.
type Human struct {
    SlackUserID string `json:"slackUserId"`
    DisplayName string `json:"displayName"`
    Kind        string `json:"kind"` // always IdentityKindHuman ("human")
}

// New: slack-package representation of a user; consumed by the
// decoder enrichment + Phase 2's who_else filter.
type UserInfo struct {
    UserID      string `json:"userId"`
    DisplayName string `json:"displayName"`
    IsBot       bool   `json:"isBot"`
    Deactivated bool   `json:"deactivated"`
}
```

No changes to existing `Identity`, `IdentityKind*` consts, `WhoElseResult`, `CreateArgs`, `CreateResult` — those land in later phases.

### 2. `internal/slack/iface.go` — extend `Adapter`

Three new methods:

```go
// BotUserID returns the bot's own Slack user ID, captured via auth.test
// at adapter construction (see D7). Empty string from noTokenAdapter.
BotUserID() string

// ListChannelMembers returns Slack user IDs in the channel.
// Single-page only in v1 (NFR §2: typical huddles are < 10 humans).
ListChannelMembers(ctx context.Context, channelID string) ([]string, error)

// LookupUser resolves a ref (Slack user ID matching ^U[A-Z0-9]{8,}$
// or an email containing @) to UserInfo. Cached in-process with 1h
// TTL; concurrent calls for the same user ID deduplicated via
// singleflight. Returns ErrInvalidUserRef for other ref shapes.
LookupUser(ctx context.Context, ref string) (types.UserInfo, error)
```

### 3. `internal/slack/impl.go` — `slackGoAdapter` + `noTokenAdapter` + sentinels

New error sentinels (alongside existing `ErrNoToken`):

```go
var (
    ErrInvalidUserRef    = errors.New("ref is not a Slack user ID or email")
    ErrUserNotFound      = errors.New("user not found")
    ErrMissingEmailScope = errors.New("users:read.email scope is not granted")
    ErrRateLimited       = errors.New("slack returned Retry-After")
)
```

`slackGoAdapter` gains four new fields:

```go
type slackGoAdapter struct {
    client                  *slackapi.Client
    botUserID               string             // auth.test at construction
    orchestratorSlackUserID string             // from cfg.OrchestratorSlackUserID; may be ""
    userCache               *userCache         // sync.RWMutex + map; 1h TTL
    lookupGroup             singleflight.Group // dedupe concurrent LookupUser per user ID
    seq                     atomic.Uint64      // existing
}
```

`NewAdapter(cfg config.Config) Adapter`:

1. If `cfg.SlackBotToken == ""` → return `noTokenAdapter` (existing behavior).
2. Else, construct the client, call `auth.test` once. If `auth.test` fails → return `noTokenAdapter` (D7: a token that doesn't authenticate is effectively no token).
3. Otherwise, populate `botUserID` from `auth.test`'s response, store `cfg.OrchestratorSlackUserID` on the struct, initialize `userCache` (TTL 1h) + `lookupGroup`, return the adapter.

**The slack package does NOT import `internal/config`.** `cfg` is the parameter, but only `cfg.SlackBotToken` and `cfg.OrchestratorSlackUserID` are read at construction. Per TDD §D7 the orchestrator user ID is captured into a private field so `mapConversationMessages` can reach it without a config dep.

`LookupUser` implementation:

```go
func (a *slackGoAdapter) LookupUser(ctx context.Context, ref string) (types.UserInfo, error) {
    var userID string
    var err error
    switch {
    case userIDRegex.MatchString(ref):
        userID = ref
    case strings.Contains(ref, "@"):
        userID, err = a.lookupUserIDByEmail(ctx, ref) // users.lookupByEmail → translate "missing_scope" to ErrMissingEmailScope, "users_not_found" to ErrUserNotFound
        if err != nil {
            return types.UserInfo{}, err
        }
    default:
        return types.UserInfo{}, ErrInvalidUserRef
    }

    // Cache hit?
    if info, ok := a.userCache.get(userID); ok {
        return info, nil
    }

    // Singleflight to dedupe concurrent in-flight fetches per user ID.
    v, err, _ := a.lookupGroup.Do(userID, func() (any, error) {
        info, ferr := a.fetchUserInfo(ctx, userID) // users.info → UserInfo; translate rate-limit to ErrRateLimited
        if ferr != nil {
            return types.UserInfo{}, ferr
        }
        a.userCache.put(userID, info)
        return info, nil
    })
    if err != nil {
        return types.UserInfo{}, err
    }
    return v.(types.UserInfo), nil
}
```

`userIDRegex` — `regexp.MustCompile("^U[A-Z0-9]{8,}$")` at package level. (TODO comment: Slack Enterprise Grid uses `W`-prefixed user IDs — relax to `^[UW][A-Z0-9]{8,}$` in v0.2 if anyone hits it. Per TDD §D3.)

Display name resolution inside `fetchUserInfo`: prefer `profile.display_name` when non-empty, fall back to `profile.real_name`. (TDD OQ2 confirms.)

`ListChannelMembers` is a thin wrapper around the existing `slack-go/slack` `GetUsersInConversationContext`. Translate rate-limit responses to `ErrRateLimited`. No cache.

`BotUserID()` returns the cached `a.botUserID`. `noTokenAdapter.BotUserID()` returns `""`.

`noTokenAdapter` also gets the two new methods returning `ErrNoToken` — same pattern as the existing methods on it. Required so the interface check still passes; the `who_else` handler in Phase 2 will then degrade gracefully on `ErrNoToken` per TDD §7.3.

### 4. `internal/slack/fake_adapter.go` — `FakeAdapter` extensions

New fields and methods so Phase 2 + 3 handler tests can drive the adapter without real Slack:

```go
type FakeAdapter struct {
    // ... existing fields ...

    // BotUserIDValue is returned by BotUserID(); tests set this in setup.
    BotUserIDValue string

    // OrchestratorSlackUserIDValue is returned via a test-only accessor;
    // mirrors the slackGoAdapter's private field so tests can verify the
    // decoder's orchestrator-direct-Slack branch.
    OrchestratorSlackUserIDValue string

    // UsersByRef drives LookupUser. Key is the ref the test passes
    // (user ID, email, or anything); value is the UserInfo returned.
    // Missing key → ErrUserNotFound.
    UsersByRef map[string]types.UserInfo

    // ChannelMembers drives ListChannelMembers. Key is channelID.
    ChannelMembers map[string][]string

    // LookupUserErr / ListChannelMembersErr override the default
    // behavior to surface specific errors (ErrNoToken, ErrRateLimited).
    LookupUserErr         error
    ListChannelMembersErr error

    // LookupUserCalls records each (ctx, ref) call for assertions.
    LookupUserCalls []FakeLookupCall
}

type FakeLookupCall struct {
    Ref string
}
```

`FakeAdapter.LookupUser(ctx, ref)`:
- If `LookupUserErr != nil`, return it.
- If `ref` is in `UsersByRef`, return the entry.
- Otherwise return `ErrUserNotFound`.

`FakeAdapter.ListChannelMembers(ctx, channelID)`:
- If `ListChannelMembersErr != nil`, return nil + the error.
- Otherwise return `ChannelMembers[channelID]` (nil if absent — also fine).

`FakeAdapter.BotUserID()` returns `BotUserIDValue`.

A compile-time interface check at file end:

```go
var _ Adapter = (*FakeAdapter)(nil)
```

### 5. `internal/slack/messages.go` — enrich `mapConversationMessages`

This is the load-bearing decoder change. Per TDD §7.4 the function moves from a package-level helper to a method on `*slackGoAdapter` so it can reach `a.orchestratorSlackUserID` + call `a.LookupUser` without crossing the slack-package boundary.

**Signature change:**

```go
// Was:
//   func mapConversationMessages(messages []slackapi.Message) ([]types.Message, error)
// Now:
func (a *slackGoAdapter) mapConversationMessages(ctx context.Context, messages []slackapi.Message) ([]types.Message, error)
```

The only caller today (`History` in the same file) updates its callsite from `mapConversationMessages(resp.Messages)` to `a.mapConversationMessages(ctx, resp.Messages)`. `History` is itself a method on `*slackGoAdapter` and already takes `ctx`, so this is mechanical.

**Logic change** (replaces the existing `dup.DisplayName = "user-" + rawUser` synthetic-only branch, lines ~127–131):

```go
identity, body := Decode(msg.Text)
if identity.Kind == types.IdentityKindHuman && rawUser != "" {
    // (A) Orchestrator-via-Slack-direct: if the configured orchestrator
    // user ID matches, upgrade kind to orchestrator. When empty (env unset)
    // this is a no-op and the orchestrator's direct posts fall through
    // to (B) as a human — best-effort per TDD §7.4.
    if a.orchestratorSlackUserID != "" && rawUser == a.orchestratorSlackUserID {
        identity.Kind = types.IdentityKindOrchestrator
    }

    // (B) Enrich display name via users.info. On any error, fall back
    // to the existing synthetic. The read path never blocks on Slack
    // (TDD §D5). Note: if (A) upgraded kind to orchestrator AND (B)
    // fails, the result is {Kind: orchestrator, DisplayName: "user-Uxxxxx"} —
    // explicitly accepted v1 degradation per TDD §7.4.
    info, err := a.LookupUser(ctx, rawUser)
    if err == nil && info.DisplayName != "" {
        identity.DisplayName = info.DisplayName
    } else {
        identity.DisplayName = "user-" + rawUser
        // warn log on err (sampled — don't fill the log on a sustained outage)
    }
}
```

`Decode` itself does NOT change. Bot posts (with `*[name]` or `[name]` prefixes) still decode via the existing logic in `encoding.go`. The bot's own user ID is NOT consulted in the read path: if a message has a bracket prefix it's a seat/orchestrator (via the bot); if not, it's a human (or orchestrator-via-Slack-direct, handled in (A)).

## Acceptance

- `make check` green (vet + `go tool golangci-lint run` + `go test ./...` + build).
- `internal/types/types.go` has `Human` + `UserInfo` types, both exported, both with the documented JSON shape (`humans[].kind` is always `"human"`).
- `Adapter` interface in `iface.go` has `BotUserID() string`, `ListChannelMembers`, `LookupUser`. `var _ Adapter = (*slackGoAdapter)(nil)` and `var _ Adapter = (*FakeAdapter)(nil)` and `var _ Adapter = noTokenAdapter{}` all compile.
- `slackGoAdapter` stores `botUserID` from `auth.test` + `orchestratorSlackUserID` from `cfg`. `NewAdapter` falls back to `noTokenAdapter` if `auth.test` fails.
- The slack package does NOT import `internal/config` from inside `messages.go` or `impl.go`'s non-constructor methods. The `cfg` parameter to `NewAdapter` is the only entry point.
- `LookupUser`:
  - Returns cached `UserInfo` on subsequent calls within 1h TTL.
  - Dedupes concurrent calls for the same user ID via `singleflight`.
  - Translates Slack `missing_scope` (on `users.lookupByEmail`) to `ErrMissingEmailScope`.
  - Translates Slack rate-limit responses to `ErrRateLimited`.
  - Returns `ErrInvalidUserRef` for refs that match neither the user-ID regex nor contain `@`.
- `mapConversationMessages` is now a method on `*slackGoAdapter` and emits real display names from `users.info` for non-bot, non-orchestrator-via-bot human posts; falls back to `"user-" + rawUser` on lookup failure.
- A message from `rawUser == a.orchestratorSlackUserID` decodes as `{Kind: "orchestrator", DisplayName: <looked-up name>}`; when `users.info` fails, falls back to `{Kind: "orchestrator", DisplayName: "user-Uxxxxx"}` (per TDD §7.4 explicit accept).

## Test plan

1. **`internal/slack/impl_test.go`** — extend (or add file if absent):
   - `TestNewAdapterCachesBotUserID` — happy `auth.test` populates `botUserID`; tokenless path returns `noTokenAdapter`; failing `auth.test` returns `noTokenAdapter`.
   - `TestLookupUserCacheTTL` — first call hits Slack (stub), second call within TTL is local, post-TTL re-fetches. Use a short TTL (e.g. 50ms) via a constructor option or package-private setter for the test.
   - `TestLookupUserSingleflight` — N goroutines call `LookupUser("U123")` concurrently with the same uncached user ID; only one Slack call fires.
   - `TestLookupUserRefDispatch` — `U0ABC12345` → user-ID path; `joe@company.com` → email path; `nonsense` → `ErrInvalidUserRef`; `@joe` → `ErrInvalidUserRef` (handle support dropped in v3 per TDD §D3).
   - `TestLookupUserErrMissingEmailScope` — stub `users.lookupByEmail` returning `missing_scope` → `ErrMissingEmailScope`.
   - `TestLookupUserErrRateLimited` — stub returning `429` / `Retry-After` → `ErrRateLimited`.
2. **`internal/slack/messages_test.go`** — extend (or add):
   - `TestMapConversationMessagesEnrichesHuman` — `rawUser` not matching bot or orchestrator → `users.info` (FakeAdapter) → `{Kind: human, DisplayName: "Joe Smith"}`.
   - `TestMapConversationMessagesOrchestratorDirect` — `rawUser == orchestratorSlackUserIDValue` → `{Kind: orchestrator, DisplayName: <looked-up>}`.
   - `TestMapConversationMessagesOrchestratorDirectLookupFailure` — orchestrator match + `LookupUser` error → `{Kind: orchestrator, DisplayName: "user-Uxxxxx"}` (the accepted-v1 degradation).
   - `TestMapConversationMessagesLookupFailureSyntheticFallback` — human path + `LookupUser` error → `{Kind: human, DisplayName: "user-" + rawUser}`.
   - `TestMapConversationMessagesBotPrefixUnchanged` — `*[Operator] hi` decoded via bot prefix logic unchanged; `[seat-name] hi` likewise.
   - `TestMapConversationMessagesEmptyOrchestratorID` — `orchestratorSlackUserID == ""` → no (A) match; all non-bot users decode as human per (B).
3. **`internal/slack/encoding_test.go`** — light verification: `Decode` itself is untouched, but a couple of regression tests confirm existing seat/orchestrator/human-fallthrough cases still pass. (May already exist.)
4. **`internal/slack/fake_adapter_test.go`** — confirm `FakeAdapter.LookupUser` returns `UsersByRef[ref]` happy path + `ErrUserNotFound` on miss + `LookupUserErr` override.
5. **Compile-time interface checks**: `var _ Adapter = (*slackGoAdapter)(nil)`, `var _ Adapter = (*FakeAdapter)(nil)`, `var _ Adapter = noTokenAdapter{}`. If any are absent, the file won't compile.
6. `make check` — full green at the end.

## Out of scope

- `WhoElseResult.Humans` field + `who_else` handler edit — Phase 2.
- `CreateArgs.Humans` + create handler edit + `huddle.invite_human` verb + `internal/handlers/invite.go` — Phase 3.
- `SkippedHuman` / `SkippedReason` typed consts — those land in Phase 3 with the verbs that emit them.
- README + design.md updates documenting the new verb surface — Phase 3.
- A `huddle.refresh_humans` or any cache-busting verb — explicit non-goal per TDD §5.
- Re-calling `auth.test` on Slack auth-error responses for live bot-token rotation — TDD §D7 documents "restart on bot token rotation" as accepted v1 behavior; future work.
- `Adapter.History` signature changes beyond the internal `mapConversationMessages` callsite update — keep the public interface stable.
- `@handle` resolution path in `LookupUser` — dropped in TDD §D3 v3; the resolver returns `ErrInvalidUserRef` for `@…` refs.
