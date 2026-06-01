package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterInviteHuman registers the huddle.invite_human tool.
func RegisterInviteHuman(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "huddle.invite_human",
		Description: "Invite one or more humans (Slack user IDs or emails) to an existing huddle's channel. " +
			"Best-effort: unresolvable or un-invitable refs are returned under skipped.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args types.InviteHumanArgs) (*mcp.CallToolResult, types.InviteHumanResult, error) {
		out, err := executeInviteHuman(ctx, deps, args)
		if err != nil {
			return nil, types.InviteHumanResult{}, err
		}

		return nil, out, nil
	})
}

func executeInviteHuman(ctx context.Context, deps Deps, args types.InviteHumanArgs) (types.InviteHumanResult, error) {
	huddleID := strings.TrimSpace(args.HuddleID)
	if huddleID == "" {
		return types.InviteHumanResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("huddleId is required"))
	}

	if len(args.Humans) == 0 {
		return types.InviteHumanResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("at least one human ref is required"))
	}

	hdl, err := deps.Store.LookupHuddle(ctx, huddleID)
	if err != nil {
		if errors.Is(err, huddleerr.ErrHuddleNotFound) {
			return types.InviteHumanResult{}, huddleerr.MCPError(jsonrpc.CodeInvalidParams, huddleerr.ErrHuddleNotFound)
		}

		return types.InviteHumanResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("lookup huddle: %w", err))
	}

	invited, skipped := resolveAndInviteHumans(ctx, deps.Slack, deps.Log, hdl.SlackChannelID, args.Humans)

	return types.InviteHumanResult{Invited: invited, Skipped: skipped}, nil
}
