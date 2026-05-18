package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/handlers"
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

	deps := handlers.Deps{Store: st}
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
	require.Equal(t, "lead", got.Orchestrator.DisplayName)
	require.ElementsMatch(t, []types.Seat{
		{ID: "s1", DisplayName: "one"},
		{ID: "s2", DisplayName: "two"},
		{ID: "s3", DisplayName: "three"},
	}, got.Seats)
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

	deps := handlers.Deps{Store: st}
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

	deps := handlers.Deps{Store: st}
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

	deps := handlers.Deps{Store: st}
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
}
