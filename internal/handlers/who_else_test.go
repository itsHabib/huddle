package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/handlers"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestWhoElse_OK_includesCallerSeatAndPeers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_who_else",
		Purpose:                 "sync",
		OrchestratorID:          "michael",
		OrchestratorDisplayName: "lead",
		SlackChannelID:          "C1",
		SlackChannelName:        "huddle-sync",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	keys := []store.Key{
		{Key: "KEY_A", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base},
		{Key: "KEY_B", HuddleID: h.ID, SeatID: "s2", DisplayName: "two", CreatedAt: base.Add(time.Minute)},
		{Key: "KEY_C", HuddleID: h.ID, SeatID: "s3", DisplayName: "three", CreatedAt: base.Add(2 * time.Minute)},
	}
	for _, k := range keys {
		require.NoError(t, st.InsertKey(ctx, k))
	}

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT"}},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_A"},
	})
	require.NoError(t, callErr)
	require.False(t, res.IsError)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, "sync", got.Purpose)
	require.Equal(t, types.Seat{ID: "michael", DisplayName: "lead"}, got.Orchestrator)
	require.ElementsMatch(t, []types.Seat{
		{ID: "s1", DisplayName: "one"},
		{ID: "s2", DisplayName: "two"},
		{ID: "s3", DisplayName: "three"},
	}, got.Seats)
	require.NotNil(t, got.Humans)
	require.Empty(t, got.Humans)
}

func TestWhoElse_invalidParams_revokedKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_rev",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	k := store.Key{Key: "KEY_REV", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}
	require.NoError(t, st.InsertKey(ctx, k))
	require.NoError(t, st.RevokeKey(ctx, k.Key, base.Add(time.Hour)))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT"}},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	_, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_REV"},
	})
	require.Error(t, callErr)

	var wire *jsonrpc.Error
	require.True(t, errors.As(callErr, &wire))
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
	require.Equal(t, huddleerr.ErrKeyInvalid.Error(), wire.Message)
}

func TestWhoElse_invalidParams_unknownKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT"}},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	_, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_MISSING"},
	})
	require.Error(t, callErr)

	var wire *jsonrpc.Error
	require.True(t, errors.As(callErr, &wire))
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
}

func TestWhoElse_activeSeats_excludesRevokedSeatFromList(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_partial",
		Purpose:                 "y",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	k1 := store.Key{Key: "KEY_1", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}
	k2 := store.Key{Key: "KEY_2", HuddleID: h.ID, SeatID: "s2", DisplayName: "two", CreatedAt: base.Add(time.Minute)}
	k3 := store.Key{Key: "KEY_3", HuddleID: h.ID, SeatID: "s3", DisplayName: "three", CreatedAt: base.Add(2 * time.Minute)}
	require.NoError(t, st.InsertKey(ctx, k1))
	require.NoError(t, st.InsertKey(ctx, k2))
	require.NoError(t, st.InsertKey(ctx, k3))

	require.NoError(t, st.RevokeKey(ctx, k2.Key, base.Add(3*time.Minute)))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT"}},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_1"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))

	require.ElementsMatch(t, []types.Seat{
		{ID: "s1", DisplayName: "one"},
		{ID: "s3", DisplayName: "three"},
	}, got.Seats)
	require.NotNil(t, got.Humans)
	require.Empty(t, got.Humans)
}

func TestWhoElse_noHumans_botOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_no_humans",
		Purpose:                 "solo",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_SOLO", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT"}},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_SOLO"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.Humans)
	require.Empty(t, got.Humans)
}

func TestWhoElse_oneHuman(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_one_human",
		Purpose:                 "sync",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_H", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT", "U_human"}},
			UsersByRef: map[string]types.UserInfo{
				"U_human": {UserID: "U_human", DisplayName: "Joe Smith"},
			},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_H"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, []types.Human{{
		SlackUserID: "U_human",
		DisplayName: "Joe Smith",
		Kind:        types.IdentityKindHuman,
	}}, got.Humans)
}

func TestWhoElse_dropsBotMembers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_drop_bot",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_B", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT", "U_other_bot"}},
			UsersByRef: map[string]types.UserInfo{
				"U_other_bot": {UserID: "U_other_bot", DisplayName: "Other Bot", IsBot: true},
			},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_B"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Empty(t, got.Humans)
}

func TestWhoElse_dropsDeactivated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_deact",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_D", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT", "U_gone"}},
			UsersByRef: map[string]types.UserInfo{
				"U_gone": {UserID: "U_gone", DisplayName: "Former", Deactivated: true},
			},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_D"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Empty(t, got.Humans)
}

func TestWhoElse_orchestratorNotDoubleCounted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_orch",
		Purpose:                 "x",
		OrchestratorID:          "michael",
		OrchestratorDisplayName: "lead",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_O", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Cfg:   config.Config{OrchestratorSlackUserID: "U_orch"},
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT", "U_orch", "U_peer"}},
			UsersByRef: map[string]types.UserInfo{
				"U_orch": {UserID: "U_orch", DisplayName: "lead"},
				"U_peer": {UserID: "U_peer", DisplayName: "Peer"},
			},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_O"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, []types.Human{{
		SlackUserID: "U_peer",
		DisplayName: "Peer",
		Kind:        types.IdentityKindHuman,
	}}, got.Humans)
}

func TestWhoElse_listMembersError_internalError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_list_err",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_E", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{ListChannelMembersErr: slack.ErrRateLimited},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	_, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_E"},
	})
	require.Error(t, callErr)

	var wire *jsonrpc.Error
	require.True(t, errors.As(callErr, &wire))
	require.Equal(t, int64(jsonrpc.CodeInternalError), wire.Code)
}

func TestWhoElse_tokenless_humansEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_tokenless",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_T", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{ListChannelMembersErr: slack.ErrNoToken},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_T"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.Humans)
	require.Empty(t, got.Humans)
	require.Len(t, got.Seats, 1)
}

func TestWhoElse_lookupErrorSkipsMember(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_lookup_skip",
		Purpose:                 "x",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "ch",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{Key: "KEY_L", HuddleID: h.ID, SeatID: "s1", DisplayName: "one", CreatedAt: base}))

	deps := handlers.Deps{
		Store: st,
		Slack: &slack.FakeAdapter{
			BotUserIDValue: "UBOT",
			ChannelMembers: map[string][]string{"C1": {"UBOT", "U_bad", "U_good"}},
			UsersByRef: map[string]types.UserInfo{
				"U_good": {UserID: "U_good", DisplayName: "Good"},
			},
		},
	}
	cs := newToolSession(t, registerWhoElse(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.who_else",
		Arguments: map[string]any{"key": "KEY_L"},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got types.WhoElseResult
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, []types.Human{{
		SlackUserID: "U_good",
		DisplayName: "Good",
		Kind:        types.IdentityKindHuman,
	}}, got.Humans)
}
