package handlers

import (
	"context"
	"errors"
	"strings"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterWhoElse registers the huddle.who_else tool.
func RegisterWhoElse(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.who_else",
		Description: `Return the huddle purpose, orchestrator display name, and active seats for a seat key.`,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args types.WhoElseArgs) (*mcp.CallToolResult, types.WhoElseResult, error) {
		if strings.TrimSpace(args.Key) == "" {
			return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("key is required"))
		}

		k, err := deps.Store.LookupKey(ctx, args.Key)
		if err != nil {
			if errors.Is(err, huddleerr.ErrKeyInvalid) {
				return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrKeyInvalid)
			}

			return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, err)
		}

		hdl, err := deps.Store.LookupHuddle(ctx, k.HuddleID)
		if err != nil {
			return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, err)
		}

		seats, err := deps.Store.ListSeats(ctx, k.HuddleID)
		if err != nil {
			return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, err)
		}

		out := types.WhoElseResult{
			Purpose: hdl.Purpose,
			Orchestrator: types.Seat{
				ID:          hdl.OrchestratorID,
				DisplayName: hdl.OrchestratorDisplayName,
			},
			Seats: seats,
		}

		return nil, out, nil
	})
}
