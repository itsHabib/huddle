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

// slackChannelNameMax is the Slack-side hard limit on conversation names.
// Names longer than this are rejected with `invalid_name_maxlength`.
const slackChannelNameMax = 80

// nameWithSuffix appends a short disambiguator to name for the
// `name_taken` retry path. The suffix is `-<hex4>` (5 chars) — when
// adding it would push the result past Slack's 80-char limit, the base
// is truncated first so the final string always fits. Falls back to a
// monotonic counter if crypto/rand isn't available.
func nameWithSuffix(name string, seq *atomic.Uint64) string {
	var suffix string
	buf := make([]byte, 2)
	if _, err := rand.Read(buf); err == nil {
		suffix = fmt.Sprintf("-%x", buf)
	} else {
		suffix = fmt.Sprintf("-%d", seq.Add(1))
	}

	base := name
	if budget := slackChannelNameMax - len(suffix); len(base) > budget {
		base = base[:budget]
	}

	return base + suffix
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

// InviteUserToChannel adds a single Slack user to channelID. Treats
// `already_in_channel` as idempotent success so re-runs converge instead of
// erroring on the second invite.
func (a *slackGoAdapter) InviteUserToChannel(ctx context.Context, channelID, userID string) error {
	if _, err := a.client.InviteUsersToConversationContext(ctx, channelID, userID); err != nil {
		if slackErrorCode(err) == "already_in_channel" {
			return nil
		}

		return fmt.Errorf("invite user %s to channel %s: %w", userID, channelID, err)
	}

	return nil
}

func (a *slackGoAdapter) ArchiveChannel(ctx context.Context, channelID string) error {
	if err := a.client.ArchiveConversationContext(ctx, channelID); err != nil {
		// Treat `already_archived` as idempotent success so retries after
		// a partial-success path (archive ok, MarkClosed failed) can
		// converge instead of failing forever.
		if slackErrorCode(err) == "already_archived" {
			return nil
		}

		return fmt.Errorf("archive channel %s: %w", channelID, err)
	}

	return nil
}
