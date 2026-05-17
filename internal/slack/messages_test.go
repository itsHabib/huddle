package slack

import (
	"testing"

	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"

	"github.com/stretchr/testify/require"
)

func TestConversationMappingSkipsSubtypesHumanAugmentation(t *testing.T) {
	t.Parallel()

	const rawSlackTs = "1738458123.000456"

	postedWant, tsErr := parseSlackTSToTime(rawSlackTs)
	require.NoError(t, tsErr)

	sys := slackapi.Message{Msg: slackapi.Msg{SubType: "channel_join"}}

	msgs := []slackapi.Message{
		sys,

		{Msg: slackapi.Msg{
			SubType:         "",
			Text:            "hello world",
			Timestamp:       rawSlackTs,
			User:            "U9ABCDEF",
			ThreadTimestamp: "",
		}},
	}

	out, err := mapConversationMessages(msgs)
	require.NoError(t, err)
	require.Len(t, out, 1)

	got := out[0]
	require.Equal(t, types.IdentityKindHuman, got.Identity.Kind)
	require.Equal(t, "user-U9ABCDEF", got.Identity.DisplayName)
	require.Equal(t, "hello world", got.Body)
	require.Equal(t, "U9ABCDEF", got.UserIDRaw)

	require.True(t, postedWant.Equal(got.PostedAt))
}
