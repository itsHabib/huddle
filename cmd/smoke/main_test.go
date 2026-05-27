package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvWithOverridesReplacesExistingKeys(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HUDDLE_STATE_DIR=/real/state",
		"HUDDLE_CHANNEL_PREFIX=huddle-",
		"OTHER=value",
	}

	out := envWithOverrides(base, map[string]string{
		"HUDDLE_STATE_DIR":      "/tmp/smoke",
		"HUDDLE_CHANNEL_PREFIX": "huddle-smoke-",
	})

	require.NotContains(t, out, "HUDDLE_STATE_DIR=/real/state", "base override target must be removed before re-adding")
	require.NotContains(t, out, "HUDDLE_CHANNEL_PREFIX=huddle-", "base override target must be removed before re-adding")

	require.Contains(t, out, "HUDDLE_STATE_DIR=/tmp/smoke")
	require.Contains(t, out, "HUDDLE_CHANNEL_PREFIX=huddle-smoke-")
	require.Contains(t, out, "PATH=/usr/bin", "non-overridden keys must pass through")
	require.Contains(t, out, "OTHER=value", "non-overridden keys must pass through")

	// Each overridden key appears exactly once; getenv would return the
	// override regardless of scan order.
	require.Equal(t, 1, count(out, "HUDDLE_STATE_DIR="), "exactly one HUDDLE_STATE_DIR entry expected")
	require.Equal(t, 1, count(out, "HUDDLE_CHANNEL_PREFIX="), "exactly one HUDDLE_CHANNEL_PREFIX entry expected")
}

func TestEnvWithOverridesPreservesNonOverriddenKeys(t *testing.T) {
	base := []string{"A=1", "B=2", "C=3"}
	out := envWithOverrides(base, map[string]string{"D": "4"})

	require.True(t, slices.Contains(out, "A=1"))
	require.True(t, slices.Contains(out, "B=2"))
	require.True(t, slices.Contains(out, "C=3"))
	require.True(t, slices.Contains(out, "D=4"))
	require.Len(t, out, 4)
}

func TestEnvWithOverridesHandlesMalformedEntries(t *testing.T) {
	base := []string{"GOOD=value", "NOEQUALS"}
	out := envWithOverrides(base, map[string]string{})

	require.Contains(t, out, "GOOD=value")
	require.Contains(t, out, "NOEQUALS", "entries without '=' are kept verbatim")
}

func count(xs []string, prefix string) int {
	n := 0
	for _, s := range xs {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}
