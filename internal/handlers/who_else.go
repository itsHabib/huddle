package handlers

import (
	"context"
	"errors"
	"strings"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterWhoElse registers the huddle.who_else tool.
func RegisterWhoElse(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.who_else",
		Description: `Return the huddle purpose, orchestrator display name, active seats, and channel humans for a seat key.`,
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

		humans, herr := listChannelHumans(ctx, deps, hdl.SlackChannelID)
		if herr != nil {
			return nil, types.WhoElseResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, herr)
		}

		out := types.WhoElseResult{
			Purpose: hdl.Purpose,
			Orchestrator: types.Seat{
				ID:          hdl.OrchestratorID,
				DisplayName: hdl.OrchestratorDisplayName,
			},
			Seats:  seats,
			Humans: humans,
		}

		return nil, out, nil
	})
}

func listChannelHumans(ctx context.Context, deps Deps, channelID string) ([]types.Human, error) {
	members, err := deps.Slack.ListChannelMembers(ctx, channelID)
	if errors.Is(err, slack.ErrNoToken) {
		return []types.Human{}, nil
	}

	if err != nil {
		return nil, err
	}

	botID := deps.Slack.BotUserID()
	orchID := strings.TrimSpace(deps.Cfg.OrchestratorSlackUserID)
	log := humanLogger(deps.Log)
	humans := make([]types.Human, 0)

	for _, memberID := range members {
		if memberID == botID {
			continue
		}

		if orchID != "" && memberID == orchID {
			continue
		}

		info, lookupErr := deps.Slack.LookupUser(ctx, memberID)
		if lookupErr != nil {
			log.Warn("who_else human lookup skipped",
				"user_id", memberID,
				"error", lookupErr.Error(),
			)

			continue
		}

		if info.IsBot || info.Deactivated {
			continue
		}

		humans = append(humans, types.Human{
			SlackUserID: info.UserID,
			DisplayName: info.DisplayName,
			Kind:        types.IdentityKindHuman,
		})
	}

	return humans, nil
}
