package handlers

import (
	"context"
	"encoding/json"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func listArgsFromWire(in any) (types.ListArgs, error) {
	if in == nil {
		return types.ListArgs{}, nil
	}

	raw, err := json.Marshal(in)
	if err != nil {
		return types.ListArgs{}, err
	}

	var args types.ListArgs
	if len(raw) == 0 || string(raw) == "null" {
		return types.ListArgs{}, nil
	}

	if err := json.Unmarshal(raw, &args); err != nil {
		return types.ListArgs{}, err
	}

	return args, nil
}

// RegisterList registers the huddle.list tool (operator; no key parameter).
func RegisterList(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.list",
		Description: `List huddle metadata. Set active to true to return only open huddles.`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in any) (*mcp.CallToolResult, any, error) {
		args, err := listArgsFromWire(in)
		if err != nil {
			return nil, nil, huddleerr.MCPError(jsonrpc.CodeInvalidParams, err)
		}

		huddles, err := deps.Store.ListHuddles(ctx, args.Active)
		if err != nil {
			return nil, nil, huddleerr.MCPError(jsonrpc.CodeInternalError, err)
		}

		if huddles == nil {
			huddles = []types.Huddle{}
		}

		return nil, huddles, nil
	})
}
