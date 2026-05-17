package slack

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"
)

func (a *slackGoAdapter) PostMessage(ctx context.Context, channelID, text, threadTS string) (string, error) {
	opts := []slackapi.MsgOption{
		slackapi.MsgOptionText(text, false),
	}
	if strings.TrimSpace(threadTS) != "" {
		opts = append(opts, slackapi.MsgOptionTS(threadTS))
	}

	_, ts, err := a.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return "", fmt.Errorf("post message to channel %s: %w", channelID, err)
	}

	return ts, nil
}

func (a *slackGoAdapter) History(ctx context.Context, channelID string, since *time.Time, limit int) ([]types.Message, error) {
	params := &slackapi.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     limit,
	}

	if since != nil && !since.IsZero() {
		ts := strconv.FormatFloat(float64(since.UTC().UnixNano())/1e9, 'f', 6, 64)
		params.Oldest = ts
	}

	resp, err := a.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("fetch history channel %s: %w", channelID, err)
	}

	msgs, err := mapConversationMessages(resp.Messages)

	return msgs, err
}

func mapConversationMessages(messages []slackapi.Message) ([]types.Message, error) {
	msgs := make([]types.Message, 0, len(messages))

	for _, sm := range messages {
		if strings.TrimSpace(sm.SubType) != "" {
			continue
		}

		id := strings.TrimSpace(sm.Timestamp)
		if id == "" && sm.SubMessage != nil {
			id = strings.TrimSpace(sm.SubMessage.Timestamp)
		}

		txt := strings.TrimSpace(sm.Text)
		if txt == "" && sm.SubMessage != nil {
			txt = strings.TrimSpace(sm.SubMessage.Text)
		}

		posted, perr := parseSlackTSToTime(id)
		if perr != nil {
			return nil, fmt.Errorf("parse Slack ts %q: %w", id, perr)
		}

		identity, body := Decode(txt)

		rawUser := strings.TrimSpace(sm.User)
		if rawUser == "" && sm.SubMessage != nil {
			rawUser = strings.TrimSpace(sm.SubMessage.User)
		}

		if identity.Kind == types.IdentityKindHuman && rawUser != "" {
			dup := identity
			dup.DisplayName = "user-" + rawUser
			identity = dup
		}

		threadTS := strings.TrimSpace(sm.ThreadTimestamp)
		if threadTS == "" && sm.SubMessage != nil {
			threadTS = strings.TrimSpace(sm.SubMessage.ThreadTimestamp)
		}

		msgs = append(msgs, types.Message{
			ID:        id,
			Body:      body,
			PostedAt:  posted,
			ThreadTS:  threadTS,
			SubType:   sm.SubType,
			Identity:  identity,
			UserIDRaw: rawUser,
		})
	}

	return msgs, nil
}

func parseSlackTSToTime(tsStr string) (time.Time, error) {
	if strings.TrimSpace(tsStr) == "" {
		return time.Time{}, nil
	}

	f, err := strconv.ParseFloat(tsStr, 64)
	if err != nil {
		return time.Time{}, err
	}

	sec := int64(f)
	sub := f - float64(sec)

	nsec := int64(sub * 1e9)

	return time.Unix(sec, nsec).UTC(), nil
}
