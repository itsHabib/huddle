# Human participants in huddles — Technical Design Document

**Status:** draft / proposal — NOT a build commitment. The artifact we decide from.
**Owner:** @itsHabib
**Date:** 2026-05-30 (v3 — cycle-2 review revision; design lock candidate)
**Related:** [`docs/design.md`](../../design.md), [`README.md`](../../../README.md), [`internal/slack/encoding.go`](../../../internal/slack/encoding.go), [`internal/slack/messages.go`](../../../internal/slack/messages.go), [`internal/types/types.go`](../../../internal/types/types.go)

> **Reviewers — focus areas (v2):**
> - **§4 D7 (new)** — bot user ID provenance. Choosing `auth.test` at adapter construction; weigh against `HUDDLE_BOT_SLACK_USER_ID` env.
> - **§7.4 — where the new logic actually lands.** `internal/slack/messages.go::mapConversationMessages` (which has the raw `slack_user_id`), NOT `encoding.go::Decode` (which only sees text). Reviewers of v1 caught the layer error — confirm v2 places it correctly.
> - **§4 D3 (revised)** — handle support dropped to v0.2; v1 supports user ID + email only. Confirm.
> - **§7.3 (revised)** — filter is `{bot, orchestrator}`, not `{bot, seats}`. Seats aren't channel members.

> **What changed from v2 (cycle-2 review):**
> Cycle-2 surfaced five blockers, all small and mechanical. Resolutions: (a) §3 file count 7 → 8 — `invite.go` is the 8th. (b) §6 + §7.4: `cfg.OrchestratorSlackUserID` reference in v2's `mapConversationMessages` pseudocode crossed the `slack` → `config` package boundary. v3 stores the orchestrator user ID on `slackGoAdapter` (parallel to `botUserID` from D7) — `cmd/huddle/main.go` passes both at construction; the slack package never imports `config`. (c) §7.1: human invite failures are now best-effort and land in `Skipped{Reason: invite_failed}`, matching the existing `inviteOrchestrator` pattern in `create.go` and resolving the v2 compensation contradiction. (d) §7.3: a `ListChannelMembers` `ErrNoToken` no longer fails `who_else` — it degrades to `{seats, orchestrator, humans: []}`, preserving the tokenless `huddle.who_else` operation shipped in PR #19. (e) §6 + Phase 1 task list now include `ErrRateLimited` (introduced in §7.5 but absent from the contract in v2). Should-fixes also folded in: §7.3's "per D5" cross-reference points at §7.4 (its actual home); §7.4 names the orchestrator-direct + `LookupUser`-failure case as accept-it ("user-Uxxxxx"); §7.3 deactivated filter simplified to just `Deactivated == true`; D3 regex notes `W...` enterprise grid IDs as a follow-up; §7.4 documents the orchestrator-via-`huddle.post`-vs-direct-Slack display name inconsistency as a known v1 gap.

> **What changed from v1 (cycle-1 review):**
> Cycle-1 review caught seven blockers and four should-fixes. The big ones: (a) §7.4 misdescribed the existing decoder — `encoding.go::Decode` already returns `IdentityKindHuman` for fallthrough and `messages.go::mapConversationMessages` already sets `"user-" + rawUser`, so the change is a *replacement of the synthetic-name path*, not a new branch in `Decode`. (b) The new logic needs the raw Slack user ID, which `Decode` doesn't have — it lives in `mapConversationMessages`. (c) The bot user ID was used dispositively in v1 with no source defined; v2 adds D7 to fix. (d) `who_else`'s "drop seats from channel members" was a no-op (seats post via the bot and aren't channel members at all); v2's filter is `{bot, orchestrator}`. (e) Resolver ordering misrouted `@handle` to email; v2 drops handle support entirely for v1. (f) Email path requires `users:read.email` scope; called out in §2. (g) Type snippets in §6 didn't match actual `types.go`; v2 matches.

## 1. Problem & hypothesis

Today a huddle has seats (per-seat keys, bot-posts-on-behalf-of) and one implicit orchestrator (a single Slack user, auto-invited via `HUDDLE_ORCHESTRATOR_SLACK_USER_ID`). Humans other than the orchestrator can't be first-class participants:

- Agents calling `huddle.who_else` see seats + orchestrator only — no way to discover a teammate is in the channel.
- Posts in the channel from non-orchestrator humans decode to `Identity{Kind: human, DisplayName: "user-<rawUserID>"}` — the kind is correct but the display name is the raw Slack user ID, not their actual name.
- Agents have no surfaced way to `@`-mention a human (they don't know who's there).

The hypothesis is that this is fixable cheaply because Slack already authenticates humans natively. A human's Slack identity is stamped on every message they send — we don't need to issue them a key. The asymmetry is the design:

- **Seats** post via the bot → identified by **key** (huddle issues, huddle owns the lifecycle).
- **Humans** post natively → identified by **Slack user ID** (Slack owns the identity; huddle just reads it and looks up the display name).

The bet: with that asymmetry, humans require *zero* huddle-side persistence. They're not rows in a `humans` table — they're whoever is in the Slack channel minus the bot minus the orchestrator (counted separately) minus other bots. Adding a human is `conversations.invite`. Removing is `conversations.kick`. The Slack channel IS the participant registry for humans.

**Non-goals (v1).**
- A `humans` table or any human-keyed schema. The whole point is no huddle-side state.
- A new auth surface — Slack does the auth.
- `add_seat` / `revoke_key` for humans. Those concepts only apply to keyed (bot-mediated) identities.
- A `huddle.remove_human` verb. Removal is "kick from Slack channel" — see D6.
- Audit logs of "who was in this huddle at time T." See §8.
- `@handle` resolution (e.g. `@joe`). Punted to v0.2 — see D3.
- Mention-routing intelligence beyond surfacing user IDs. Agents construct `<@U…>` strings directly.

## 2. Functional & non-functional requirements

**Functional.**

- F1. `huddle.create` accepts an optional `humans: [...]` array. Each entry is a Slack user ID (`U…`) or an email (resolved via `users.lookupByEmail`). Server resolves to user IDs and invites each via `conversations.invite`.
- F2. New verb `huddle.invite_human { huddleId, humans }` adds humans to an existing huddle (same resolver, same invite logic).
- F3. `huddle.who_else` returns a `humans` array alongside `seats` + `orchestrator`. Each entry: `{slackUserId, displayName, kind: "human"}`.
- F4. `huddle.read` decodes posts from Slack users who are neither the bot nor the orchestrator as `Identity{Kind: human, DisplayName: <slack profile display_name>}` via a cached `users.info` lookup.
- F5. The `users.info` lookup used by F3/F4 is cached in-process and deduplicated within a single read via `singleflight` (see §7.5 — addresses cycle-1 cold-cache rate-limit concern).

**Non-functional.**

| Concern | Target |
|---|---|
| `who_else` latency (cold) | < 500ms p95 for huddles with < 10 humans. One `conversations.members` + N `users.info` calls; N typically 2-5. Pagination on `conversations.members` is single-page for that size. Acknowledged: large channels (200+ members from app integrations) would page. |
| `who_else` latency (warm) | < 50ms p95 from local cache. |
| `huddle.read` cold-cache | A single `read` covering history with K distinct humans fires K `users.info` calls (deduplicated via `singleflight` within the call). Bounded by K, not by message count. Slack tier-2 method limit (~50/min) is the ceiling; at K=10 and 1 `read` per minute we're well below. |
| Cache TTL | 1h for `users.info` entries keyed by user ID (display names rarely change). No invalidation verb; restart busts. |
| Backward compat (wire) | Existing `huddle.create` calls without `humans` work unchanged. `huddle.who_else` responses gain a `humans` field — always present as a JSON array (no `omitempty`), so clients always see `"humans": []` for empty. Strict-schema clients need a minor bump; loose clients are unaffected. |
| Backward compat (decoder) | Existing posts that today decode as `{Kind: human, DisplayName: "user-<rawUserID>"}` will decode as `{Kind: human, DisplayName: <profile.display_name>}` after the change. Same `Kind`; the only delta is a real name in place of the synthetic. Posts where `users.info` fails fall back to the existing synthetic name (D5). |
| Required Slack OAuth scopes | `channels:read`, `users:read`, `users:read.email`. The first two should already be granted (the existing `conversations.invite` + `users.info` paths use them); `users:read.email` is NEW for `users.lookupByEmail` in F1/F2. Operator must add it via Slack workspace settings before the create-with-humans-by-email path will work. The TDD's v1 claim of "no new scopes" was wrong; this is the fix. |
| Rate-limit safety | `users.info` is Slack tier-2 (~50 calls/min/method). With `singleflight` + 1h cache + small-huddle assumption, typical operation is < 5 `users.info` calls per minute. Burst case (cold restart, simultaneous `read` across multiple huddles) is bounded by the # of distinct user IDs across all reads in flight; if it ever saturates, decoder falls through to D5 (synthetic name) and the next `read` retries. |

## 3. Architecture overview

The change is additive across **eight files** (one new: `internal/handlers/invite.go`):

```
internal/types/types.go          — Human, UserInfo, ErrUserNotFound; small extensions to WhoElseResult/CreateArgs/CreateResult/InviteHumanArgs/InviteHumanResult
internal/slack/iface.go          — Adapter.{ListChannelMembers, LookupUser, BotUserID}
internal/slack/impl.go           — slackGoAdapter methods + in-process userCache; auth.test at construction
internal/slack/fake_adapter.go   — FakeAdapter implementations of the new methods
internal/slack/messages.go       — mapConversationMessages: enrich Kind=human messages with real display_name via LookupUser
internal/handlers/create.go      — resolve + invite humans at create time
internal/handlers/who_else.go    — channel-members join, filter to humans
internal/handlers/invite.go      — NEW: huddle.invite_human verb
```

The seam is `slack.Adapter` — the existing interface in `iface.go`. Three new methods land there; everything else uses them. No new packages, no DB migrations, no schema changes.

```
huddle.create {humans}          ──► resolveHuman ──► conversations.invite
                                       │
                                       └─► (no DB write for humans)

huddle.invite_human             ──► resolveHuman ──► conversations.invite

huddle.who_else                 ──► conversations.members  (Slack)
                                       │
                                       ├─ drop bot user (adapter.BotUserID())
                                       ├─ drop orchestrator (cfg.OrchestratorSlackUserID, if configured)
                                       └─► users.info × N (cached + singleflight) ──► humans[]

huddle.read (mapConversationMessages — adapter-owned)
                                       │ for each message:
                                       │   1) Decode(text) → kind, displayName
                                       │   2) if Kind == human && rawUser != "":
                                       │      • if rawUser == adapter.orchestratorSlackUserID → kind = orchestrator
                                       │      • else → LookupUser(rawUser) (cached) → use display_name
                                       │      • on lookup failure → "user-" + rawUser (existing fallback;
                                       │        applies to the orchestrator-direct branch too — see §7.4)
```

## 4. Key decisions & trade-offs

### D1 — Field shape: `humans` vs `participants`

**Chosen: `humans` (asymmetric).**

`huddle.create`'s argv currently has `seats: [{id, displayName}]`. The simplest extension is `humans: ["U…" | "email"]` — an array of unresolved refs. Different shape (string vs object), different resolution (Slack lookup vs key issuance), different lifecycle (channel membership vs DB row). Forcing them into one `participants` array with a discriminator (`{kind: "seat"|"human", ...}`) would make the argv shape lumpier and hide the real asymmetry.

**Alternative considered:** `participants: [{kind, ...}]`. Rejected — looks uniform on the wire but isn't uniform anywhere else.

### D2 — `huddle.invite_human` as a distinct verb vs `create`-only

**Chosen: distinct verb + `create` extension. Both.**

`huddle.create` accepts `humans` at create time. `huddle.invite_human { huddleId, humans: [...] }` adds humans to an existing channel. The verb is small (~30 LOC for handler + tests) and the UX dividend is large — forcing the operator to switch to Slack UI mid-huddle to add a teammate is the wrong default.

**Alternative considered:** `create`-only. Rejected. **Not chosen:** generalized `huddle.add_participants` — same reasoning as D1; adding seats to an existing huddle is a separate v1 feature (`add_seat` is in the v1 roadmap in `docs/design.md`) with its own key-issuance semantics.

### D3 — Resolver: ref formats supported in v1

**Chosen v2 (revised): Slack user ID + email only. Drop `@handle`.**

Cycle-1 review surfaced multiple problems with `@handle`:

- `users.list` is cursor-paginated; cold-fill for a 5000-user workspace is minutes of latency on the first call. The TDD didn't spec who pays that latency or the refresh policy.
- Two candidate fields (`profile.display_name`, `name`) — neither is unambiguous; operators expect `@joe` to match either.
- Handles can change; a cached handle→ID mapping goes stale silently.
- No `Skipped.Reason` for ambiguity in v1's contract.

For v1 we drop handle support entirely. Operator passes user IDs (`U…`) or emails. Both are unambiguous, single-call lookups, no list traversal. Handles can come in v0.2 with a proper `users.list` lifecycle if there's demand.

The resolver in `Adapter.LookupUser(ctx, ref) (UserInfo, error)`:

- Matches `^U[A-Z0-9]{8,}$` → user ID, pass through to `users.info`. (TODO: Slack Enterprise Grid workspaces use `W`-prefixed user IDs. The implementation should leave a comment on the regex flagging this as a known limitation; relax to `^[UW][A-Z0-9]{8,}$` in v0.2 if anyone hits it.)
- Contains `@` → email, `users.lookupByEmail` (requires `users:read.email` scope — see §2).
- Otherwise → `slack.ErrInvalidUserRef` (caller appends to `Skipped`).

Cache: a small in-process map `slackUserID → UserInfo{displayName, isBot, deactivated}` with a 1h TTL. Cache key is always user ID; the resolver normalizes refs to user IDs before caching.

**Alternative considered:** push resolution into the handler. Rejected — handler depends on `Adapter` (the seam); duplicating resolution logic in handlers and the message-mapper leaks ref-format knowledge into two places.

### D4 — `who_else` polls Slack every call vs caches channel membership

**Chosen: poll every call.**

`who_else` is called rarely (an agent might call it 1-3x per session). One `conversations.members` (tier-2) plus N `users.info` (cached after first hit + singleflight per D7). Typical huddle: < 10 humans, < 200ms p95 cold, < 50ms warm. Eventual consistency on adds is the expected behavior.

**Alternative considered:** cache `conversations.members` with TTL or invalidation. Rejected — invalidation requires Slack Events API subscription (huddle doesn't have one); a TTL alone creates the worst-case "I added Joe and he doesn't show up for 30s" bug.

### D5 — Decoder fallback when `users.info` fails

**Chosen: keep the existing `"user-" + rawUserID` fallback. No change to the synthetic format.**

Cycle-1 review caught that v1's "user-<suffix>" proposal conflicted with the existing full-ID format in `messages.go:127-129`. v2 keeps the existing format. The change is *adding* the `users.info` enrichment ahead of the fallback, not changing the fallback itself.

```
preferred:  display_name (from users.info)
fallback:   "user-" + slackUserID  (existing; covers Slack outage, deactivated user, rate-limit, etc.)
```

A warn log fires on `users.info` failure so the operator notices a sustained outage. The fallback path matches the existing behavior, so this PR doesn't regress `huddle.read` output even when Slack is down — it just doesn't get the enrichment.

### D6 — No `huddle.remove_human` verb (v1)

**Chosen: not a verb. Removal is `conversations.kick` directly in Slack.**

Promoted from v1's OQ4 — this should have been a named decision, not an open question.

The "channel membership is truth" model is symmetric: adding a human is `conversations.invite`, removing is `conversations.kick`. The invite verb exists because agents have no Slack-write capability through huddle and need *some* way to onboard a teammate mid-session; removal is rarer and almost always done by a human (the operator) who has direct Slack access.

**Alternative considered:** symmetric `remove_human` verb. Rejected for v1 — adds API surface for a flow that almost never matters. If it does, we add it in v0.2.

### D7 — Bot user ID + orchestrator user ID stored on the adapter

**Chosen: both captured at `NewAdapter` construction time. The slack package never imports `config`.**

Cycle-1 review caught that v1 used `bot_user_id` in the decoder (§7.4) and `who_else` filter (§7.3) without specifying where it comes from. Cycle-2 caught the same problem for `cfg.OrchestratorSlackUserID` — v2's `mapConversationMessages` pseudocode referenced it directly, which would force `internal/slack` to import `internal/config`. That's the wrong layering: the slack package depends only on `types` and `slack-go`. v3 fixes both with the same shape.

`slackGoAdapter` captures two identifiers at construction:

- `botUserID string` — populated by calling `auth.test` once in `NewAdapter`.
- `orchestratorSlackUserID string` — passed in by the caller (`cmd/huddle/main.go`), read from `cfg.OrchestratorSlackUserID`. May be empty when the env var is unset.

`Adapter.BotUserID() string` exposes the bot ID. The orchestrator ID is internal to the adapter — used by `History` / `mapConversationMessages` for the reclassification branch in §7.4 — and doesn't need a public accessor.

`NewAdapter`'s signature thus becomes:

```go
func NewAdapter(cfg config.Config) Adapter
```

(unchanged — it already takes `cfg`; v3 just uses two fields of it). `cmd/huddle/main.go` passes the full `cfg` to `NewAdapter` exactly as today.

If `auth.test` fails at construction, `NewAdapter` returns the existing `noTokenAdapter` (see `slack/impl.go`'s tokenless path) — same shape as the no-token case, since a token that doesn't authenticate is effectively no token. In that path, `BotUserID()` returns `""` and `History`/`mapConversationMessages` skip the reclassification branch — see §7.3's tokenless-degradation handling.

**Token rotation:** if the operator rotates the bot token mid-process, the cached user ID is now stale. Decoder + `who_else` could misclassify. Acceptable v1 behavior: document "restart on bot token rotation"; the noTokenAdapter path will catch rotations that invalidate the token, but rotations that re-bind to a different bot user (rare) require restart. Future: re-call `auth.test` on Slack auth-error responses.

**Alternative considered:** `HUDDLE_BOT_SLACK_USER_ID` env var. Rejected — error-prone (operator copies it wrong, drift on token rotation is silent). `auth.test` is one tier-2 call at startup; cheap.

**Alternative considered (v3 cycle-2):** thread `orchestratorSlackUserID` through `Adapter.History`'s argument list (`History(ctx, channelID, since, limit, orchestratorSlackUserID string)`). Rejected — handler-side smell, leaks orchestrator concept into a generic history-fetch method, breaks the adapter's existing contract. Storing on the adapter struct is cleaner.

## 5. Data model

**Zero schema changes.** No new tables, no new columns.

The only state added is the in-process caches in `slack.slackGoAdapter`:

```go
type slackGoAdapter struct {
    // existing fields
    client       *slackapi.Client

    // new fields
    botUserID               string             // populated by auth.test at construction
    orchestratorSlackUserID string             // from cfg.OrchestratorSlackUserID at construction; may be ""
    userCache               *userCache         // keyed by Slack user ID
    lookupGroup             singleflight.Group // dedupe concurrent LookupUser per user ID
}

type userCache struct {
    mu   sync.RWMutex
    ttl  time.Duration  // 1h
    data map[string]userCacheEntry
}

type userCacheEntry struct {
    info     types.UserInfo
    expires  time.Time
}
```

`types.UserInfo` (new package-level type):

```go
type UserInfo struct {
    UserID      string  // Slack user ID
    DisplayName string  // profile.display_name; falls back to profile.real_name if empty
    IsBot       bool    // from users.info user.is_bot
    Deactivated bool    // from users.info user.deleted
}
```

Cache is private to the slack package. Lost on restart by design (operator restart busts; no `huddle.refresh` verb).

## 6. API contract

### Types (`internal/types/types.go`)

The existing identity types stay as-is — `Identity.Kind` is a plain `string` field and the kind constants are package-level string consts (not a typed `IdentityKind`). v2 does NOT propose typing them; that would be a breaking refactor out of scope.

**Existing — referenced for context, not changing:**

```go
const (
    IdentityKindSeat         = "seat"
    IdentityKindOrchestrator = "orchestrator"
    IdentityKindHuman        = "human"
    IdentityKindUnknown      = "unknown"
)

type Identity struct {
    Kind        string `json:"kind"`
    DisplayName string `json:"displayName,omitempty"`
    // …
}
```

**New types:**

```go
// Human is a non-orchestrator human participant returned by huddle.who_else.
// Always emitted with Kind=IdentityKindHuman.
type Human struct {
    SlackUserID string `json:"slackUserId"`
    DisplayName string `json:"displayName"`
    Kind        string `json:"kind"`  // always "human"
}

// UserInfo is the slack-package representation of a user, cached and shared
// between LookupUser and ListChannelMembers consumers.
type UserInfo struct {
    UserID      string `json:"userId"`
    DisplayName string `json:"displayName"`
    IsBot       bool   `json:"isBot"`
    Deactivated bool   `json:"deactivated"`
}
```

**Extensions to existing types:**

```go
// CreateArgs gains:
type CreateArgs struct {
    // existing fields…
    Humans []string `json:"humans,omitempty"`  // Slack user IDs or emails (per D3)
}

// CreateResult gains (cycle-1 review: claude flagged this was unspecified):
type CreateResult struct {
    // existing fields…
    Humans  []Human          `json:"humans"`             // always present; empty array if none
    Skipped []SkippedHuman   `json:"skippedHumans,omitempty"`
}

// WhoElseResult gains:
type WhoElseResult struct {
    // existing fields…
    Humans []Human `json:"humans"`  // always present; empty array if none (no omitempty — see §2)
}

// InviteHumanArgs is the args struct for the new huddle.invite_human verb.
type InviteHumanArgs struct {
    HuddleID string   `json:"huddleId"`
    Humans   []string `json:"humans"`  // same ref formats as CreateArgs.Humans
}

// InviteHumanResult mirrors CreateResult's invited/skipped shape.
type InviteHumanResult struct {
    Invited []Human        `json:"invited"`
    Skipped []SkippedHuman `json:"skipped,omitempty"`
}

// SkippedHuman is the typed reason a human ref couldn't be invited.
// Reason values are typed constants so callers can switch on them.
type SkippedHuman struct {
    Ref    string         `json:"ref"`
    Reason SkippedReason  `json:"reason"`
}

type SkippedReason string

const (
    SkippedReasonAlreadyInChannel  SkippedReason = "already_in_channel"
    SkippedReasonUnknownUser       SkippedReason = "unknown_user"
    SkippedReasonInvalidRef        SkippedReason = "invalid_ref"
    SkippedReasonMissingEmailScope SkippedReason = "missing_email_scope"  // users:read.email not granted
    SkippedReasonInviteFailed      SkippedReason = "invite_failed"        // §7.1: conversations.invite returned an error other than already_in_channel
)
```

### Adapter interface (`internal/slack/iface.go`)

Three new methods:

```go
type Adapter interface {
    // existing methods…

    // BotUserID returns the bot's own Slack user ID, captured via auth.test at
    // adapter construction. Empty string from the no-token adapter.
    BotUserID() string

    // ListChannelMembers returns Slack user IDs in the channel.
    // Single-page only in v1 (acknowledged in §2 NFR).
    ListChannelMembers(ctx context.Context, channelID string) ([]string, error)

    // LookupUser resolves a ref (Slack user ID or email) to UserInfo.
    // Cached in-process with 1h TTL; concurrent calls for the same user ID
    // deduplicated via singleflight.
    LookupUser(ctx context.Context, ref string) (types.UserInfo, error)
}
```

### Errors (`internal/slack/`)

New sentinels alongside existing `ErrNoToken`:

```go
var (
    ErrInvalidUserRef     = errors.New("ref is not a Slack user ID or email")
    ErrUserNotFound       = errors.New("user not found")
    ErrMissingEmailScope  = errors.New("users:read.email scope is not granted")  // surfaced from users.lookupByEmail "missing_scope"
    ErrRateLimited        = errors.New("slack returned Retry-After")              // §7.5; consumer decides degrade-vs-fail
)
```

Handlers translate to `MCPError(CodeInvalidParams, ...)` or append to `SkippedHuman` lists per verb.

## 7. Key flows

### 7.1 `huddle.create { humans: ["U0ABC", "joe@company.com"] }`

1. Existing create flow runs to channel creation + huddle row insert + seat keys (i.e. `executeCreate` runs to completion as today; humans are added *after* the existing flow, never before).
2. For each ref in `humans`: `Adapter.LookupUser(ref)`. On `ErrInvalidUserRef` / `ErrUserNotFound` / `ErrMissingEmailScope`: append to `Skipped` with the matching `SkippedReason`, continue.
3. For each successfully resolved user ID: `Adapter.InviteUserToChannel(channelID, userID)`. Behavior matches the existing `inviteOrchestrator` pattern in `create.go` — **best-effort, never fail the verb**:
   - `already_in_channel` → `Skipped{Reason: AlreadyInChannel}`, continue.
   - Other Slack errors → `Skipped{Reason: InviteFailed}` + warn log, continue.
   - Success → append to `Invited`.
4. `CreateResult.Humans` = successfully invited; `CreateResult.Skipped` = the skip list (may be empty). The verb returns `200 OK` even when `Invited` is empty — same shape as `huddle.invite_human`.

There's no compensation path for human invites — they happen post-commit and are best-effort by design. If `huddle.create`'s pre-commit flow fails (the existing channel-create / huddle-row-insert / seat-key-insert path) the existing compensation runs as today; human invites never started, so nothing to clean up. The v2 spec contained a contradictory compensation note ("archive channel if humans invited but huddle row not committed") which was unreachable given the ordering — v3 removes it.

### 7.2 `huddle.invite_human { huddleId, humans }`

1. `Store.LookupHuddle(huddleId)` → if not found, return `CodeInvalidParams`.
2. For each ref: same resolver + invite path as §7.1 (including best-effort invite semantics — `InviteFailed` → `Skipped`, never fails the verb). Build `Invited` / `Skipped`.
3. Return `{Invited, Skipped}`. Empty `Invited` with non-empty `Skipped` is a normal return (not an error).

No DB write at any point.

### 7.3 `huddle.who_else { key }`

1. Existing lookup: key → seat → huddle.
2. `Adapter.ListChannelMembers(huddle.SlackChannelID)` → `[U…]`. Error handling:
   - `slack.ErrNoToken` → **graceful degrade**: return `{purpose, orchestrator, seats, humans: []}`. Preserves the tokenless-`huddle.who_else` operation shipped in PR #19 (`internal/slack/impl.go`'s `noTokenAdapter` returns `ErrNoToken` from every adapter method including the new `ListChannelMembers`). `huddle.who_else` continues to work as a local-only verb when the operator hasn't set `HUDDLE_SLACK_BOT_TOKEN` — the human-discovery feature simply isn't available in that mode, which is exactly the expected v0.2-and-later behavior anyway.
   - `slack.ErrRateLimited` or other Slack errors → `CodeInternalError`. With a real token, the verb has nothing useful to say about humans without channel membership.
3. Filter (skipped when step 2 degraded):
   - Drop `Adapter.BotUserID()`.
   - Drop `adapter.orchestratorSlackUserID` if non-empty (stored on the adapter per D7 — never `cfg.` reference here, since `internal/slack` doesn't import `internal/config`). If empty, the orchestrator isn't filtered here and will appear in `humans[]` — per §7.4's unset-env behavior. The orchestrator is reported in the existing `orchestrator` field of `WhoElseResult` regardless.
   - **Don't filter seats** — seats post via the bot and never appear in `conversations.members` as individual users. The v1 spec's "seat owners" filter was a no-op; v2 removes that step.
4. For each remaining user ID: `Adapter.LookupUser(userID)`. Drop entries where `UserInfo.IsBot == true` (e.g., other Slack apps in the channel). Drop entries where `Deactivated == true` — a deactivated user being a current channel member is a Slack edge case and the simpler rule is "deactivated users are not live participants." (v2 had an extra "AND we never see them post" qualifier; that would require a `conversations.history` join out of scope for this verb. v3 simplifies.)
5. Result: `{purpose, orchestrator, seats: [...], humans: [...]}`.

### 7.4 `huddle.read` decoder — where the new logic lives

This is the most important rewrite from v1. The cycle-1 review correctly noted that v1 misdescribed the existing decoder.

**Existing code (ground truth):**

- `internal/slack/encoding.go::Decode(text)` returns `Identity{Kind: IdentityKindSeat | IdentityKindOrchestrator | IdentityKindHuman | IdentityKindUnknown}`, prefix-based, and only sees text.
- `internal/slack/messages.go::mapConversationMessages` calls `Decode(msg.Text)`, then for messages where `identity.Kind == IdentityKindHuman && rawUser != ""`, sets `dup.DisplayName = "user-" + rawUser`. The raw user ID is available HERE.

**v2/v3 change: enrich `mapConversationMessages`, not `Decode`. Function moves from package-level to a method on `slackGoAdapter` so it can reach the cached orchestrator user ID and call `LookupUser` directly.**

`mapConversationMessages` becomes a method on `slackGoAdapter`:

```go
// internal/slack/messages.go — signature changes from package-level fn to method:
//   was: func mapConversationMessages(messages []slackapi.Message) ([]types.Message, error)
//   now: func (a *slackGoAdapter) mapConversationMessages(ctx context.Context, messages []slackapi.Message) ([]types.Message, error)
//
// History (the only caller today) is itself an adapter method, so this is a
// one-line callsite change: msgs, err := a.mapConversationMessages(ctx, resp.Messages)

identity, body := Decode(msg.Text)
if identity.Kind == types.IdentityKindHuman && rawUser != "" {
    // (A) Orchestrator-via-Slack-direct: if the cached orchestrator
    //     Slack user ID matches, surface as orchestrator (not human).
    //     When HUDDLE_ORCHESTRATOR_SLACK_USER_ID is unset, the cached
    //     value is "" and this check is a no-op — orchestrator direct
    //     posts fall through to (B) and decode as human (best-effort).
    //
    //     This uses a.orchestratorSlackUserID — the cached value stored
    //     on the adapter at construction (D7). The slack package does
    //     NOT import internal/config; the value comes in via NewAdapter.
    if a.orchestratorSlackUserID != "" && rawUser == a.orchestratorSlackUserID {
        identity.Kind = types.IdentityKindOrchestrator
        // display name resolution: try LookupUser, fall back below
    }

    // (B) Enrich the display name via users.info.
    info, err := a.LookupUser(ctx, rawUser)
    if err == nil && info.DisplayName != "" {
        identity.DisplayName = info.DisplayName
    } else {
        // Fallback matches existing v1 behavior — never block on Slack.
        identity.DisplayName = "user-" + rawUser
        // warn log on err (sampled if noisy)
    }
}
```

`Decode` itself does not change — bot posts still decode via the existing prefix logic. The bot's own user ID (`a.botUserID`) is NOT consulted in the read path: if a message has a bracket-prefix it's a seat/orchestrator (via the bot); if it doesn't, it's a human (or an orchestrator-via-Slack-direct, handled in (A) above).

**Accept-the-degraded-orchestrator-name case (v3 explicit).** If branch (A) fires AND branch (B)'s `LookupUser` fails, the result is `{Kind: orchestrator, DisplayName: "user-Uxxxxx"}` — the orchestrator kind is preserved but the display name is the synthetic. This is an accepted v1 degradation; the alternative (caching the huddle's `orchestrator_display_name` field at adapter-construction or per-read) would require either a join or pre-fetch that the §7.5 latency target doesn't justify. The `huddle.who_else` path still surfaces the orchestrator with the canonical display name from the huddle row, so the agent has a working name to `@`-mention; the `huddle.read` transcript view tolerates the synthetic during a Slack outage. Restoring the canonical name is part of a v0.2 nice-to-have if the degradation becomes painful.

**Known v1 display-name inconsistency.** When the orchestrator posts via `huddle.post`, the message decodes via the prefix logic (`*[OrchestratorDisplayName]`) using the name stored on the huddle row. When the orchestrator posts directly in Slack, the same person's message decodes via the (A)+(B) path above using their Slack profile display name. These can differ (e.g. `"Operator"` vs `"John Doe"`). v1 accepts this — both names are "real" for that person; reconciling them would require unifying the huddle row's `orchestrator_display_name` with the Slack profile, a separate concern. Documented so implementers don't treat it as a bug.

This places the new logic where the data is available and avoids changing `Decode`'s signature.

**Cycle-1 review surfaced (now resolved):**
- v1's "drop the bot user from the decoder branch" check was redundant — `Decode` already separates bot posts (which have prefixes) from human posts (which don't), and `mapConversationMessages` only enriches `IdentityKindHuman` entries.
- v1's `Kind: ""` description of the existing code was wrong — `Decode` returns `IdentityKindHuman` for fallthrough, not empty.

### 7.5 Concurrency and rate-limit safety

`userCache` uses `sync.RWMutex` — reads (the hot path) take RLock, writes (cache fills + TTL evictions) take Lock.

**`singleflight` from day 1.** Cycle-1 review noted that a cold `huddle.read` over a 100-message channel with K distinct humans without singleflight fires K concurrent `users.info` per goroutine that reads it. With `singleflight.Group` keyed on user ID, K concurrent reads of the same uncached user ID collapse to one Slack call. This is ~5 LOC; v2 commits to it from Phase 1 rather than punting per v1.

**Per-read deduplication.** Within a single `huddle.read`, the message loop visits the same user ID K times (every message from Joe references Joe). The cache absorbs this — `LookupUser` is O(1) on a cache hit. Cold-start: the first lookup populates, subsequent same-ID lookups in the same loop hit cache.

**Rate-limit recovery.** Slack returns `Retry-After` on tier-2 saturation. v1 didn't acknowledge this; v2: on `Retry-After`, the adapter returns `ErrRateLimited` (a new sentinel). The decoder falls through to the D5 synthetic-name fallback; `who_else` returns `CodeInternalError`. No automatic retry in v1 — log the event so operator notices a pattern.

## 8. Concurrency / consistency / failure model

- **Slack as truth (for membership and identity).** Channel membership is whatever Slack says at call time. Display names are whatever `users.info` said within the last 1h.
- **`users.info` cache staleness.** Bounded by TTL (1h). Display name changes within an hour are missed; restart busts the cache. Acceptable v1 trade.
- **Slack outage.** `who_else` fails (`CodeInternalError` — verb has nothing useful to say). `read` degrades to existing synthetic display names + warn log. `create`/`invite` fail at the invite step; channel exists, operator can retry. No persistent corruption possible (no DB writes for humans at any point).
- **Bot-vs-human disambiguation.** The decoder branch in `mapConversationMessages` only enriches `Kind: human` entries; bot posts decode as seat/orchestrator via the prefix logic in `Decode`. `who_else`'s filter step drops the bot via `Adapter.BotUserID()`. If a malicious actor adds another Slack app to the channel, `users.info.is_bot` filters them; if they lie about bot-status (Slack model doesn't really allow this), the decoder treats them as human — Slack's auth model, not huddle's problem.
- **Race: human joins channel between `ListChannelMembers` and `LookupUser`.** Worst case: their lookup succeeds, they appear in the result. Best case (lookup fails because user was deleted in the same window): they're omitted. Both acceptable.
- **Race: cache eviction during burst.** `singleflight` collapses the burst; only the singleflight winner calls Slack.

**Explicit non-goal — audit / "who was here at T".** Reconstructing the participant set at a past timestamp is unanswerable from this design (no `seen_humans` table, display names are cached not persisted). If an audit story is needed later, it lands as a small additive table — explicitly *not* an "additive bolt-on" that breaks the v1 "zero state" model, because audit is a different feature. Cycle-1 review surfaced this; v2 calls it out so it's not silent. Slack's own audit log is the workaround in the meantime.

## 9. Rollout / implementation plan

Three phases. Each is one PR. No validation gate — the feature is mechanical, not exploratory. Phase 1 has slightly grown from v1 (singleflight is now in scope; bot user ID provenance via `auth.test`).

| Phase | Goal | Tasks | Depends on |
|---|---|---|---|
| **1. Decoder + types + adapter plumbing** | `huddle.read` decoder enriches human posts with real display names; bot user ID + orchestrator user ID captured on the adapter; `LookupUser` + `ListChannelMembers` adapter surface in place. No operator-facing verb changes; the existing wire shape of `huddle.read` is preserved (just better display names). | `types.Human` + `types.UserInfo` + `ErrInvalidUserRef`/`ErrUserNotFound`/`ErrMissingEmailScope`/`ErrRateLimited`; `Adapter.BotUserID()`/`ListChannelMembers`/`LookupUser` + `slackGoAdapter` impl (`auth.test` at construction, store `botUserID` + `orchestratorSlackUserID` on the struct, cache, singleflight); `FakeAdapter` impl with `UsersByRef`/`ChannelMembers`/`BotUserIDValue`/`OrchestratorSlackUserIDValue` fields; `mapConversationMessages` moved from package-level fn to `*slackGoAdapter` method with `(ctx, messages)` signature per §7.4; decoder enrichment; tests for cache, singleflight, decoder enrichment, orchestrator-direct-Slack branch, lookup-failure fallback, orchestrator+lookup-failure synthetic-name accept, `ErrRateLimited` propagation. | — |
| **2. `who_else` returns humans** | `huddle.who_else` joins Slack at call time and surfaces humans alongside seats + orchestrator. Tokenless operation preserved via the §7.3 `ErrNoToken` degrade path. | `WhoElseResult.Humans` field; `who_else` handler edit per §7.3 (including the `ErrNoToken` graceful-degrade branch); handler tests via FakeAdapter (no humans / one human / mixed bots-humans / Slack list error → `CodeInternalError` / tokenless → `humans: []` / orchestrator-in-channel-not-double-counted / deactivated user dropped). | Phase 1 |
| **3. Create-with-humans + `invite_human` verb** | Operator-facing surface: `humans` at create time and the new verb. Human invite errors are best-effort per §7.1 — `Skipped{Reason: invite_failed}`, never fails the verb. | `CreateArgs.Humans` + `CreateResult.{Humans, Skipped}` + create handler edit per §7.1; new `internal/handlers/invite.go` with `huddle.invite_human` per §7.2 + §6; `Register` it in `internal/server/`; `InviteHumanArgs`/`InviteHumanResult`/`SkippedHuman` + `SkippedReason` consts (including new `SkippedReasonInviteFailed`); handler tests (happy + partial-skip create, invite_human happy/missing-huddle/already-in-channel/invite-failed-becomes-skipped, email-scope-missing skip path); `README.md` + `docs/design.md` updates (verb table + env table + scope note for `users:read.email`). | Phase 1 |

Phase 2 and 3 are independent and can ship in parallel after Phase 1 lands.

Rough sizes (per the polish-phase weighted-LOC convention):

| Phase | Weighted LOC |
|---|---|
| 1 | ~140 (was ~80 in v1; +~60 for singleflight, bot ID, error types, and richer tests) |
| 2 | ~50 |
| 3 | ~110 (was ~90; +~20 for `CreateResult` shape + scope note + skip-reason consts) |
| **Total** | **~300** |

All three still sit in the "amazing" band individually (sub-500 weighted).

## 10. Open questions

- **OQ1.** What happens when `huddle.invite_human` is called with a ref the resolver succeeds on, but `conversations.invite` returns `not_in_channel` because the channel is private? Slack public channels are the v0 default; if a private channel ever exists, the invite-flow needs a `conversations.invite` capability check first. Punted: assume public until proven otherwise.
- **OQ2.** Display-name source from `users.info`: prefer `profile.display_name` when non-empty, fall back to `profile.real_name`, fall back to `user-<rawUserID>`. Matches Slack's UI behavior. Confirmed in §7.4 / D5; calling out here as a v0 implementation choice that's worth a 1-line confirm in the impl review.
- **OQ3.** Should `users.info` retries on Slack 5xx be in scope for v1? Current v2 stance: no — log + fall through. A retry loop with backoff is straightforward but adds latency variance on the read path; defer until observed.

## 11. Validation plan

No formal validation gate — the rollout is small enough that a gate would be theater. The acceptance signal is per-phase:

- **Phase 1:** a hand-test where the operator posts in a huddle channel as themselves (not via the bot), then calls `huddle.read` via the seat CLI — the post should appear with `kind: human` and their **actual display name** (not `user-Uxxxxx`). `make check` green. A second test with `users.info` deliberately broken (point at a bogus token) confirms the synthetic-name fallback still works.
- **Phase 2:** invite a teammate to the huddle channel manually in Slack, then a seat-CLI `who-else` shows them under `humans`. The orchestrator does NOT appear in `humans` (they're in the `orchestrator` field). No DB write inspected.
- **Phase 3:** `huddle.create { humans: ["U0ABC", "teammate@company.com"] }` on a fresh huddle results in those users invited; `CreateResult.Humans` lists them with real display names. `huddle.invite_human` on an existing huddle adds without touching the DB. Email-scope-missing path returns `Skipped{Reason: missing_email_scope}` cleanly (test by temporarily revoking the scope or using a workspace where it was never granted).

After all three phases ship, run `cmd/smoke` once with a hand-extended scenario that includes a human in the huddle to exercise the full path end-to-end against real Slack.
