package slack

import (
	"context"
	"net/http"
	"testing"

	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func testMapAdapter(orchestratorID string, users map[string]types.UserInfo) *slackGoAdapter {
	a := &slackGoAdapter{
		orchestratorSlackUserID: orchestratorID,
		userCache:               newUserCache(defaultUserCacheTTL),
	}
	for id, info := range users {
		a.userCache.put(id, info)
	}

	return a
}

func TestConversationMappingSkipsSubtypesHumanAugmentation(t *testing.T) {
	t.Parallel()

	const rawSlackTs = "1738458123.000456"

	postedWant, tsErr := parseSlackTSToTime(rawSlackTs)
	require.NoError(t, tsErr)

	sys := slackapi.Message{Msg: slackapi.Msg{SubType: "channel_join"}}

	msgs := []slackapi.Message{
		sys,
		{Msg: slackapi.Msg{
			SubType:   "",
			Text:      "hello world",
			Timestamp: rawSlackTs,
			User:      "U09ABCDEF",
		}},
	}

	a := testMapAdapter("", map[string]types.UserInfo{
		"U09ABCDEF": {UserID: "U09ABCDEF", DisplayName: "Joe Smith"},
	})

	out, err := a.mapConversationMessages(context.Background(), msgs)
	require.NoError(t, err)
	require.Len(t, out, 1)

	got := out[0]
	require.Equal(t, types.IdentityKindHuman, got.Identity.Kind)
	require.Equal(t, "Joe Smith", got.Identity.DisplayName)
	require.Equal(t, "hello world", got.Body)
	require.Equal(t, "U09ABCDEF", got.UserIDRaw)
	require.True(t, postedWant.Equal(got.PostedAt))
}

func TestMapConversationMessagesEnrichesHuman(t *testing.T) {
	t.Parallel()

	a := testMapAdapter("", map[string]types.UserInfo{
		"U0HUMAN01": {UserID: "U0HUMAN01", DisplayName: "Joe Smith"},
	})

	out, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "hi there", User: "U0HUMAN01", Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, types.IdentityKindHuman, out[0].Identity.Kind)
	require.Equal(t, "Joe Smith", out[0].Identity.DisplayName)
}

func TestMapConversationMessagesOrchestratorDirect(t *testing.T) {
	t.Parallel()

	const orchID = "UORCH12345"

	a := testMapAdapter(orchID, map[string]types.UserInfo{
		orchID: {UserID: orchID, DisplayName: "Operator Name"},
	})

	out, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "from slack UI", User: orchID, Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, types.IdentityKindOrchestrator, out[0].Identity.Kind)
	require.Equal(t, "Operator Name", out[0].Identity.DisplayName)
}

func TestMapConversationMessagesOrchestratorDirectLookupFailure(t *testing.T) {
	t.Parallel()

	const orchID = "UORCH99999"

	a := testSlackAdapter(t, testSlackHandlers{
		userInfo: func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
		},
	})
	a.orchestratorSlackUserID = orchID

	out, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "direct", User: orchID, Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, types.IdentityKindOrchestrator, out[0].Identity.Kind)
	require.Equal(t, "user-"+orchID, out[0].Identity.DisplayName)
}

func TestMapConversationMessagesLookupFailureSyntheticFallback(t *testing.T) {
	t.Parallel()

	const uid = "U0FAIL001"

	a := testSlackAdapter(t, testSlackHandlers{
		userInfo: func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
		},
	})

	out, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "plain", User: uid, Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, types.IdentityKindHuman, out[0].Identity.Kind)
	require.Equal(t, "user-"+uid, out[0].Identity.DisplayName)
}

func TestMapConversationMessagesBotPrefixUnchanged(t *testing.T) {
	t.Parallel()

	a := testMapAdapter("UORCH", nil)

	orchMsg, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "*[Operator] hi", User: "UBOT0001", Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, orchMsg, 1)
	require.Equal(t, types.IdentityKindOrchestrator, orchMsg[0].Identity.Kind)
	require.Equal(t, "Operator", orchMsg[0].Identity.DisplayName)
	require.Equal(t, "hi", orchMsg[0].Body)

	seatMsg, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "[seat-name] hello", User: "UBOT0001", Timestamp: "2.0"},
	}})
	require.NoError(t, err)
	require.Len(t, seatMsg, 1)
	require.Equal(t, types.IdentityKindSeat, seatMsg[0].Identity.Kind)
	require.Equal(t, "seat-name", seatMsg[0].Identity.DisplayName)
	require.Equal(t, "hello", seatMsg[0].Body)
}

func TestMapConversationMessagesEmptyOrchestratorID(t *testing.T) {
	t.Parallel()

	const orchID = "UORCHEMPTY"

	a := testMapAdapter("", map[string]types.UserInfo{
		orchID: {UserID: orchID, DisplayName: "Would Be Operator"},
	})

	out, err := a.mapConversationMessages(context.Background(), []slackapi.Message{{
		Msg: slackapi.Msg{Text: "direct post", User: orchID, Timestamp: "1.0"},
	}})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, types.IdentityKindHuman, out[0].Identity.Kind)
	require.Equal(t, "Would Be Operator", out[0].Identity.DisplayName)
}
