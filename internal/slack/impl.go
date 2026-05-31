package slack

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"
	"golang.org/x/sync/singleflight"
)

// userIDRegex matches Slack user IDs. TODO: Enterprise Grid uses W-prefixed IDs;
// relax to ^[UW][A-Z0-9]{8,}$ in v0.2 if needed (TDD §D3).
var userIDRegex = regexp.MustCompile(`^U[A-Z0-9]{8,}$`)

const defaultUserCacheTTL = time.Hour

type slackGoAdapter struct {
	client                  *slackapi.Client
	botUserID               string
	orchestratorSlackUserID string
	userCache               *userCache
	lookupGroup             singleflight.Group
	seq                     atomic.Uint64
}

// userCache memoizes users.info lookups with a TTL. Eviction is lazy: get
// treats an expired entry as a miss, and the next LookupUser re-fetch overwrites
// it via put. Entries for users never seen again are not actively purged, so the
// map grows with the count of distinct users ever observed. For v0 this is
// bounded in practice (small huddles, < 10 humans per the NFR) and deliberately
// avoids a background sweeper. TODO(v0.x): add active eviction if a long-lived
// server in a large workspace ever shows unbounded growth.
type userCache struct {
	mu   sync.RWMutex
	ttl  time.Duration
	data map[string]userCacheEntry
}

type userCacheEntry struct {
	info    types.UserInfo
	expires time.Time
}

func newUserCache(ttl time.Duration) *userCache {
	if ttl <= 0 {
		ttl = defaultUserCacheTTL
	}

	return &userCache{ttl: ttl, data: make(map[string]userCacheEntry)}
}

func (c *userCache) get(userID string) (types.UserInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, ok := c.data[userID]
	if !ok || time.Now().After(e.expires) {
		return types.UserInfo{}, false
	}

	return e.info, true
}

func (c *userCache) put(userID string, info types.UserInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[userID] = userCacheEntry{info: info, expires: time.Now().Add(c.ttl)}
}

// ErrNoToken is returned by every Slack-touching adapter method when the
// server was started without HUDDLE_SLACK_BOT_TOKEN. Slack-touching verbs
// (huddle.create / .close / .post / .read) surface this to the caller;
// huddle.who_else and huddle.list don't go through the adapter and are
// unaffected.
//
// Note on wrapping: handlers pass this through huddleerr.MCPError to
// serialize it across the MCP/JSON-RPC boundary. That wrapping drops
// the Go error chain by design — only the .Error() string survives. If
// a caller needs to detect ErrNoToken post-wrap, they have to match on
// the message text rather than use errors.Is. The wording is guarded
// by TestErrNoTokenMessageDocumentsRemedy so it stays stable.
var ErrNoToken = errors.New("HUDDLE_SLACK_BOT_TOKEN is not set; Slack-touching verbs (create, close, post, read) are unavailable — set the env to enable them")

// ErrInvalidUserRef is returned when LookupUser receives a ref that is neither
// a Slack user ID nor an email address.
var ErrInvalidUserRef = errors.New("ref is not a Slack user ID or email")

// ErrUserNotFound is returned when Slack reports the user does not exist.
var ErrUserNotFound = errors.New("user not found")

// ErrMissingEmailScope is returned when users.lookupByEmail lacks users:read.email.
var ErrMissingEmailScope = errors.New("users:read.email scope is not granted")

// ErrRateLimited is returned when Slack responds with HTTP 429 / Retry-After.
var ErrRateLimited = errors.New("slack returned Retry-After")

// noTokenAdapter satisfies Adapter without a Slack client; every method
// returns ErrNoToken. Used when HUDDLE_SLACK_BOT_TOKEN is unset so the
// MCP server still boots and local-only verbs can be served.
type noTokenAdapter struct{}

var _ Adapter = noTokenAdapter{}

func (noTokenAdapter) CreateChannel(context.Context, string) (Channel, error) {
	return Channel{}, ErrNoToken
}

func (noTokenAdapter) ArchiveChannel(context.Context, string) error {
	return ErrNoToken
}

func (noTokenAdapter) InviteUserToChannel(context.Context, string, string) error {
	return ErrNoToken
}

func (noTokenAdapter) PostMessage(context.Context, string, string, string) (string, error) {
	return "", ErrNoToken
}

func (noTokenAdapter) History(context.Context, string, *time.Time, int) ([]types.Message, error) {
	return nil, ErrNoToken
}

func (noTokenAdapter) BotUserID() string {
	return ""
}

func (noTokenAdapter) ListChannelMembers(context.Context, string) ([]string, error) {
	return nil, ErrNoToken
}

func (noTokenAdapter) LookupUser(context.Context, string) (types.UserInfo, error) {
	return types.UserInfo{}, ErrNoToken
}

// NewAdapter wires a Slack Web API Client from configuration. When
// cfg.SlackBotToken is empty, returns a no-token adapter that errors on
// every method with ErrNoToken — keeps the server bootable for
// local-only verbs (e.g. huddle.who_else).
func NewAdapter(cfg config.Config) Adapter {
	if cfg.SlackBotToken == "" {
		return noTokenAdapter{}
	}

	return newAdapterFromClient(newUnderlyingClient(cfg), cfg.OrchestratorSlackUserID)
}

func newAdapterFromClient(client *slackapi.Client, orchestratorSlackUserID string) Adapter {
	auth, err := client.AuthTest()
	if err != nil {
		slog.Warn("slack auth.test failed; Slack-touching verbs unavailable", "err", err)
		return noTokenAdapter{}
	}

	botUserID := ""
	if auth != nil {
		botUserID = strings.TrimSpace(auth.UserID)
	}

	return &slackGoAdapter{
		client:                  client,
		botUserID:               botUserID,
		orchestratorSlackUserID: orchestratorSlackUserID,
		userCache:               newUserCache(defaultUserCacheTTL),
	}
}

func (a *slackGoAdapter) BotUserID() string {
	return a.botUserID
}

func (a *slackGoAdapter) ListChannelMembers(ctx context.Context, channelID string) ([]string, error) {
	members, _, err := a.client.GetUsersInConversationContext(ctx, &slackapi.GetUsersInConversationParameters{
		ChannelID: channelID,
	})
	if err != nil {
		return nil, translateSlackAPIErr(err)
	}

	return members, nil
}

func (a *slackGoAdapter) LookupUser(ctx context.Context, ref string) (types.UserInfo, error) {
	var userID string

	var err error

	switch {
	case userIDRegex.MatchString(ref):
		userID = ref
	case strings.Contains(ref, "@"):
		if strings.HasPrefix(ref, "@") {
			return types.UserInfo{}, ErrInvalidUserRef
		}

		userID, err = a.lookupUserIDByEmail(ctx, ref)
		if err != nil {
			return types.UserInfo{}, err
		}
	default:
		return types.UserInfo{}, ErrInvalidUserRef
	}

	if info, ok := a.userCache.get(userID); ok {
		return info, nil
	}

	v, err, _ := a.lookupGroup.Do(userID, func() (any, error) {
		if info, ok := a.userCache.get(userID); ok {
			return info, nil
		}

		info, ferr := a.fetchUserInfo(ctx, userID)
		if ferr != nil {
			return types.UserInfo{}, ferr
		}

		a.userCache.put(userID, info)

		return info, nil
	})
	if err != nil {
		return types.UserInfo{}, err
	}

	info, ok := v.(types.UserInfo)
	if !ok {
		return types.UserInfo{}, fmt.Errorf("lookup singleflight: unexpected type %T", v)
	}

	return info, nil
}

func (a *slackGoAdapter) lookupUserIDByEmail(ctx context.Context, email string) (string, error) {
	user, err := a.client.GetUserByEmailContext(ctx, email)
	if err != nil {
		return "", translateLookupByEmailErr(err)
	}

	if user == nil || strings.TrimSpace(user.ID) == "" {
		return "", ErrUserNotFound
	}

	return user.ID, nil
}

func (a *slackGoAdapter) fetchUserInfo(ctx context.Context, userID string) (types.UserInfo, error) {
	user, err := a.client.GetUserInfoContext(ctx, userID)
	if err != nil {
		return types.UserInfo{}, translateSlackAPIErr(err)
	}

	if user == nil {
		return types.UserInfo{}, ErrUserNotFound
	}

	return userInfoFromSlackUser(*user), nil
}

func userInfoFromSlackUser(u slackapi.User) types.UserInfo {
	display := strings.TrimSpace(u.Profile.DisplayName)
	if display == "" {
		display = strings.TrimSpace(u.Profile.RealName)
	}

	return types.UserInfo{
		UserID:      u.ID,
		DisplayName: display,
		IsBot:       u.IsBot,
		Deactivated: u.Deleted,
	}
}

func translateLookupByEmailErr(err error) error {
	var slackErr slackapi.SlackErrorResponse
	if errors.As(err, &slackErr) {
		switch slackErr.Err {
		case "missing_scope":
			return ErrMissingEmailScope
		case "users_not_found":
			return ErrUserNotFound
		}
	}

	return translateSlackAPIErr(err)
}

func translateSlackAPIErr(err error) error {
	var rateLim *slackapi.RateLimitedError
	if errors.As(err, &rateLim) {
		return ErrRateLimited
	}

	var slackErr slackapi.SlackErrorResponse
	if errors.As(err, &slackErr) {
		switch slackErr.Err {
		case "user_not_found", "users_not_found":
			return ErrUserNotFound
		}
	}

	return fmt.Errorf("slack api: %w", err)
}

var _ Adapter = (*slackGoAdapter)(nil)
