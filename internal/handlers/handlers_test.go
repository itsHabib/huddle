package handlers_test

import (
	"context"
	"testing"

	"github.com/itsHabib/huddle/internal/handlers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func testImpl(t *testing.T) *mcp.Implementation {
	t.Helper()

	return &mcp.Implementation{Name: t.Name(), Version: "v0-test"}
}

// newToolSession connects an in-memory MCP client to a server after register adds tools.
func newToolSession(t *testing.T, register func(*mcp.Server)) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	srv := mcp.NewServer(testImpl(t), nil)
	register(srv)

	ct, st := mcp.NewInMemoryTransports()
	_, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)

	cl := mcp.NewClient(testImpl(t), nil)
	cs, err := cl.Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

func registerWhoElse(deps handlers.Deps) func(*mcp.Server) {
	return func(s *mcp.Server) {
		handlers.RegisterWhoElse(s, deps)
	}
}

func registerList(deps handlers.Deps) func(*mcp.Server) {
	return func(s *mcp.Server) {
		handlers.RegisterList(s, deps)
	}
}
