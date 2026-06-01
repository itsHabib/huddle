package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/types"
)

// resolveAndInviteHumans resolves each ref to a Slack user and invites them to
// channelID. Best-effort: every ref yields either an invited human or a skipped
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
	if errors.Is(merr, slack.ErrNoToken) {
		// No token: every ref is un-invitable, and the per-ref LookupUser calls
		// would all fail with ErrNoToken too. Short-circuit (mirrors who_else's
		// tokenless degrade) and report every ref as skipped.
		for _, ref := range refs {
			skipped = append(skipped, types.SkippedHuman{Ref: ref, Reason: types.SkippedReasonInviteFailed})
		}

		return invited, skipped
	}

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
		human, skip, ok := inviteOneHuman(ctx, adapter, logger, channelID, ref, memberSet)
		if !ok {
			skipped = append(skipped, skip)

			continue
		}

		invited = append(invited, human)
		// Record the just-invited user so a duplicate ref later in the same
		// request (e.g. the same person by ID and by email) resolves as
		// already_in_channel instead of triggering a second invite.
		memberSet[human.SlackUserID] = struct{}{}
	}

	return invited, skipped
}

// inviteOneHuman resolves one ref and invites the user when they are not already
// a channel member. Returns (human, _, true) on a successful invite, or
// (_, skip, false) carrying the reason the ref was skipped.
func inviteOneHuman(
	ctx context.Context,
	adapter slack.Adapter,
	log *slog.Logger,
	channelID, ref string,
	memberSet map[string]struct{},
) (types.Human, types.SkippedHuman, bool) {
	info, err := adapter.LookupUser(ctx, ref)
	if err != nil {
		reason := classifyLookupErr(err)
		if reason == types.SkippedReasonInviteFailed {
			log.Warn("human lookup failed",
				"ref", ref,
				"error", err.Error(),
			)
		}

		return types.Human{}, types.SkippedHuman{Ref: ref, Reason: reason}, false
	}

	if _, inChannel := memberSet[info.UserID]; inChannel {
		return types.Human{}, types.SkippedHuman{Ref: ref, Reason: types.SkippedReasonAlreadyInChannel}, false
	}

	if err = adapter.InviteUserToChannel(ctx, channelID, info.UserID); err != nil {
		log.Warn("human invite failed",
			"ref", ref,
			"user_id", info.UserID,
			"error", err.Error(),
		)

		return types.Human{}, types.SkippedHuman{Ref: ref, Reason: types.SkippedReasonInviteFailed}, false
	}

	return types.Human{
		SlackUserID: info.UserID,
		DisplayName: info.DisplayName,
		Kind:        types.IdentityKindHuman,
	}, types.SkippedHuman{}, true
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
