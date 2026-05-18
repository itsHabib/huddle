package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/stretchr/testify/require"
)

func TestHuddlesCRUD_Memory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	want := types.Huddle{
		ID:                      "hud_test_1",
		Purpose:                 "brainstorm notes",
		OrchestratorDisplayName: "ops",
		SlackChannelID:          "C0123",
		SlackChannelName:        "huddle-brain",
		CreatedAt:               now,
		TTLHours:                ptr(24),
	}

	require.NoError(t, st.InsertHuddle(ctx, want))

	got, err := st.LookupHuddle(ctx, want.ID)
	require.NoError(t, err)
	matchHuddle(t, want, got)

	got2, err := st.LookupHuddle(ctx, "missing")
	require.Equal(t, huddleerr.ErrHuddleNotFound, err)

	huddleEmpty := types.Huddle{}
	require.Equal(t, huddleEmpty, got2)

	all, err := st.ListHuddles(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 1)

	matchHuddle(t, want, all[0])

	active, err := st.ListHuddles(ctx, true)
	require.NoError(t, err)
	require.Len(t, active, 1)

	closeTime := now.Add(time.Hour)
	require.NoError(t, st.MarkClosed(ctx, want.ID, closeTime))

	want.ClosedAt = &closeTime
	gotClosed, err := st.LookupHuddle(ctx, want.ID)
	require.NoError(t, err)
	matchHuddle(t, want, gotClosed)

	active, err = st.ListHuddles(ctx, true)
	require.NoError(t, err)
	require.Empty(t, active)

	all2, err := st.ListHuddles(ctx, false)
	require.NoError(t, err)
	require.Len(t, all2, 1)
	matchHuddle(t, want, all2[0])

	require.NoError(t, st.ApplySchema(ctx))

	require.Equal(t, huddleerr.ErrHuddleNotFound, st.MarkClosed(ctx, "nosuch", time.Now()))
}

func TestHuddlesOnDisk_IdempotentSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	st1, err := New(filepath.Join(dir, "state"))
	require.NoError(t, err)

	require.NoError(t, st1.ApplySchema(ctx))
	require.NoError(t, st1.Close())

	st2, err := New(filepath.Join(dir, "state"))
	require.NoError(t, err)
	require.NoError(t, st2.Close())
}

func matchHuddle(t *testing.T, want, got types.Huddle) {
	t.Helper()

	require.Equal(t, want.ID, got.ID)
	require.Equal(t, want.Purpose, got.Purpose)
	require.Equal(t, want.OrchestratorDisplayName, got.OrchestratorDisplayName)
	require.Equal(t, want.SlackChannelID, got.SlackChannelID)
	require.Equal(t, want.SlackChannelName, got.SlackChannelName)

	require.True(t, want.CreatedAt.UTC().Equal(got.CreatedAt.UTC()))

	// TTL round-trip is independent of ClosedAt — verify on both paths.
	switch want.TTLHours {
	case nil:
		require.Nil(t, got.TTLHours)
	default:
		require.NotNil(t, got.TTLHours)
		require.Equal(t, *want.TTLHours, *got.TTLHours)
	}

	if want.ClosedAt == nil {
		require.Nil(t, got.ClosedAt)

		return
	}

	require.NotNil(t, got.ClosedAt)
	require.True(t, want.ClosedAt.UTC().Equal(got.ClosedAt.UTC()))
}

func ptr(v int) *int { return &v }
