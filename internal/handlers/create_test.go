package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

type retryCreateAdapter struct {
	slack.FakeAdapter
}

func (r *retryCreateAdapter) CreateChannel(_ context.Context, base string) (slack.Channel, error) {
	// Simulates slackGoAdapter: first conversations.create returns name_taken, retry succeeds.
	r.CreatedNames = append(r.CreatedNames, base)
	winName := base + "-retrywin"
	r.CreatedNames = append(r.CreatedNames, winName)

	return slack.Channel{ID: "C-won", Name: winName}, nil
}

type doubleNameTakenAdapter struct {
	slack.FakeAdapter
}

func (d *doubleNameTakenAdapter) CreateChannel(_ context.Context, base string) (slack.Channel, error) {
	d.CreatedNames = append(d.CreatedNames, base, base+"-sfx")

	return slack.Channel{}, &slackapi.SlackErrorResponse{Err: "name_taken"}
}

func TestCreateHappyPathTwoSeats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	fa := &slack.FakeAdapter{}
	deps := Deps{
		Slack: fa,
		Store: st,
		Cfg:   config.Config{ChannelPrefix: "hu-"},
	}

	args := types.CreateArgs{
		Purpose:      "Sprint Planning!",
		Orchestrator: types.Seat{ID: "michael", DisplayName: "lead"},
		Seats: []types.SeatDefinition{
			{ID: "seat-a", DisplayName: "agent-a"},
			{ID: "seat-b", DisplayName: "agent-b"},
		},
	}

	res, execErr := executeCreate(ctx, deps, args)
	require.NoError(t, execErr)
	require.NotEmpty(t, res.HuddleID)
	require.Equal(t, types.Seat{ID: "michael", DisplayName: "lead"}, res.Orchestrator)
	require.Len(t, res.Seats, 2)
	require.Len(t, fa.CreatedNames, 1)
	require.Contains(t, fa.CreatedNames[0], "hu-sprint-planning-")
	require.Equal(t, res.Channel, fa.CreatedNames[0])

	h, lerr := st.LookupHuddle(ctx, res.HuddleID)
	require.NoError(t, lerr)
	require.Equal(t, "Sprint Planning!", h.Purpose)
	require.Equal(t, "michael", h.OrchestratorID)
	require.Equal(t, "lead", h.OrchestratorDisplayName)

	keyA := res.Seats[0].Key
	keyB := res.Seats[1].Key
	require.NotEqual(t, keyA, keyB)
	require.Contains(t, keyA, "seat-a")
	require.Contains(t, keyB, "seat-b")

	rowA, kerr := st.LookupKey(ctx, keyA)
	require.NoError(t, kerr)
	require.Equal(t, res.HuddleID, rowA.HuddleID)

	rowB, kerr := st.LookupKey(ctx, keyB)
	require.NoError(t, kerr)
	require.Equal(t, res.HuddleID, rowB.HuddleID)
}

func TestCreateSlackNameTakenRetrySucceeds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ad := &retryCreateAdapter{}

	deps := Deps{Slack: ad, Store: st, Cfg: config.Config{ChannelPrefix: "huddle-"}}
	args := types.CreateArgs{
		Purpose: "x",
		Seats: []types.SeatDefinition{
			{ID: "s1", DisplayName: "one"},
		},
	}

	res, execErr := executeCreate(ctx, deps, args)
	require.NoError(t, execErr)
	require.Len(t, ad.CreatedNames, 2)
	require.Equal(t, ad.CreatedNames[1], res.Channel)
}

func TestCreateStorageFailureOnInsertHuddle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	require.NoError(t, st.InsertHuddle(ctx, types.Huddle{
		ID:                      "hud_pre",
		Purpose:                 "old",
		OrchestratorDisplayName: "orch",
		SlackChannelID:          "C-collide",
		SlackChannelName:        "h-old",
		CreatedAt:               time.Now().UTC(),
	}))

	fa := &slack.FakeAdapter{Chan: slack.Channel{ID: "C-collide", Name: "h-new"}}

	deps := Deps{Slack: fa, Store: st, Cfg: config.Config{ChannelPrefix: "huddle-"}}
	args := types.CreateArgs{
		Purpose: "new-topic",
		Seats:   []types.SeatDefinition{{ID: "a", DisplayName: "A"}},
	}

	_, execErr := executeCreate(ctx, deps, args)
	requireRPCCode(t, execErr, jsonrpc.CodeInternalError)
	require.Contains(t, strings.ToLower(execErr.Error()), "storage")
}

func TestCreateEmptySeatsInvalidParams(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	deps := Deps{Slack: &slack.FakeAdapter{}, Store: st, Cfg: config.Config{ChannelPrefix: "huddle-"}}
	_, execErr := executeCreate(ctx, deps, types.CreateArgs{Purpose: "p", Seats: nil})
	requireRPCCode(t, execErr, jsonrpc.CodeInvalidParams)
}

func TestCreateSlackCollisionAfterRetryInternalError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	ad := &doubleNameTakenAdapter{}
	deps := Deps{Slack: ad, Store: st, Cfg: config.Config{ChannelPrefix: "huddle-"}}
	_, execErr := executeCreate(ctx, deps, types.CreateArgs{
		Purpose: "p",
		Seats:   []types.SeatDefinition{{ID: "x", DisplayName: "y"}},
	})
	requireRPCCode(t, execErr, jsonrpc.CodeInternalError)
}

func TestCreateInvitesConfiguredOrchestrator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	fa := &slack.FakeAdapter{}
	deps := Deps{
		Slack: fa,
		Store: st,
		Cfg: config.Config{
			ChannelPrefix:           "huddle-",
			OrchestratorSlackUserID: "U0ABC123",
		},
	}

	_, execErr := executeCreate(ctx, deps, types.CreateArgs{
		Purpose: "p",
		Seats:   []types.SeatDefinition{{ID: "s1", DisplayName: "one"}},
	})
	require.NoError(t, execErr)
	require.Len(t, fa.Invites, 1)
	require.Equal(t, fa.Chan.ID, fa.Invites[0][0])
	require.Equal(t, "U0ABC123", fa.Invites[0][1])
}

func TestCreateSkipsInviteWhenNotConfigured(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	fa := &slack.FakeAdapter{}
	deps := Deps{Slack: fa, Store: st, Cfg: config.Config{ChannelPrefix: "huddle-"}}

	_, execErr := executeCreate(ctx, deps, types.CreateArgs{
		Purpose: "p",
		Seats:   []types.SeatDefinition{{ID: "s1", DisplayName: "one"}},
	})
	require.NoError(t, execErr)
	require.Empty(t, fa.Invites)
}

func TestCreateInviteFailureDoesNotFailCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := store.OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	fa := &slack.FakeAdapter{InviteErr: errors.New("user_not_found")}
	deps := Deps{
		Slack: fa,
		Store: st,
		Cfg: config.Config{
			ChannelPrefix:           "huddle-",
			OrchestratorSlackUserID: "U0BADID",
		},
	}

	res, execErr := executeCreate(ctx, deps, types.CreateArgs{
		Purpose: "p",
		Seats:   []types.SeatDefinition{{ID: "s1", DisplayName: "one"}},
	})
	require.NoError(t, execErr)
	require.NotEmpty(t, res.HuddleID)
	require.Len(t, fa.Invites, 1)
}

func requireRPCCode(t *testing.T, err error, want int64) {
	t.Helper()

	var jerr *jsonrpc.Error
	require.ErrorAs(t, err, &jerr)
	require.Equal(t, want, jerr.Code)
}
