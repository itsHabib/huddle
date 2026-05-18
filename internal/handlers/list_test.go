package handlers_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/handlers"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestList_emptyStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	deps := handlers.Deps{Store: st}
	cs := newToolSession(t, registerList(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.list",
		Arguments: map[string]any{},
	})
	require.NoError(t, callErr)
	require.False(t, res.IsError)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var got []types.Huddle
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Empty(t, got)
}

func TestList_activeFilter_andOrder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	huddles := []types.Huddle{
		{ID: "h_a", Purpose: "a", OrchestratorDisplayName: "o", SlackChannelID: "C1", SlackChannelName: "n1", CreatedAt: t0},
		{ID: "h_b", Purpose: "b", OrchestratorDisplayName: "o", SlackChannelID: "C2", SlackChannelName: "n2", CreatedAt: t0.Add(time.Hour)},
		{ID: "h_c", Purpose: "c", OrchestratorDisplayName: "o", SlackChannelID: "C3", SlackChannelName: "n3", CreatedAt: t0.Add(2 * time.Hour)},
	}
	for _, h := range huddles {
		require.NoError(t, st.InsertHuddle(ctx, h))
	}

	closeTime := t0.Add(10 * time.Hour)
	require.NoError(t, st.MarkClosed(ctx, "h_b", closeTime))

	deps := handlers.Deps{Store: st}
	cs := newToolSession(t, registerList(deps))

	resAll, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.list",
		Arguments: map[string]any{"active": false},
	})
	require.NoError(t, callErr)

	rawAll, mErr := json.Marshal(resAll.StructuredContent)
	require.NoError(t, mErr)

	var gotAll []types.Huddle
	require.NoError(t, json.Unmarshal(rawAll, &gotAll))
	require.Len(t, gotAll, 3)
	// store.ListHuddles orders by created_at ASC (oldest first)
	require.Equal(t, []string{"h_a", "h_b", "h_c"}, []string{gotAll[0].ID, gotAll[1].ID, gotAll[2].ID})

	resActive, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.list",
		Arguments: map[string]any{"active": true},
	})
	require.NoError(t, err)

	rawActive, mErr2 := json.Marshal(resActive.StructuredContent)
	require.NoError(t, mErr2)

	var gotActive []types.Huddle
	require.NoError(t, json.Unmarshal(rawActive, &gotActive))
	require.Len(t, gotActive, 2)
	require.Equal(t, []string{"h_a", "h_c"}, []string{gotActive[0].ID, gotActive[1].ID})
}

func TestList_responseShape_hasNoKeyMaterial(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "h_keys",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C1",
		SlackChannelName:        "n",
		CreatedAt:               base,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.InsertKey(ctx, store.Key{
		Key: "SECRET_K", HuddleID: h.ID, SeatID: "s1", DisplayName: "bot", CreatedAt: base,
	}))

	deps := handlers.Deps{Store: st}
	cs := newToolSession(t, registerList(deps))

	res, callErr := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.list",
		Arguments: map[string]any{},
	})
	require.NoError(t, callErr)

	raw, mErr := json.Marshal(res.StructuredContent)
	require.NoError(t, mErr)

	var rows []map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Len(t, rows, 1)

	row := rows[0]
	_, hasKey := row["key"]
	require.False(t, hasKey)
	_, hasKeys := row["keys"]
	require.False(t, hasKeys)

	var typed []types.Huddle
	require.NoError(t, json.Unmarshal(raw, &typed))
	require.Len(t, typed, 1)
	require.Equal(t, h.ID, typed[0].ID)
}
