// Package config loads process environment for the huddle binary.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const (
	envSlackBotToken           = "HUDDLE_SLACK_BOT_TOKEN"
	envStateDir                = "HUDDLE_STATE_DIR"
	envLogLevel                = "HUDDLE_LOG_LEVEL"
	envChannelPrefix           = "HUDDLE_CHANNEL_PREFIX"
	envOrchestratorSlackUserID = "HUDDLE_ORCHESTRATOR_SLACK_USER_ID"
	defaultStateDir            = ".huddle"
	defaultLogLevel            = "info"
	defaultChanPrefix          = "huddle-"
)

// Config holds validated runtime flags for one huddle process.
type Config struct {
	SlackBotToken           string
	StateDir                string
	LogLevel                slog.Level
	ChannelPrefix           string
	OrchestratorSlackUserID string
}

// Load reads environment variables once and applies defaults documented in docs/design.md.
//
// HUDDLE_SLACK_BOT_TOKEN is read but no longer required. Verbs that don't
// hit Slack (huddle.who_else, plus any future local-only verb) work
// tokenless. Verbs that do hit Slack — huddle.create, huddle.close,
// huddle.post, huddle.read — error at call time with a clear message via
// the slack package's no-token adapter when the token is unset.
func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv(envSlackBotToken))

	// State dir defaults to .huddle in the working directory; set HUDDLE_STATE_DIR
	// to override (the MCP server config pins an absolute path). The default is
	// resolved against cwd at store.New time, and cmd/huddle logs the resolved
	// absolute path at startup so the location is never a mystery.
	stateDir := strings.TrimSpace(os.Getenv(envStateDir))
	if stateDir == "" {
		stateDir = defaultStateDir
	}
	stateDir = filepath.Clean(stateDir)

	logLevelRaw := strings.TrimSpace(strings.ToLower(os.Getenv(envLogLevel)))
	if logLevelRaw == "" {
		logLevelRaw = defaultLogLevel
	}
	var lvl slog.Level
	switch logLevelRaw {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return Config{}, fmt.Errorf("invalid HUDDLE_LOG_LEVEL %q", logLevelRaw)
	}

	channelPrefix := strings.TrimSpace(os.Getenv(envChannelPrefix))
	if channelPrefix == "" {
		channelPrefix = defaultChanPrefix
	}

	cfg := Config{
		SlackBotToken:           token,
		StateDir:                stateDir,
		LogLevel:                lvl,
		ChannelPrefix:           channelPrefix,
		OrchestratorSlackUserID: strings.TrimSpace(os.Getenv(envOrchestratorSlackUserID)),
	}
	return cfg, nil
}
