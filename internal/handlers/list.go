package handlers

import (
	"context"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListResult wraps the huddle slice so MCP schema-driven clients see a
// concrete output shape on `tools/list` rather than an opaque array.
// (The SDK requires output schemas to be `type: "object"`.)
type ListResult struct {
	Huddles []types.Huddle `json:"huddles"`
}

// RegisterList registers the huddle.list tool (operator; no key parameter).
func RegisterList(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.list",
		Description: `List huddle metadata. Set active to true to return only open huddles.`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args types.ListArgs) (*mcp.CallToolResult, ListResult, error) {
		active := false
		if args.Active != nil {
			active = *args.Active
		}

		huddles, err := deps.Store.ListHuddles(ctx, active)
		if err != nil {
			return nil, ListResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, err)
		}

		if huddles == nil {
			huddles = []types.Huddle{}
		}

		return nil, ListResult{Huddles: huddles}, nil
	})
}
