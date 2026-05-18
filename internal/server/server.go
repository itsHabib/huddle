package server

import (
	"context"
	"log/slog"

	"github.com/itsHabib/huddle/internal/handlers"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubVerb struct {
	Verb string `json:"verb"`
	OK   bool   `json:"ok"`
}

// RegisterVerbStubs wires remaining v0 tool names with deterministic placeholder results.
func RegisterVerbStubs(s *mcp.Server, deps Deps) {
	hdep := handlers.Deps{
		Slack: deps.Slack,
		Store: deps.Store,
		Cfg:   deps.Cfg,
		Log:   deps.Log,
	}

	handlers.RegisterCreate(s, hdep)
	handlers.RegisterClose(s, hdep)
	handlers.RegisterWhoElse(s, hdep)
	handlers.RegisterList(s, hdep)

	const description = `Foundation stub; handler logic arrives in downstream streams`
	verbs := [...]string{
		"huddle.post",
		"huddle.read",
	}

	deps.Log.Info("wiring MCP stub tools", slog.Int("stub_count", len(verbs)))

	for _, name := range verbs {
		title := name
		mcp.AddTool(s, &mcp.Tool{Name: title, Description: description},
			func(_ context.Context, _ *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, stubVerb, error) {
				out := stubVerb{
					Verb: title,
					OK:   true,
				}

				return nil, out, nil
			})
	}
}
