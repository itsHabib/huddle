package server

import (
	"context"
	"log/slog"

	"github.com/itsHabib/huddle/internal/handlers"
<<<<<<< HEAD

=======
>>>>>>> 37a0164 (feat(handlers): huddle.post + huddle.read)
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubVerb struct {
	Verb string `json:"verb"`
	OK   bool   `json:"ok"`
}

// RegisterVerbStubs wires remaining v0 tool names with deterministic placeholder results.
func RegisterVerbStubs(s *mcp.Server, deps Deps) {
<<<<<<< HEAD
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
=======
	const description = `Foundation stub; handler logic arrives in downstream streams`

	handlers.RegisterPost(s, handlers.Deps{Slack: deps.Slack, Store: deps.Store})
	handlers.RegisterRead(s, handlers.Deps{Slack: deps.Slack, Store: deps.Store})

	verbs := [...]string{
		"huddle.create",
		"huddle.close",
		"huddle.list",
		"huddle.who_else",
	}

	deps.Log.Info("wiring MCP foundation stubs", slog.Int("tool_count", len(verbs)+2))
>>>>>>> 37a0164 (feat(handlers): huddle.post + huddle.read)

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
