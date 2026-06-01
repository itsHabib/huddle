package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func seedHuddle(t *testing.T, st *store.Store, channelID string) types.Huddle {
	t.Helper()

	ctx := context.Background()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_invite",
		Purpose:                 "invite test",
		OrchestratorDisplayName: "o",
		SlackChannelID:          channelID,
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	return h
}

func TestInviteHuman_happy(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	channelID := "C_invite"
	h := seedHuddle(t, st, channelID)

	fa := &slack.FakeAdapter{
		Chan: slack.Channel{ID: channelID, Name: "ch"},
		UsersByRef: map[string]types.UserInfo{
			"U_bob": {UserID: "U_bob", DisplayName: "Bob"},
		},
		ChannelMembers: map[string][]string{channelID: {}},
	}
	deps := Deps{Slack: fa, Store: st}

	res, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   []string{"U_bob"},
	})
	require.NoError(t, execErr)
	require.Equal(t, []types.Human{{
		SlackUserID: "U_bob",
		DisplayName: "Bob",
		Kind:        types.IdentityKindHuman,
	}}, res.Invited)
	require.Empty(t, res.Skipped)
	require.Len(t, fa.Invites, 1)
	require.Equal(t, channelID, fa.Invites[0][0])
	require.Equal(t, "U_bob", fa.Invites[0][1])
}

func TestInviteHuman_missingHuddle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	deps := Deps{Slack: &slack.FakeAdapter{}, Store: st}

	_, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: "hud_missing",
		Humans:   []string{"U_bob"},
	})
	requireRPCCode(t, execErr, jsonrpc.CodeInvalidParams)

	var wire *jsonrpc.Error
	require.ErrorAs(t, execErr, &wire)
	require.Contains(t, wire.Message, huddleerr.ErrHuddleNotFound.Error())
}

func TestInviteHuman_emptyHumans(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	h := seedHuddle(t, st, "C1")
	deps := Deps{Slack: &slack.FakeAdapter{}, Store: st}

	_, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   nil,
	})
	requireRPCCode(t, execErr, jsonrpc.CodeInvalidParams)
}

func TestInviteHuman_alreadyInChannel(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	channelID := "C_member"
	h := seedHuddle(t, st, channelID)

	fa := &slack.FakeAdapter{
		UsersByRef: map[string]types.UserInfo{
			"U_bob": {UserID: "U_bob", DisplayName: "Bob"},
		},
		ChannelMembers: map[string][]string{channelID: {"U_bob"}},
	}
	deps := Deps{Slack: fa, Store: st}

	res, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   []string{"U_bob"},
	})
	require.NoError(t, execErr)
	require.Empty(t, res.Invited)
	require.Equal(t, []types.SkippedHuman{{
		Ref:    "U_bob",
		Reason: types.SkippedReasonAlreadyInChannel,
	}}, res.Skipped)
	require.Empty(t, fa.Invites)
}

func TestInviteHuman_inviteFailed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	channelID := "C_fail"
	h := seedHuddle(t, st, channelID)

	fa := &slack.FakeAdapter{
		InviteErr: errors.New("invite_denied"),
		UsersByRef: map[string]types.UserInfo{
			"U_bob": {UserID: "U_bob", DisplayName: "Bob"},
		},
		ChannelMembers: map[string][]string{channelID: {}},
	}
	deps := Deps{Slack: fa, Store: st}

	res, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   []string{"U_bob"},
	})
	require.NoError(t, execErr)
	require.Empty(t, res.Invited)
	require.Equal(t, []types.SkippedHuman{{
		Ref:    "U_bob",
		Reason: types.SkippedReasonInviteFailed,
	}}, res.Skipped)
}

func TestInviteHuman_missingEmailScope(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	channelID := "C_email"
	h := seedHuddle(t, st, channelID)

	fa := &slack.FakeAdapter{
		LookupUserErrByRef: map[string]error{
			"alice@example.com": slack.ErrMissingEmailScope,
		},
		ChannelMembers: map[string][]string{channelID: {}},
	}
	deps := Deps{Slack: fa, Store: st}

	res, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   []string{"alice@example.com"},
	})
	require.NoError(t, execErr)
	require.Empty(t, res.Invited)
	require.Equal(t, []types.SkippedHuman{{
		Ref:    "alice@example.com",
		Reason: types.SkippedReasonMissingEmailScope,
	}}, res.Skipped)
}

func TestInviteHuman_tokenless(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	channelID := "C_tokenless"
	h := seedHuddle(t, st, channelID)

	fa := &slack.FakeAdapter{
		ListChannelMembersErr: slack.ErrNoToken,
		LookupUserErr:         slack.ErrNoToken,
	}
	deps := Deps{Slack: fa, Store: st}

	res, execErr := executeInviteHuman(ctx, deps, types.InviteHumanArgs{
		HuddleID: h.ID,
		Humans:   []string{"U_bob", "U_carol"},
	})
	require.NoError(t, execErr)
	require.Empty(t, res.Invited)
	require.Len(t, res.Skipped, 2)
	for _, skip := range res.Skipped {
		require.Equal(t, types.SkippedReasonInviteFailed, skip.Reason)
	}
}
