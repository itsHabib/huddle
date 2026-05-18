package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestReadHappyMixedIdentities(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 15, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_read_mix",
		Purpose:                 "p",
		OrchestratorDisplayName: "orch",
		SlackChannelID:          "C-read",
		SlackChannelName:        "h-read",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := store.Key{
		Key:         "K_read_mix",
		HuddleID:    h.ID,
		SeatID:      "seat-a",
		DisplayName: "alpha",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	posted := now.Add(time.Minute)
	want := []types.Message{
		{
			ID: "1.000001", PostedAt: posted, Body: "from seat",
			Identity: types.Identity{Kind: types.IdentityKindSeat, DisplayName: "alpha", SeatID: "seat-a"},
		},
		{
			ID: "1.000002", PostedAt: posted.Add(time.Second), Body: "from orch",
			Identity: types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: "orch"},
		},
		{
			ID: "1.000003", PostedAt: posted.Add(2 * time.Second), Body: "human said hi",
			Identity: types.Identity{Kind: types.IdentityKindHuman, DisplayName: "user-U111"},
		},
		{
			ID: "1.000004", PostedAt: posted.Add(3 * time.Second), Body: "weird",
			Identity: types.Identity{Kind: types.IdentityKindUnknown, DisplayName: ""},
		},
	}

	fake := &slack.FakeAdapter{Hist: want}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterRead(srv, Deps{Slack: fake, Store: st})
	})

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.read",
		Arguments: map[string]any{
			"key": rec.Key,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)

	var envelope types.ReadResult
	require.NoError(t, json.Unmarshal(raw, &envelope))
	got := envelope.Messages
	require.Len(t, got, len(want))

	for i := range want {
		require.Equal(t, want[i].ID, got[i].ID)
		require.Equal(t, want[i].Body, got[i].Body)
		require.Equal(t, want[i].Identity, got[i].Identity)
	}
}

func TestReadFiltersSystemSubTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 16, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_read_sys",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-sys",
		SlackChannelName:        "h-sys",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	fake := &slack.FakeAdapter{Hist: []types.Message{
		{ID: "x", SubType: "channel_join", Body: "join noise"},
		{ID: "y", Body: "real", Identity: types.Identity{Kind: types.IdentityKindSeat, DisplayName: "s"}},
	}}

	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterRead(srv, Deps{Slack: fake, Store: st})
	})

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.read",
		Arguments: map[string]any{"huddleId": h.ID},
	})
	require.NoError(t, err)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)

	var envelope types.ReadResult
	require.NoError(t, json.Unmarshal(raw, &envelope))
	require.Len(t, envelope.Messages, 1)
	require.Equal(t, "y", envelope.Messages[0].ID)
}

func TestReadSlackHistoryFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 17, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_read_err",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-err",
		SlackChannelName:        "h-err",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	fake := &slack.FakeAdapter{HistErr: errors.New("history unavailable")}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterRead(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.read",
		Arguments: map[string]any{"huddleId": h.ID},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInternalError), wire.Code)
	require.Contains(t, wire.Message, "slack history")
}

func TestReadUnknownHuddle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	fake := &slack.FakeAdapter{}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterRead(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "huddle.read",
		Arguments: map[string]any{"huddleId": "missing"},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
	require.Contains(t, wire.Message, huddleerr.ErrHuddleNotFound.Error())
}
