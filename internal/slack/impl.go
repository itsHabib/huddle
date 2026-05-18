package slack

import (
	"sync/atomic"

	"github.com/itsHabib/huddle/internal/config"

	slackapi "github.com/slack-go/slack"
)

type slackGoAdapter struct {
	client *slackapi.Client
	seq    atomic.Uint64
}

// NewAdapter wires a Slack Web API Client from configuration.
func NewAdapter(cfg config.Config) Adapter {
	return &slackGoAdapter{
		client: newUnderlyingClient(cfg),
	}
}
