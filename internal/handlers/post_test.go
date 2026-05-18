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

func testMCPClient(t *testing.T, wire func(srv *mcp.Server)) *mcp.ClientSession {
	t.Helper()

	srv := mcp.NewServer(&mcp.Implementation{Name: "huddle-tests", Version: "v0"}, nil)
	wire(srv)

	ct, st := mcp.NewInMemoryTransports()
	_, err := srv.Connect(context.Background(), st, nil)
	require.NoError(t, err)

	cli := mcp.NewClient(&mcp.Implementation{Name: "huddle-tests-cli", Version: "v0"}, nil)
	cs, err := cli.Connect(context.Background(), ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func TestPostSeatHappyPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_post_seat",
		Purpose:                 "p",
		OrchestratorDisplayName: "orch",
		SlackChannelID:          "C-seat-happy",
		SlackChannelName:        "h-seat",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := store.Key{
		Key:         "K_seat_happy",
		HuddleID:    h.ID,
		SeatID:      "s1",
		DisplayName: "ghost",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	fake := &slack.FakeAdapter{ReturnedTS: "99.000001"}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"key":     rec.Key,
			"body":    "hello room",
			"replyTo": "1738458123.000400",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)

	var got types.PostResult
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, "99.000001", got.MessageID)
	require.Equal(t, types.IdentityKindSeat, got.Identity.Kind)
	require.Equal(t, "ghost", got.Identity.DisplayName)
	require.Equal(t, "s1", got.Identity.SeatID)
	require.False(t, got.PostedAt.IsZero())

	require.Len(t, fake.Posts, 1)
	require.Equal(t, "C-seat-happy", fake.Posts[0][0])
	require.Equal(t, "[ghost] hello room", fake.Posts[0][1])
	require.Equal(t, "1738458123.000400", fake.Posts[0][2])
}

func TestPostOrchestratorHappyPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_post_orch",
		Purpose:                 "p",
		OrchestratorDisplayName: "lead operator",
		SlackChannelID:          "C-orch-happy",
		SlackChannelName:        "h-orch",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	fake := &slack.FakeAdapter{}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"huddleId": h.ID,
			"body":     "ops here",
		},
	})
	require.NoError(t, err)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)

	var got types.PostResult
	require.NoError(t, json.Unmarshal(raw, &got))

	require.Equal(t, types.IdentityKindOrchestrator, got.Identity.Kind)
	require.Equal(t, "lead operator", got.Identity.DisplayName)
	require.Empty(t, got.Identity.SeatID)

	require.Len(t, fake.Posts, 1)
	require.Equal(t, "C-orch-happy", fake.Posts[0][0])
	require.Equal(t, "*[lead operator] ops here", fake.Posts[0][1])
}

func TestPostRevokedKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_rev",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-r",
		SlackChannelName:        "h-r",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := store.Key{
		Key:         "K_revoked",
		HuddleID:    h.ID,
		SeatID:      "s9",
		DisplayName: "gone",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))
	require.NoError(t, st.RevokeKey(ctx, rec.Key, now.Add(time.Minute)))

	fake := &slack.FakeAdapter{}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"key":  rec.Key,
			"body": "nope",
		},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
	require.Contains(t, wire.Message, huddleerr.ErrKeyInvalid.Error())
	require.Empty(t, fake.Posts)
}

func TestPostClosedHuddle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 13, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_closed",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-cl",
		SlackChannelName:        "h-cl",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))
	require.NoError(t, st.MarkClosed(ctx, h.ID, now.Add(time.Hour)))

	rec := store.Key{
		Key:         "K_closed",
		HuddleID:    h.ID,
		SeatID:      "s1",
		DisplayName: "seat",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	fake := &slack.FakeAdapter{}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"key":  rec.Key,
			"body": "too late",
		},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
	require.Contains(t, wire.Message, huddleerr.ErrHuddleClosed.Error())
	require.Empty(t, fake.Posts)
}

func TestPostUnknownHuddleOrchestratorPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	fake := &slack.FakeAdapter{}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"huddleId": "hud_nosuch",
			"body":     "x",
		},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInvalidParams), wire.Code)
	require.Contains(t, wire.Message, huddleerr.ErrHuddleNotFound.Error())
}

func TestPostSlackFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, 5, 17, 14, 0, 0, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_slack_fail",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-fail",
		SlackChannelName:        "h-fail",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := store.Key{
		Key:         "K_slack_fail",
		HuddleID:    h.ID,
		SeatID:      "s1",
		DisplayName: "seat",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	fake := &slack.FakeAdapter{PostErr: errors.New("slack exploded")}
	cs := testMCPClient(t, func(srv *mcp.Server) {
		RegisterPost(srv, Deps{Slack: fake, Store: st})
	})

	_, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.post",
		Arguments: map[string]any{
			"key":  rec.Key,
			"body": "boom",
		},
	})
	require.Error(t, err)

	var wire *jsonrpc.Error
	require.ErrorAs(t, err, &wire)
	require.Equal(t, int64(jsonrpc.CodeInternalError), wire.Code)
	require.Contains(t, wire.Message, "slack post")
}
