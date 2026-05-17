package store

import (
	"context"
	"testing"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/stretchr/testify/require"
)

func TestKeysRoundTrip_Memory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	h := types.Huddle{
		ID:                      "hud_k1",
		Purpose:                 "keys",
		OrchestratorDisplayName: "orch",
		SlackChannelID:          "C55",
		SlackChannelName:        "h-key",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := Key{
		Key:         "K_hud_a_seat_xyz",
		HuddleID:    h.ID,
		SeatID:      "s1",
		DisplayName: "alpha",
		CreatedAt:   now.Add(time.Minute),
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	got, err := st.LookupKey(ctx, rec.Key)
	require.NoError(t, err)

	require.Equal(t, rec.Key, got.Key)
	require.Equal(t, rec.HuddleID, got.HuddleID)
	require.Equal(t, rec.SeatID, got.SeatID)
	require.Equal(t, rec.DisplayName, got.DisplayName)
	require.True(t, rec.CreatedAt.UTC().Equal(got.CreatedAt.UTC()))
	require.Nil(t, got.RevokedAt)

	seats, err := st.ListSeats(ctx, h.ID)
	require.NoError(t, err)
	require.Equal(t, []types.Seat{{ID: rec.SeatID, DisplayName: rec.DisplayName}}, seats)

	ev := now.Add(2 * time.Hour)
	require.NoError(t, st.RevokeKey(ctx, rec.Key, ev))

	_, lkErr := st.LookupKey(ctx, rec.Key)
	require.Equal(t, huddleerr.ErrKeyInvalid, lkErr)

	active, err := st.ListSeats(ctx, h.ID)
	require.NoError(t, err)
	require.Empty(t, active)
}

func TestLookupKey_Invalid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	_, lkErr := st.LookupKey(ctx, "ghost")
	require.Equal(t, huddleerr.ErrKeyInvalid, lkErr)

	require.Equal(t, huddleerr.ErrKeyInvalid, st.RevokeKey(ctx, "ghost", time.Now()))
}

func TestCascadeDelete_RemovesKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	st, err := OpenMemory(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	now := time.Now().UTC()
	h := types.Huddle{
		ID:                      "hud_cascade",
		Purpose:                 "del",
		OrchestratorDisplayName: "o",
		SlackChannelID:          "C99",
		SlackChannelName:        "h99",
		CreatedAt:               now,
	}
	require.NoError(t, st.InsertHuddle(ctx, h))

	rec := Key{
		Key:         "k1",
		HuddleID:    h.ID,
		SeatID:      "s",
		DisplayName: "d",
		CreatedAt:   now,
	}
	require.NoError(t, st.InsertKey(ctx, rec))

	_, err = st.db.ExecContext(ctx, `DELETE FROM huddles WHERE id = ?`, h.ID)
	require.NoError(t, err)

	_, lkErr := st.LookupKey(ctx, rec.Key)
	require.Equal(t, huddleerr.ErrKeyInvalid, lkErr)
}
