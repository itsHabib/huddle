package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestCloseHappyPathArchivesAndMarks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	now := time.Now().UTC()
	require.NoError(t, st.InsertHuddle(ctx, types.Huddle{
		ID:                      "hud_live",
		Purpose:                 "retro",
		OrchestratorDisplayName: "facilitator",
		SlackChannelID:          "C-live",
		SlackChannelName:        "#retro",
		CreatedAt:               now,
	}))

	fa := &slack.FakeAdapter{}
	deps := Deps{Slack: fa, Store: st, Cfg: config.Config{}}

	res, execErr := executeClose(ctx, deps, types.CloseArgs{HuddleID: "hud_live"})
	require.NoError(t, execErr)
	require.True(t, res.Closed)
	require.Equal(t, "#retro", res.ArchivedChannel)
	require.Equal(t, []string{"C-live"}, fa.ArchivedIDs)

	after, lerr := st.LookupHuddle(ctx, "hud_live")
	require.NoError(t, lerr)
	require.NotNil(t, after.ClosedAt)
}

func TestCloseIdempotentSkipsSlack(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	closed := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, st.InsertHuddle(ctx, types.Huddle{
		ID:                      "hud_done",
		Purpose:                 "done",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-done",
		SlackChannelName:        "#done",
		CreatedAt:               time.Now().UTC().Add(-2 * time.Hour),
		ClosedAt:                &closed,
	}))

	fa := &slack.FakeAdapter{ArchiveErr: errors.New("slack should not be called")}
	deps := Deps{Slack: fa, Store: st, Cfg: config.Config{}}

	res, execErr := executeClose(ctx, deps, types.CloseArgs{HuddleID: "hud_done"})
	require.NoError(t, execErr)
	require.True(t, res.Closed)
	require.Equal(t, "#done", res.ArchivedChannel)
	require.Empty(t, fa.ArchivedIDs)
}

func TestCloseUnknownHuddleInvalidParams(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	deps := Deps{Slack: &slack.FakeAdapter{}, Store: st, Cfg: config.Config{}}

	_, execErr := executeClose(ctx, deps, types.CloseArgs{HuddleID: "hud_nope"})
	requireRPCCode(t, execErr, jsonrpc.CodeInvalidParams)
	require.Contains(t, execErr.Error(), "huddle not found")
}

func TestCloseSlackArchiveFailsInternalError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.InsertHuddle(ctx, types.Huddle{
		ID:                      "hud_arc",
		Purpose:                 "p",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C-arc",
		SlackChannelName:        "#arc",
		CreatedAt:               time.Now().UTC(),
	}))

	fa := &slack.FakeAdapter{ArchiveErr: errors.New("archive exploded")}
	deps := Deps{Slack: fa, Store: st, Cfg: config.Config{}}

	_, execErr := executeClose(ctx, deps, types.CloseArgs{HuddleID: "hud_arc"})
	requireRPCCode(t, execErr, jsonrpc.CodeInternalError)
}
