package server

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubVerb struct {
	Verb string `json:"verb"`
	OK   bool   `json:"ok"`
}

// RegisterVerbStubs wires six v0 tool names with deterministic placeholder results.
func RegisterVerbStubs(s *mcp.Server, deps Deps) {
	const description = `Foundation stub; handler logic arrives in downstream streams`
	verbs := [...]string{
		"huddle.create",
		"huddle.close",
		"huddle.list",
		"huddle.post",
		"huddle.read",
		"huddle.who_else",
	}

	deps.Log.Info("wiring MCP foundation stubs", slog.Int("tool_count", len(verbs)))

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
