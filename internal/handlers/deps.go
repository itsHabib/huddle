<<<<<<< HEAD
// Package handlers implements MCP tool handlers for huddle verbs.
package handlers

import (
	"log/slog"

	"github.com/itsHabib/huddle/internal/config"
=======
// Package handlers implements MCP tool verbs for huddle.
package handlers

import (
>>>>>>> 37a0164 (feat(handlers): huddle.post + huddle.read)
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"
)

<<<<<<< HEAD
// Deps aggregates runtime collaborators for handlers (mirrors server.Deps).
type Deps struct {
	Slack slack.Adapter
	Store *store.Store
	Cfg   config.Config
	Log   *slog.Logger
=======
// Deps is the collaborator bundle passed into verb registration (avoids an import cycle with server).
type Deps struct {
	Slack slack.Adapter
	Store *store.Store
>>>>>>> 37a0164 (feat(handlers): huddle.post + huddle.read)
}
