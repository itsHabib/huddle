// Package handlers implements MCP tool handlers for huddle verbs.
package handlers

<<<<<<< HEAD
import "github.com/itsHabib/huddle/internal/store"

// Deps carries runtime collaborators for handlers. It mirrors server.Deps with
// only the fields handlers need, avoiding an import cycle with package server.
type Deps struct {
	Store *store.Store
=======
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
>>>>>>> fcbd58c (feat(handlers): implement huddle.create and huddle.close)
}
