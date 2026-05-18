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
}

// Channel summarizes a Slack conversation created for a huddle.
type Channel struct {
	ID   string
	Name string
}
