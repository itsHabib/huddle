package slack

import (
	"context"
	"time"

	"github.com/itsHabib/huddle/internal/types"
)

// Adapter is the Slack façade used across handlers so tests can fake it cleanly.
type Adapter interface {
	CreateChannel(ctx context.Context, name string) (Channel, error)
	ArchiveChannel(ctx context.Context, channelID string) error
	InviteUserToChannel(ctx context.Context, channelID, userID string) error
	PostMessage(ctx context.Context, channelID, text, threadTS string) (string, error)
	History(ctx context.Context, channelID string, since *time.Time, limit int) ([]types.Message, error)

	// BotUserID returns the bot's own Slack user ID, captured via auth.test at
	// adapter construction. Empty string from noTokenAdapter.
	BotUserID() string

	// ListChannelMembers returns Slack user IDs in the channel. Single-page in
	// v1 — capped at Slack's default conversations.members page size, far above
	// the < 10-humans-per-huddle NFR, so who_else / invite consume it directly.
	// TODO: add cursor-based pagination (GetUsersInConversationParameters.Cursor)
	// if a huddle channel ever exceeds one page; until then members past page 1
	// are silently dropped. Tracked as a parked dossier follow-up.
	ListChannelMembers(ctx context.Context, channelID string) ([]string, error)

	// LookupUser resolves a ref (Slack user ID or email) to UserInfo. Cached
	// in-process with 1h TTL; concurrent calls for the same user ID deduped
	// via singleflight.
	LookupUser(ctx context.Context, ref string) (types.UserInfo, error)
}

// Channel summarizes a Slack conversation created for a huddle.
type Channel struct {
	ID   string
	Name string
}
