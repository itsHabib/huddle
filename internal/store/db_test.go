package store

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewExposesResolvedAbsolutePaths verifies the store reports the resolved
// absolute state dir and DB path it actually opened — the values cmd/huddle
// logs at startup so a wrong HUDDLE_STATE_DIR is never silent.
func TestNewExposesResolvedAbsolutePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	st, err := New(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	require.True(t, filepath.IsAbs(st.StateDir()), "state dir should be absolute")
	require.True(t, filepath.IsAbs(st.DBPath()), "db path should be absolute")
	require.Equal(t, "huddle.sqlite", filepath.Base(st.DBPath()))
	require.Equal(t, st.StateDir(), filepath.Dir(st.DBPath()))
}

// TestNewReportsFreshThenExistingDB verifies CreatedFreshDB is true only when
// New had to create a brand-new database file, and false once the file exists.
// A fresh DB alongside zero huddles is the classic "wrong state dir" symptom.
func TestNewReportsFreshThenExistingDB(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := New(dir)
	require.NoError(t, err)
	require.True(t, first.CreatedFreshDB(), "first open of an empty dir should create a fresh DB")
	require.NoError(t, first.Close())

	second, err := New(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	require.False(t, second.CreatedFreshDB(), "second open should find the existing DB")
}
