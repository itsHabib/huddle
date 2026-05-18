package handlers

import (
	"context"
	"errors"
	"fmt"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterRead wires the huddle.read tool.
func RegisterRead(s *mcp.Server, deps Deps) {
	const description = `Read recent messages from the huddle channel using a seat key or orchestrator access (huddleId without key).`

	mcp.AddTool(s, &mcp.Tool{Name: "huddle.read", Description: description},
		func(ctx context.Context, _ *mcp.CallToolRequest, args types.ReadArgs) (*mcp.CallToolResult, types.ReadResult, error) {
			h, err := resolveReadHuddle(ctx, deps.Store, args)
			if err != nil {
				return nil, types.ReadResult{}, readResolveErr(err)
			}

			msgs, err := deps.Slack.History(ctx, h.SlackChannelID, args.Since, args.Limit)
			if err != nil {
				return nil, types.ReadResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("slack history: %w", err))
			}

			return nil, types.ReadResult{Messages: msgs}, nil
		})
}

func readResolveErr(err error) error {
	switch {
	case errors.Is(err, huddleerr.ErrKeyInvalid):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrKeyInvalid)
	case errors.Is(err, huddleerr.ErrHuddleNotFound):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleNotFound)
	case errors.Is(err, errKeyOrHuddleRequired):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, err)
	default:
		return huddleerr.MCPError(jsonrpc.CodeInternalError, err)
	}
}
