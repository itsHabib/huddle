package slack

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"
)

type slackGoAdapter struct {
	client *slackapi.Client
	seq    atomic.Uint64
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

// NewAdapter wires a Slack Web API Client from configuration. When
// cfg.SlackBotToken is empty, returns a no-token adapter that errors on
// every method with ErrNoToken — keeps the server bootable for
// local-only verbs (e.g. huddle.who_else).
func NewAdapter(cfg config.Config) Adapter {
	if cfg.SlackBotToken == "" {
		return noTokenAdapter{}
	}
	return &slackGoAdapter{
		client: newUnderlyingClient(cfg),
	}
}

// noTokenAdapter satisfies Adapter without a Slack client; every method
// returns ErrNoToken. Used when HUDDLE_SLACK_BOT_TOKEN is unset so the
// MCP server still boots and local-only verbs can be served.
type noTokenAdapter struct{}

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
