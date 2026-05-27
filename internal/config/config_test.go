package config

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadTokenlessSucceeds(t *testing.T) {
	// HUDDLE_SLACK_BOT_TOKEN is no longer required at startup so the server
	// can serve local-only verbs (huddle.who_else) without it. Slack-touching
	// verbs error at call time via the slack package's no-token adapter.
	t.Setenv(envSlackBotToken, "")
	t.Setenv(envStateDir, "")
	t.Setenv(envLogLevel, "")
	t.Setenv(envChannelPrefix, "")
	t.Setenv(envOrchestratorSlackUserID, "")

	cfg, err := Load()
	require.NoError(t, err)
	require.Empty(t, cfg.SlackBotToken)
	require.Equal(t, filepath.Clean(defaultStateDir), cfg.StateDir)
	require.Equal(t, slog.LevelInfo, cfg.LogLevel)
	require.Equal(t, defaultChanPrefix, cfg.ChannelPrefix)
	require.Empty(t, cfg.OrchestratorSlackUserID)
}

func TestLoadHonorsAllEnvVars(t *testing.T) {
	t.Setenv(envSlackBotToken, "xoxb-test-token")
	t.Setenv(envStateDir, "/tmp/huddle-test")
	t.Setenv(envLogLevel, "debug")
	t.Setenv(envChannelPrefix, "test-")
	t.Setenv(envOrchestratorSlackUserID, "U0ABC123")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "xoxb-test-token", cfg.SlackBotToken)
	require.Contains(t, cfg.StateDir, "huddle-test")
	require.Equal(t, slog.LevelDebug, cfg.LogLevel)
	require.Equal(t, "test-", cfg.ChannelPrefix)
	require.Equal(t, "U0ABC123", cfg.OrchestratorSlackUserID)
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	t.Setenv(envSlackBotToken, "xoxb-test-token")
	t.Setenv(envLogLevel, "shouty")
	_, err := Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid HUDDLE_LOG_LEVEL")
}
