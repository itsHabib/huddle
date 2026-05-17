package slack

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync/atomic"

	slackapi "github.com/slack-go/slack"
)

func slackErrorCode(err error) string {
	var se slackapi.SlackErrorResponse
	if errors.As(err, &se) {
		return se.Err
	}

	return ""
}

func (a *slackGoAdapter) CreateChannel(ctx context.Context, name string) (Channel, error) {
	ch, err := a.createConversation(ctx, name)
	if err != nil {
		code := slackErrorCode(err)
		if code != "name_taken" && code != "channel_taken" {
			return Channel{}, err
		}

		ch, retryErr := a.createConversation(ctx, nameWithSuffix(name, &a.seq))
		if retryErr != nil {
			return Channel{}, retryErr
		}

		return ch, nil
	}

	return ch, nil
}

func nameWithSuffix(name string, seq *atomic.Uint64) string {
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err == nil {
		return fmt.Sprintf("%s-%x", name, buf)
	}

	n := seq.Add(1)

	return fmt.Sprintf("%s-%d", name, n)
}

func (a *slackGoAdapter) createConversation(ctx context.Context, name string) (Channel, error) {
	apiCh, err := a.client.CreateConversationContext(ctx, slackapi.CreateConversationParams{
		ChannelName: name,
	})
	if err != nil {
		return Channel{}, fmt.Errorf("create conversation %q: %w", name, err)
	}

	return Channel{
		ID:   apiCh.ID,
		Name: apiCh.Name,
	}, nil
}

func (a *slackGoAdapter) ArchiveChannel(ctx context.Context, channelID string) error {
	if err := a.client.ArchiveConversationContext(ctx, channelID); err != nil {
		return fmt.Errorf("archive channel %s: %w", channelID, err)
	}

	return nil
}
