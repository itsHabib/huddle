# Human participants in huddles — Technical Design Document

**Status:** draft / proposal — NOT a build commitment. The artifact we decide from.
**Owner:** @itsHabib
**Date:** 2026-05-27
**Related:** [`docs/design.md`](../../design.md), [`README.md`](../../../README.md), [`internal/types/types.go`](../../../internal/types/types.go) (`IdentityKind: human` already reserved)

> **Reviewers — focus areas:**
> - **§4 Key decisions** — `humans` field vs generalized `participants` (D1), separate `invite_human` verb vs `create`-extension only (D2), and whether `who_else` polls Slack or caches (D4). These are the load-bearing calls.
> - **§7.4 Decoder flow** — the read path's behavior when `users.info` is uncached + the rate-limit story.
> - **§9 Phases** — sequencing is unusual: the decoder change ships *first* with no surfaced verb, so identity is correct even when humans are channel-joined out-of-band.

## 1. Problem & hypothesis

Today a huddle has seats (per-seat keys, bot-posts-on-behalf-of) and one implicit orchestrator (a single Slack user, auto-invited via `HUDDLE_ORCHESTRATOR_SLACK_USER_ID`). Humans other than the orchestrator can't be first-class participants:

- Agents calling `huddle.who_else` see seats + orchestrator only — no way to discover a teammate is in the channel.
- Posts in the Slack channel from random users don't decode to a known identity in `huddle.read` output.
- Agents have no surfaced way to `@`-mention a human (they don't know who's there).

The hypothesis is that this is fixable cheaply because Slack already authenticates humans natively. A human's Slack identity is stamped on every message they send — we don't need to issue them a key. The asymmetry is the design:

- **Seats** post via the bot → identified by **key** (huddle issues, huddle owns the lifecycle).
- **Humans** post natively → identified by **Slack user ID** (Slack owns the identity, huddle just reads it).

The bet: with that asymmetry, humans require *zero* huddle-side persistence. They're not rows in a `humans` table — they're whoever is in the Slack channel minus the bot minus the seats. Adding a human is `conversations.invite`. Removing is `conversations.kick`. The Slack channel IS the participant registry for humans.

**Non-goals.**
- A `humans` table or any human-keyed schema. The whole point is no huddle-side state.
- A new auth surface — Slack does the auth.
- `add_seat` / `revoke_key` for humans. Those concepts only apply to keyed (bot-mediated) identities.
- Mention-routing intelligence ("@-mention everyone who reacted with 👀") — agents construct `<@U…>` strings directly; routing rules are application logic, not platform.

## 2. Functional & non-functional requirements

**Functional.**

- F1. `huddle.create` accepts an optional `humans: [...]` array. Each entry is a Slack user ID, an email, or a `@handle`. Server resolves to user IDs and invites each.
- F2. A way to add humans to an existing huddle. (See D2 — verb shape.)
- F3. `huddle.who_else` returns a `humans` array alongside `seats` + `orchestrator`. Each entry: `{slackUserId, displayName, kind: "human"}`.
- F4. `huddle.read` decodes posts from Slack users who are neither the orchestrator nor a seat as `Identity{Kind: human, DisplayName: <slack profile name>}`.
- F5. The `users.info` lookup used by F3/F4 is cached in-process to avoid per-message Slack API hits.

**Non-functional.**

| Concern | Target |
|---|---|
| `who_else` latency | < 500ms p95, dominated by one `conversations.members` + N `users.info` calls (N typically 2-5). Cache hits drop subsequent calls to local-only. |
| Cache TTL | 1h for `users.info` (display names rarely change; an explicit `huddle.refresh_humans` is out of scope — operator can restart the server to bust). |
| Backward compat | Existing `huddle.create` calls without `humans` work unchanged. Existing `huddle.who_else` responses gain a `humans` field — clients ignoring unknown fields are unaffected; clients with strict schemas need a minor bump. |
| Rate-limit safety | Slack's `users.info` is rate-limited per-method (~50/min, tier 2). Cache + small huddle sizes (< 10 humans typical) keep us well below. |
| Auth scope | Existing bot scopes already include `channels:read` and `users:read` (verified — see `internal/slack/iface.go`). No new scopes. |

## 3. Architecture overview

The change is additive across four files:

```
internal/types/types.go        — types.Human + WhoElseResult.Humans
internal/slack/iface.go        — Adapter.{ListChannelMembers,LookupUser}
internal/slack/impl.go         — slackGoAdapter methods + in-process cache
internal/slack/encoding.go     — decoder branch for non-seat non-orch users
internal/handlers/create.go    — resolve + invite humans at create time
internal/handlers/who_else.go  — channel-members join, filter to humans
internal/handlers/invite.go    — NEW: huddle.invite_human verb (see D2)
```

The seam is `slack.Adapter` — the existing interface in `iface.go`. Two new methods land there; everything else uses them. No new packages, no DB migrations, no schema changes.

```
huddle.create {humans}          ──► resolve refs ──► conversations.invite
                                       │
                                       └─► (no DB write for humans)

huddle.invite_human             ──► resolve refs ──► conversations.invite

huddle.who_else                 ──► conversations.members
                                       │
                                       ├─ filter out bot + seats
                                       └─► users.info × N (cached) ──► humans[]

huddle.read                     ──► slack history
                                       │
                                       └─► decoder:
                                             seat?    → key prefix lookup
                                             orch?    → orchestrator_slack_user_id match
                                             else     → users.info (cached) → kind:human
```

## 4. Key decisions & trade-offs

### D1 — Field shape: `humans` vs `participants`

**Chosen: `humans` (asymmetric).**

`huddle.create`'s argv currently has `seats: [{id, displayName}]`. The simplest extension is `humans: ["U…" | "email" | "@handle"]` — an array of unresolved refs. Different shape (string vs object), different resolution (Slack lookup vs key issuance), different lifecycle (channel membership vs DB row). Forcing them into one `participants` array with a discriminator (`{kind: "seat"|"human", ...}`) would:

- Make the argv shape lumpier (every entry needs a `kind`).
- Hide the fact that seats and humans go through different code paths.
- Lose the visual asymmetry that reflects the real asymmetry.

**Alternative considered:** `participants: [{kind: "seat", id, displayName} | {kind: "human", ref}]`. Rejected — looks uniform on the wire but isn't uniform anywhere else. The collapse buys nothing.

### D2 — `huddle.invite_human` as a distinct verb vs `create`-only

**Chosen: distinct verb + `create` extension. Both.**

`huddle.create` accepts `humans` at create time (one Slack invite per entry, atomic with channel creation). `huddle.invite_human { huddleId, humans: [...] }` adds humans to an existing channel.

**Alternative considered:** `create`-only — "humans can be added by inviting them to the channel directly in Slack; we don't need a verb." This is *almost* viable because the channel-membership-is-truth design (§3, §7.3) means an out-of-band Slack invite is auto-discovered on the next `who_else`. But agents currently have no Slack-write capability through huddle; forcing the operator to switch to Slack UI to add a teammate mid-huddle is the wrong default. The verb is small (~30 LOC for handler + tests, reusing the resolver from D3) and the UX dividend is large.

**Not chosen:** generalized `huddle.add_participants` that handles seats + humans. Same reasoning as D1 — and adding seats to an existing huddle is a separate v1 feature (`add_seat` is in the v1 roadmap in `docs/design.md`) with its own key-issuance semantics. Don't conflate.

### D3 — Resolver: where does ref → user ID happen

**Chosen: in `internal/slack/impl.go`, exposed via `Adapter.LookupUser(ctx, ref) (UserInfo, error)`.**

Single method, switches on ref format internally:

- Starts with `U` and matches `^U[A-Z0-9]{8,}$` → user ID, pass through.
- Contains `@` → email, `users.lookupByEmail`.
- Starts with `@` → handle, strip `@`, look up in cached `users.list`.
- Otherwise → invalid ref, error.

Cache: a small in-process map `slackUserID → UserInfo{displayName, isBot}` with a 1h TTL. `LookupUser` and the decoder branch both go through it. Cache key is always user ID; the resolver normalizes refs to user IDs before caching.

**Alternative considered:** push resolution into the handler. Rejected — handler depends on `Adapter` (the seam); duplicating resolution logic in handlers and the decoder leaks ref-format knowledge into two places.

### D4 — `who_else` polls Slack every call vs caches channel membership

**Chosen: poll every call.**

`who_else` is called rarely (an agent might call it 1-3x per session, often once at start). The Slack API is one `conversations.members` (one tier-2 call) plus N `users.info` (cached after first hit). At typical huddle sizes (< 10 people), this is ~200ms even cold, ~10ms warm. Eventual consistency on adds (someone joins → agent calls `who_else` → they appear) is the expected behavior.

**Alternative considered:** cache `conversations.members` in-memory with TTL or invalidation. Rejected — invalidation requires hooking Slack channel events (separate Socket Mode / Events API surface huddle doesn't currently subscribe to). Without invalidation, the cache is just a TTL — and a TTL on membership creates the worst-case bug ("I added Joe and he doesn't show up for 30s"). Poll-on-call is simpler and correct.

If `who_else` ends up being called per-post (it isn't supposed to be), revisit.

### D5 — Decoder branch when `users.info` is cold and Slack is down

**Chosen: degrade to `Identity{Kind: human, DisplayName: "user-<userId-suffix>"}` and emit a warn log.**

The read path can't block on Slack — `huddle.read` should still return history even when `users.info` fails. The decoder falls back to a synthetic display name derived from the user ID (e.g., `user-ABC12345`). The cache absorbs subsequent calls once Slack recovers.

**Alternative considered:** propagate the error. Rejected — a single user-info failure shouldn't poison the whole `read` response. Read is the most-called verb; resilience matters more than richness.

## 5. Data model

**Zero schema changes.** No new tables, no new columns.

The only state added is the in-process cache in `slack.slackGoAdapter`:

```go
type userCache struct {
    mu   sync.RWMutex
    ttl  time.Duration  // 1h
    data map[string]userCacheEntry
}

type userCacheEntry struct {
    info     UserInfo
    expires  time.Time
}

type UserInfo struct {
    UserID      string
    DisplayName string
    IsBot       bool
}
```

Cache is private to the slack package. Lost on restart by design (operator restart busts; no `huddle.refresh` verb).

## 6. API contract

### New / changed types (`internal/types/types.go`)

```go
// Existing — no change to existing fields.
type IdentityKind string
const (
    KindSeat         IdentityKind = "seat"
    KindOrchestrator IdentityKind = "orchestrator"
    KindHuman        IdentityKind = "human"  // already reserved, now used
)

type Human struct {
    SlackUserID string       `json:"slackUserId"`
    DisplayName string       `json:"displayName"`
    Kind        IdentityKind `json:"kind"`  // always "human"
}

// CreateArgs gains:
type CreateArgs struct {
    // … existing fields …
    Humans []string `json:"humans,omitempty"`  // Slack user IDs, emails, or @handles
}

// WhoElseResult gains:
type WhoElseResult struct {
    // … existing fields …
    Humans []Human `json:"humans"`
}

// New verb args/result:
type InviteHumanArgs struct {
    HuddleID string   `json:"huddleId"`
    Humans   []string `json:"humans"`  // same ref formats as CreateArgs.Humans
}

type InviteHumanResult struct {
    Invited []Human `json:"invited"`   // resolved + successfully invited
    Skipped []struct {
        Ref    string `json:"ref"`
        Reason string `json:"reason"`  // already_in_channel | unknown_user | invalid_ref
    } `json:"skipped"`
}
```

### Adapter interface additions (`internal/slack/iface.go`)

```go
type Adapter interface {
    // … existing methods …

    // ListChannelMembers returns Slack user IDs in the channel, excluding bots.
    ListChannelMembers(ctx context.Context, channelID string) ([]string, error)

    // LookupUser resolves a ref (user ID | email | @handle) to UserInfo.
    // Cached in-process with a 1h TTL.
    LookupUser(ctx context.Context, ref string) (UserInfo, error)
}
```

### Errors

- `LookupUser` invalid ref → `huddleerr.MCPError(CodeInvalidParams, ...)` at handler boundary.
- `LookupUser` Slack 404 → `slack.ErrUserNotFound` (new sentinel, similar to `ErrNoToken`). Handler decides whether to skip (in invite) or fall through (in decode).
- `ListChannelMembers` errors → bubble through handlers as `CodeInternalError`; never used in the read path (decoder uses `LookupUser` only).

## 7. Key flows

### 7.1 `huddle.create { humans: ["U0ABC", "joe@company.com"] }`

1. Existing create flow runs to channel creation + huddle row insert.
2. For each ref in `humans`: `Adapter.LookupUser(ref)`. On error: log warn, append to `skipped` in result, continue.
3. For each successfully resolved user ID: `Adapter.InviteUserToChannel(channelID, userID)`. Treat `already_in_channel` as success (idempotent — same pattern as orchestrator invite).
4. Result includes a `humans` array of successfully invited entries.

Compensation: if `huddle.create` fails *after* humans are invited but *before* the huddle row is committed, the existing compensation path archives the channel — humans get auto-removed via channel archive. No separate human-cleanup needed.

### 7.2 `huddle.invite_human { huddleId, humans }`

1. `Store.LookupHuddle(huddleId)` to get `channelID`. Return `CodeInvalidParams` if not found.
2. For each ref: `LookupUser` → `InviteUserToChannel`. Same skip-and-continue policy as create.
3. Return `{invited, skipped}`.

No DB write at any point.

### 7.3 `huddle.who_else { key }`

1. Existing lookup: key → seat → huddle.
2. `Adapter.ListChannelMembers(huddle.SlackChannelID)` → `[U…]`.
3. Filter: drop the bot user (known from auth context); drop user IDs that are seat owners (cross-reference with `Store.ListKeys(huddleID)`).
4. For each remaining user ID: `Adapter.LookupUser(userID)`. Bots that slipped through (e.g., other apps in the channel) are dropped via `UserInfo.IsBot`. Humans are appended.
5. Result: `{purpose, orchestrator, seats: [...], humans: [...]}`.

If `ListChannelMembers` errors, return `CodeInternalError` — the verb has nothing useful to say without it. (Contrast with §7.4.)

### 7.4 `huddle.read` — decoder branch

The current decoder in `internal/slack/encoding.go` looks at each message's prefix:

- `[displayName] body` → seat (look up display name → seat ID).
- `*[displayName] body` → orchestrator.
- Otherwise → falls through (today: returns the message with `Identity{Kind: ""}` or similar, depending on the current code).

The new branch comes BEFORE the existing prefix checks because seat/orch posts are routed via the bot (their `slack_user_id` is the bot's), while human posts have the human's own `slack_user_id`. So:

1. If `message.user == bot_user_id`: fall through to existing prefix-based decoding (seat / orchestrator).
2. Else if `message.user == orchestrator_slack_user_id`: emit `Identity{Kind: orchestrator, ...}`. (Orchestrator-via-Slack-direct, not via MCP.)
3. Else: `Adapter.LookupUser(message.user)`. On success → `Identity{Kind: human, DisplayName: info.DisplayName}`. On error (Slack down, user info missing) → `Identity{Kind: human, DisplayName: "user-<userIdSuffix>"}` + warn log (see D5).

Order matters: bot-posts can't be from a human even if their `slack_user_id` matched somehow; bot is dispositive.

### 7.5 Concurrency

`userCache` uses `sync.RWMutex` — reads (the hot path) take RLock, writes (cache fills + TTL evictions) take Lock. A burst of concurrent `huddle.read` calls hitting the same uncached user ID will all call `LookupUser` (no singleflight) — for the typical small-huddle case this is fine and simpler than coordinating. If contention shows up, swap to `singleflight.Group`.

## 8. Concurrency / consistency / failure model

- **Slack as truth.** Channel membership is whatever Slack says it is at call time. Huddle never stores it, so it never gets stale.
- **`users.info` cache staleness.** Bounded by TTL (1h). Display name changes are rare; the worst case is an agent referring to "Joe Smith" when the human now goes by "Joseph S." Acceptable.
- **Slack outage.** `who_else` fails (can't list members). `read` degrades to synthetic display names. `create`/`invite` fail at the invite step; the channel exists, the operator can retry. No persistent corruption possible.
- **Bot-vs-human disambiguation.** A bot that's been added to the channel (e.g., a CI bot) shows up in `ListChannelMembers`. `UserInfo.IsBot` filters them. If a malicious actor adds a "real" bot account that lies about its type, the decoder treats it as human — but this is Slack's auth model, not huddle's problem.
- **Race: human joins channel between `ListChannelMembers` and `LookupUser`.** Worst case: their lookup succeeds, they appear in the result. Best case (lookup fails because race with user delete): they're omitted. Either is acceptable.

## 9. Rollout / implementation plan

Three phases. Each is one PR. No validation gate — the feature is mechanical, not exploratory.

| Phase | Goal | Tasks | Depends on |
|---|---|---|---|
| **1. Decoder + types** | Read-path correctness so humans posting in the channel decode correctly, even without a surfaced add-verb. Plumbing only — no new verbs. | `types.Human` + `IdentityKind` confirm-emit; `slack.Adapter` gains `LookupUser` + `ListChannelMembers`; in-process `userCache`; encoding.go decoder branch (§7.4); tests for the new adapter methods + the decoder | — |
| **2. `who_else` returns humans** | `huddle.who_else` joins Slack at call time to surface humans alongside seats + orchestrator. | `who_else` handler edit (§7.3); `WhoElseResult.Humans`; handler tests via FakeAdapter | Phase 1 |
| **3. Create + invite verbs** | The operator-facing surface: `humans` at create time and `huddle.invite_human` for additive. | `CreateArgs.Humans` + create handler edit (§7.1); new `huddle.invite_human` verb (handler + Register) (§7.2); handler tests | Phase 1 |

Phase 2 and 3 are independent of each other (both depend on Phase 1's adapter methods). They can ship in parallel after Phase 1 lands.

Rough sizes (per the polish-phase weighted-LOC convention):

| Phase | Weighted LOC |
|---|---|
| 1 | ~80 (adapter + cache + decoder + tests) |
| 2 | ~50 (one handler + test) |
| 3 | ~90 (create extension + new verb + tests) |
| **Total** | **~220** |

All three sit in the "amazing" band individually.

## 10. Open questions

- **OQ1.** What happens when `huddle.invite_human` is called with a ref the resolver succeeds on, but `conversations.invite` returns `not_in_channel` because the channel is private? Slack public channels are the v0 default; if a private channel ever exists, the invite-flow needs a `conversations.invite` capability check first. Punted: assume public until proven otherwise.
- **OQ2.** Should `huddle.read` decoder skip messages from non-bot, non-orchestrator, non-channel-member Slack users? (i.e., someone who used to be in the channel but was kicked, but their old messages are still in history.) Current proposal: still decode as `kind: human` via `LookupUser` — they were a participant when they posted, the transcript should reflect that.
- **OQ3.** Display name source: `users.info` returns `profile.display_name` (operator-set) and `profile.real_name`. Recommendation: prefer `display_name` when non-empty, fall back to `real_name`. Matches Slack's own UI behavior. Worth confirming in implementation.
- **OQ4.** No `huddle.remove_human` verb in scope. Removal is "kick from Slack channel directly." Is that the right default, or do we want a verb for symmetry? Recommend: no verb. Removing-via-Slack matches the "channel membership is truth" mental model and saves a verb.

## 11. Validation plan

No formal validation gate — the rollout is small enough that the gate would be theater. The acceptance signal is per-phase:

- **Phase 1:** a hand-test where the operator (or anyone) posts in a huddle channel, then calls `huddle.read` via the seat CLI — the post should appear with `kind: human`, correct display name. `make check` green.
- **Phase 2:** after Phase 1 lands, invite a teammate to a huddle channel manually (in Slack), then a seat-CLI `who-else` shows them under `humans`. No DB write inspected (because there shouldn't be one).
- **Phase 3:** `huddle.create { humans: [...] }` on a fresh huddle results in those users invited. `huddle.invite_human` on an existing huddle adds without touching the DB.

After all three phases ship, run `cmd/smoke` once with a hand-extended scenario that includes a human in the huddle to exercise the full path end-to-end against real Slack.
