// Package handlers implements MCP tool handlers for huddle verbs.
package handlers

import (
	"log/slog"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
)

// Deps aggregates runtime collaborators for handlers (mirrors server.Deps).
type Deps struct {
	Slack slack.Adapter
	Store *store.Store
	Cfg   config.Config
	Log   *slog.Logger
}
