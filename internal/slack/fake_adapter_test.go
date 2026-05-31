package slack

import (
	"context"
	"testing"

	"github.com/itsHabib/huddle/internal/types"

	"github.com/stretchr/testify/require"
)

func TestFakeAdapterCapturesRenderedPostMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ad := FakeAdapter{}

	text := Encode(types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: "orch"}, "hello")
	ts, err := ad.PostMessage(ctx, "C-demo", text, "")
	require.NoError(t, err)
	require.Equal(t, "1738458123.000456", ts)
	require.Len(t, ad.Posts, 1)
	require.Equal(t, "C-demo", ad.Posts[0][0])
	require.Equal(t, text, ad.Posts[0][1])
	require.Empty(t, ad.Posts[0][2])
}

func TestFakeAdapterHistoryPassesThrough(t *testing.T) {
	t.Parallel()

	msgs := []types.Message{{ID: "1", Body: "b"}}
	ad := FakeAdapter{Hist: msgs}
	out, err := ad.History(context.Background(), "C-any", nil, 2)
	require.NoError(t, err)
	require.Len(t, out, 1)
}

func TestFakeAdapterLookupUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ad := FakeAdapter{
		UsersByRef: map[string]types.UserInfo{
			"U0FAKE01": {UserID: "U0FAKE01", DisplayName: "Pat"},
		},
	}

	info, err := ad.LookupUser(ctx, "U0FAKE01")
	require.NoError(t, err)
	require.Equal(t, "Pat", info.DisplayName)
	require.Len(t, ad.LookupUserCalls, 1)
	require.Equal(t, "U0FAKE01", ad.LookupUserCalls[0].Ref)

	_, err = ad.LookupUser(ctx, "U0MISSING")
	require.ErrorIs(t, err, ErrUserNotFound)

	ad.LookupUserErr = ErrNoToken
	_, err = ad.LookupUser(ctx, "U0FAKE01")
	require.ErrorIs(t, err, ErrNoToken)
}

func TestFakeAdapterListChannelMembers(t *testing.T) {
	t.Parallel()

	ad := FakeAdapter{ChannelMembers: map[string][]string{"C1": {"U1", "U2"}}}
	members, err := ad.ListChannelMembers(context.Background(), "C1")
	require.NoError(t, err)
	require.Equal(t, []string{"U1", "U2"}, members)

	ad.ListChannelMembersErr = ErrRateLimited
	_, err = ad.ListChannelMembers(context.Background(), "C1")
	require.ErrorIs(t, err, ErrRateLimited)
}

func TestFakeAdapterBotUserID(t *testing.T) {
	t.Parallel()

	ad := FakeAdapter{BotUserIDValue: "UBOTFAKE"}
	require.Equal(t, "UBOTFAKE", ad.BotUserID())
}

func TestFakeAdapterHistorySkipsSystemSubTypes(t *testing.T) {
	t.Parallel()

	ad := FakeAdapter{Hist: []types.Message{
		{ID: "1", SubType: "channel_join", Body: "ignored"},
		{ID: "2", Body: "hi", Identity: types.Identity{Kind: types.IdentityKindSeat, DisplayName: "s"}},
	}}
	out, err := ad.History(context.Background(), "C-any", nil, 10)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "2", out[0].ID)
}
