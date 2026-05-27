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
	defaultStateDir            = "./.huddle-state"
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

// ValidationError aggregates missing env contract violations.
type ValidationError struct {
	Missing []string
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Missing) == 0 {
		return "configuration invalid"
	}
	return "missing required configuration: " + strings.Join(e.Missing, ", ")
}

// Load reads environment variables once and applies defaults documented in docs/design.md.
func Load() (Config, error) {
	token := strings.TrimSpace(os.Getenv(envSlackBotToken))
	var missing []string
	if token == "" {
		missing = append(missing, envSlackBotToken)
	}
	if len(missing) > 0 {
		return Config{}, &ValidationError{Missing: missing}
	}

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
