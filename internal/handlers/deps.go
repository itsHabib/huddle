// Package handlers implements MCP tool handlers for huddle verbs.
package handlers

import "github.com/itsHabib/huddle/internal/store"

// Deps carries runtime collaborators for handlers. It mirrors server.Deps with
// only the fields handlers need, avoiding an import cycle with package server.
type Deps struct {
	Store *store.Store
}
