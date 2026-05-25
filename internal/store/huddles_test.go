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
		OrchestratorID:          "michael",
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

// TestBackfillOrchestratorID_OnLegacyDB simulates upgrading a DB that
// predates the orchestrator_id column: it creates the huddles table with
// the legacy column set, inserts a row, then runs ApplySchema (which
// triggers backfillColumns) and verifies the new column is present with
// the documented default and that LookupHuddle round-trips correctly.
func TestBackfillOrchestratorID_OnLegacyDB(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	st, err := New(filepath.Join(dir, "legacy"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	_, err = st.db.ExecContext(ctx, `DROP TABLE huddles`)
	require.NoError(t, err)
	_, err = st.db.ExecContext(ctx, `
CREATE TABLE huddles (
  id                          TEXT PRIMARY KEY,
  purpose                     TEXT NOT NULL,
  orchestrator_display_name   TEXT NOT NULL DEFAULT 'orchestrator',
  slack_channel_id            TEXT NOT NULL UNIQUE,
  slack_channel_name          TEXT NOT NULL UNIQUE,
  created_at                  TEXT NOT NULL,
  closed_at                   TEXT,
  ttl_hours                   INTEGER
)`)
	require.NoError(t, err)

	now := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	_, err = st.db.ExecContext(ctx, `
INSERT INTO huddles (id, purpose, orchestrator_display_name, slack_channel_id, slack_channel_name, created_at)
VALUES ('hud_legacy', 'old purpose', 'legacy-orch', 'C-legacy', 'h-legacy', ?)`, now)
	require.NoError(t, err)

	require.NoError(t, st.ApplySchema(ctx))

	got, err := st.LookupHuddle(ctx, "hud_legacy")
	require.NoError(t, err)
	require.Equal(t, "orchestrator", got.OrchestratorID, "backfilled column should default to 'orchestrator'")
	require.Equal(t, "legacy-orch", got.OrchestratorDisplayName)

	// Second ApplySchema is a no-op for backfill — column already present.
	require.NoError(t, st.ApplySchema(ctx))
}

func matchHuddle(t *testing.T, want, got types.Huddle) {
	t.Helper()

	require.Equal(t, want.ID, got.ID)
	require.Equal(t, want.Purpose, got.Purpose)
	require.Equal(t, want.OrchestratorID, got.OrchestratorID)
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
