// Package server hosts MCP lifecycle wiring alongside future verb handlers.
package server

import (
	"log/slog"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
)

// Deps aggregates runtime collaborators passed into MCP handlers once they land.
type Deps struct {
	Slack slack.Adapter
	Store *store.Store
	Cfg   config.Config
	Log   *slog.Logger
}
