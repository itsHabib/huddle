package slack

import (
	"github.com/itsHabib/huddle/internal/config"

	slackapi "github.com/slack-go/slack"
)

func newUnderlyingClient(cfg config.Config) *slackapi.Client {
	// Retry on HTTP 429 (Slack rate limit) per Slack client defaults.
	const maxRetries429 = 3

	return slackapi.New(cfg.SlackBotToken,
		slackapi.OptionRetry(maxRetries429),
	)
}
