package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterClose wires the huddle.close MCP tool.
func RegisterClose(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.close",
		Description: "Archives the Slack channel and marks the huddle closed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args types.CloseArgs) (*mcp.CallToolResult, types.CloseResult, error) {
		out, err := executeClose(ctx, deps, args)
		if err != nil {
			return nil, types.CloseResult{}, err
		}

		return nil, out, nil
	})
}

func executeClose(ctx context.Context, deps Deps, args types.CloseArgs) (types.CloseResult, error) {
	huddleID := strings.TrimSpace(args.HuddleID)
	if huddleID == "" {
		return types.CloseResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("huddleId is required"))
	}

	h, err := deps.Store.LookupHuddle(ctx, huddleID)
	if err != nil {
		if errors.Is(err, huddleerr.ErrHuddleNotFound) {
			return types.CloseResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleNotFound)
		}

		return types.CloseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("lookup huddle: %w", err))
	}

	if h.ClosedAt != nil {
		return types.CloseResult{Closed: true, ArchivedChannel: h.SlackChannelName}, nil
	}

	if err = deps.Slack.ArchiveChannel(ctx, h.SlackChannelID); err != nil {
		return types.CloseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("slack archive: %w", err))
	}

	if err = deps.Store.MarkClosed(ctx, h.ID, time.Now().UTC()); err != nil {
		return types.CloseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("mark closed: %w", err))
	}

	return types.CloseResult{Closed: true, ArchivedChannel: h.SlackChannelName}, nil
}
