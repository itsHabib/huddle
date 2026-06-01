package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/types"
)

// resolveAndInviteHumans resolves each ref to a Slack user and invites them to
// channelID. Best-effort: every ref yields either an Invited human or a Skipped
// record; the function never returns an error. invited/skipped are non-nil.
func resolveAndInviteHumans(
	ctx context.Context,
	adapter slack.Adapter,
	log *slog.Logger,
	channelID string,
	refs []string,
) ([]types.Human, []types.SkippedHuman) {
	invited := make([]types.Human, 0)
	skipped := make([]types.SkippedHuman, 0)
	if len(refs) == 0 {
		return invited, skipped
	}

	logger := humanLogger(log)

	members, merr := adapter.ListChannelMembers(ctx, channelID)
	memberSet := make(map[string]struct{})
	if merr != nil {
		logger.Warn("channel member pre-check failed; continuing without membership set",
			"channel_id", channelID,
			"error", merr.Error(),
		)
	}

	for _, m := range members {
		memberSet[m] = struct{}{}
	}

	for _, ref := range refs {
		invited, skipped = inviteOneHuman(ctx, adapter, logger, channelID, ref, memberSet, invited, skipped)
	}

	return invited, skipped
}

func inviteOneHuman(
	ctx context.Context,
	adapter slack.Adapter,
	log *slog.Logger,
	channelID, ref string,
	memberSet map[string]struct{},
	invited []types.Human,
	skipped []types.SkippedHuman,
) ([]types.Human, []types.SkippedHuman) {
	info, err := adapter.LookupUser(ctx, ref)
	if err != nil {
		reason := classifyLookupErr(err)
		if reason == types.SkippedReasonInviteFailed {
			log.Warn("human lookup failed",
				"ref", ref,
				"error", err.Error(),
			)
		}

		return invited, append(skipped, types.SkippedHuman{Ref: ref, Reason: reason})
	}

	if _, inChannel := memberSet[info.UserID]; inChannel {
		return invited, append(skipped, types.SkippedHuman{
			Ref:    ref,
			Reason: types.SkippedReasonAlreadyInChannel,
		})
	}

	if err = adapter.InviteUserToChannel(ctx, channelID, info.UserID); err != nil {
		log.Warn("human invite failed",
			"ref", ref,
			"user_id", info.UserID,
			"error", err.Error(),
		)

		return invited, append(skipped, types.SkippedHuman{
			Ref:    ref,
			Reason: types.SkippedReasonInviteFailed,
		})
	}

	return append(invited, types.Human{
		SlackUserID: info.UserID,
		DisplayName: info.DisplayName,
		Kind:        types.IdentityKindHuman,
	}), skipped
}

func classifyLookupErr(err error) types.SkippedReason {
	switch {
	case errors.Is(err, slack.ErrInvalidUserRef):
		return types.SkippedReasonInvalidRef
	case errors.Is(err, slack.ErrUserNotFound):
		return types.SkippedReasonUnknownUser
	case errors.Is(err, slack.ErrMissingEmailScope):
		return types.SkippedReasonMissingEmailScope
	default:
		return types.SkippedReasonInviteFailed
	}
}

func humanLogger(log *slog.Logger) *slog.Logger {
	if log != nil {
		return log
	}

	return slog.Default()
}
