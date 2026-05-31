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
	// v1 — capped at Slack's default conversations.members page size, which
	// covers the < 10-humans-per-huddle NFR. TODO(phase-2): add cursor-based
	// pagination (GetUsersInConversationParameters.Cursor) before who_else
	// consumes this, or large channels will silently drop members past page 1.
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
