package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	huddleerr "github.com/itsHabib/huddle/internal/errors"
	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxSlackChannelNameLen = 80

// RegisterCreate wires the huddle.create MCP tool.
func RegisterCreate(s *mcp.Server, deps Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "huddle.create",
		Description: "Creates a huddle: Slack channel, persisted row, and per-seat keys.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args types.CreateArgs) (*mcp.CallToolResult, types.CreateResult, error) {
		out, err := executeCreate(ctx, deps, args)
		if err != nil {
			return nil, types.CreateResult{}, err
		}

		return nil, out, nil
	})
}

func executeCreate(ctx context.Context, deps Deps, args types.CreateArgs) (types.CreateResult, error) {
	purpose, orchID, orchName, err := validateAndNormalizeCreate(args)
	if err != nil {
		return types.CreateResult{}, err
	}

	huddleID := "hud_" + uuid.New().String()
	channelName := slugifyChannel(deps.Cfg.ChannelPrefix, purpose, huddleID)

	ch, err := deps.Slack.CreateChannel(ctx, channelName)
	if err != nil {
		return types.CreateResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("slack create channel: %w", err))
	}

	// Best-effort invite of the configured human orchestrator. Failure is
	// logged but doesn't fail the create — the huddle is still functional;
	// the human can join the public channel manually.
	inviteOrchestrator(ctx, deps, ch.ID)

	// From here on, any failure must compensate the Slack channel + any
	// persisted huddle/keys so we don't leak channels or leave partial
	// state behind. Lookups use a fresh context so we still clean up
	// even if the caller cancels the original.
	now := time.Now().UTC()

	h := types.Huddle{
		ID:                      huddleID,
		Purpose:                 purpose,
		OrchestratorID:          orchID,
		OrchestratorDisplayName: orchName,
		SlackChannelID:          ch.ID,
		SlackChannelName:        ch.Name,
		CreatedAt:               now,
		TTLHours:                args.TTLHours,
	}

	if err = deps.Store.InsertHuddle(ctx, h); err != nil {
		// Compensation: archive the channel we just created so we
		// don't leak it. Best-effort; log via slog and otherwise
		// swallow because the original error is the headline.
		archiveOrphanChannel(ctx, deps, ch.ID, "insert huddle failed")

		return types.CreateResult{}, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("insert huddle: %w", err))
	}

	seatsOut, err := insertSeatKeys(ctx, deps, huddleID, args.Seats, now)
	if err != nil {
		// Compensation: the huddle row + any partial keys are now
		// orphans. DeleteHuddle cascades to keys via ON DELETE CASCADE,
		// then archive the channel. Best-effort.
		deleteOrphanHuddle(ctx, deps, huddleID, "insert seat keys failed")
		archiveOrphanChannel(ctx, deps, ch.ID, "insert seat keys failed")

		return types.CreateResult{}, err
	}

	return types.CreateResult{
		HuddleID:     huddleID,
		Channel:      ch.Name,
		Orchestrator: types.Seat{ID: orchID, DisplayName: orchName},
		Seats:        seatsOut,
	}, nil
}

// inviteOrchestrator adds the configured human orchestrator (env
// HUDDLE_ORCHESTRATOR_SLACK_USER_ID) to channelID. Skipped if the env is
// unset; logged-and-swallowed on failure because the huddle is otherwise
// usable and the channel is public. The skip is logged at info level so
// that operators who expected to be invited (a common expectation, since
// the env is documented and usually configured) get a breadcrumb rather
// than a silent absence.
func inviteOrchestrator(ctx context.Context, deps Deps, channelID string) {
	userID := strings.TrimSpace(deps.Cfg.OrchestratorSlackUserID)
	if userID == "" {
		compensationLogger(deps).Info("orchestrator invite skipped",
			"channel_id", channelID,
			"reason", "HUDDLE_ORCHESTRATOR_SLACK_USER_ID unset",
		)

		return
	}

	if err := deps.Slack.InviteUserToChannel(ctx, channelID, userID); err != nil {
		compensationLogger(deps).Warn("orchestrator invite failed",
			"channel_id", channelID,
			"user_id", userID,
			"error", err.Error(),
		)
	}
}

// compensationLogger returns deps.Log when set, falling back to slog's
// default logger so the compensation path never panics on a nil Log.
// Handler-test fixtures often omit Log when building Deps; that path
// converted partial-failure errors into process crashes before this.
func compensationLogger(deps Deps) *slog.Logger {
	if deps.Log != nil {
		return deps.Log
	}

	return slog.Default()
}

// archiveOrphanChannel runs the Slack archive call against a context
// derived from ctx but explicitly uncancellable, so cleanup survives the
// caller's cancellation. Errors are logged and swallowed (the original
// error is the headline).
func archiveOrphanChannel(ctx context.Context, deps Deps, channelID, reason string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := deps.Slack.ArchiveChannel(cleanupCtx, channelID); err != nil {
		compensationLogger(deps).Warn("orphan channel archive failed during compensation",
			"channel_id", channelID,
			"reason", reason,
			"error", err.Error(),
		)
	}
}

// deleteOrphanHuddle removes a huddle row that was inserted as part of a
// create that subsequently failed. Cascades to keys via the schema's FK.
// Same uncancellable-context rationale as archiveOrphanChannel.
func deleteOrphanHuddle(ctx context.Context, deps Deps, huddleID, reason string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := deps.Store.DeleteHuddle(cleanupCtx, huddleID); err != nil {
		compensationLogger(deps).Warn("orphan huddle delete failed during compensation",
			"huddle_id", huddleID,
			"reason", reason,
			"error", err.Error(),
		)
	}
}

func validateAndNormalizeCreate(args types.CreateArgs) (purpose, orchID, orchName string, err error) {
	orchID = strings.TrimSpace(args.Orchestrator.ID)
	if orchID == "" {
		orchID = "orchestrator"
	}

	orchName = strings.TrimSpace(args.Orchestrator.DisplayName)
	if orchName == "" {
		orchName = "orchestrator"
	}

	purpose = strings.TrimSpace(args.Purpose)
	if purpose == "" {
		return "", "", "", huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("purpose is required"))
	}

	if len(args.Seats) == 0 {
		return "", "", "", huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("at least one seat is required"))
	}

	seen := make(map[string]struct{}, len(args.Seats))
	for _, seat := range args.Seats {
		id := strings.TrimSpace(seat.ID)
		if id == "" {
			return "", "", "", huddleerr.MCPError(jsonrpc.CodeInvalidParams, errors.New("seat id must not be empty"))
		}

		if _, dup := seen[id]; dup {
			return "", "", "", huddleerr.MCPError(jsonrpc.CodeInvalidParams, fmt.Errorf("duplicate seat id %q", id))
		}

		seen[id] = struct{}{}
	}

	return purpose, orchID, orchName, nil
}

func insertSeatKeys(ctx context.Context, deps Deps, huddleID string, seats []types.SeatDefinition, now time.Time) ([]types.CreatedSeat, error) {
	seatsOut := make([]types.CreatedSeat, 0, len(seats))

	for _, s := range seats {
		seatID := strings.TrimSpace(s.ID)
		keyMaterial, genErr := generateSeatKey(huddleID, seatID)
		if genErr != nil {
			return nil, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("generate seat key: %w", genErr))
		}

		if err := deps.Store.InsertKey(ctx, store.Key{
			Key:         keyMaterial,
			HuddleID:    huddleID,
			SeatID:      seatID,
			DisplayName: s.DisplayName,
			CreatedAt:   now,
		}); err != nil {
			return nil, huddleerr.MCPError(jsonrpc.CodeInternalError, fmt.Errorf("insert key: %w", err))
		}

		seatsOut = append(seatsOut, types.CreatedSeat{
			ID:          seatID,
			Key:         keyMaterial,
			DisplayName: s.DisplayName,
		})
	}

	return seatsOut, nil
}

func slugifyChannel(prefix, purpose, huddleID string) string {
	p := strings.TrimSpace(purpose)
	var b strings.Builder
	for _, r := range strings.ToLower(p) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	slug := strings.Trim(b.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	if slug == "" {
		slug = "huddle"
	}

	short := huddleIDShort(huddleID)
	name := prefix + slug + "-" + short

	for len(name) > maxSlackChannelNameLen && len(slug) > 1 {
		slug = slug[:len(slug)-1]
		slug = strings.TrimRight(slug, "-")
		if slug == "" {
			slug = "h"
		}

		name = prefix + slug + "-" + short
	}

	if len(name) > maxSlackChannelNameLen {
		name = name[:maxSlackChannelNameLen]
		name = strings.TrimRight(name, "-")
	}

	return name
}

func huddleIDShort(huddleID string) string {
	raw := strings.TrimPrefix(huddleID, "hud_")
	raw = strings.ReplaceAll(raw, "-", "")
	if len(raw) > 8 {
		raw = raw[:8]
	}

	return raw
}

func generateSeatKey(huddleID, seatID string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	randPart := enc.EncodeToString(buf)

	return "K_" + huddleIDShort(huddleID) + "_" + seatID + "_" + randPart, nil
}
