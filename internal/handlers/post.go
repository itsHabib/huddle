package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterPost wires the huddle.post tool.
func RegisterPost(s *mcp.Server, deps Deps) {
	const description = `Post a message to the huddle Slack channel using a seat key or orchestrator access (huddleId without key).`

	mcp.AddTool(s, &mcp.Tool{Name: "huddle.post", Description: description},
		func(ctx context.Context, _ *mcp.CallToolRequest, args types.PostArgs) (*mcp.CallToolResult, types.PostResult, error) {
			if strings.TrimSpace(args.Body) == "" {
				return nil, types.PostResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("body is required"))
			}

			identity, huddleID, err := resolvePostSpeaker(ctx, deps.Store, args)
			if err != nil {
				return nil, types.PostResult{}, postResolveErr(err)
			}

			h, err := deps.Store.LookupHuddle(ctx, huddleID)
			if err != nil {
				return nil, types.PostResult{}, postFinalLookupErr(err)
			}

			if h.ClosedAt != nil {
				return nil, types.PostResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleClosed)
			}

			text := slack.Encode(identity, args.Body)
			ts, err := deps.Slack.PostMessage(ctx, h.SlackChannelID, text, args.ReplyTo)
			if err != nil {
				return nil, types.PostResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("slack post: %w", err))
			}

			return nil, types.PostResult{
				MessageID: ts,
				PostedAt:  time.Now().UTC(),
				Identity:  identity,
			}, nil
		})
}

func postResolveErr(err error) error {
	switch {
	case errors.Is(err, huddleerr.ErrKeyInvalid):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrKeyInvalid)
	case errors.Is(err, huddleerr.ErrHuddleNotFound):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleNotFound)
	case errors.Is(err, errHuddleIDRequired):
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, err)
	default:
		return huddleerr.MCPError(jsonrpc.CodeInternalError, err)
	}
}

func postFinalLookupErr(err error) error {
	if errors.Is(err, huddleerr.ErrHuddleNotFound) {
		return huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleNotFound)
	}

	return huddleerr.MCPError(jsonrpc.CodeInternalError, err)
}
