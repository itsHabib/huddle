package handlers

import (
	"context"
	"errors"

	"github.com/itsHabib/huddle/internal/store"
	"github.com/itsHabib/huddle/internal/types"
)

var errHuddleIDRequired = errors.New("huddleId is required")
var errKeyOrHuddleRequired = errors.New("key or huddleId is required")

func resolvePostSpeaker(ctx context.Context, st *store.Store, args types.PostArgs) (types.Identity, string, error) {
	if args.Key != "" {
		k, lerr := st.LookupKey(ctx, args.Key)
		if lerr != nil {
			return types.Identity{}, "", lerr
		}

		return types.Identity{
			Kind:        types.IdentityKindSeat,
			SeatID:      k.SeatID,
			DisplayName: k.DisplayName,
		}, k.HuddleID, nil
	}

	if args.HuddleID == "" {
		return types.Identity{}, "", errHuddleIDRequired
	}

	h, lerr := st.LookupHuddle(ctx, args.HuddleID)
	if lerr != nil {
		return types.Identity{}, "", lerr
	}

	return types.Identity{
		Kind:        types.IdentityKindOrchestrator,
		DisplayName: h.OrchestratorDisplayName,
	}, h.ID, nil
}

func resolveReadHuddle(ctx context.Context, st *store.Store, args types.ReadArgs) (types.Huddle, error) {
	if args.Key != "" {
		k, err := st.LookupKey(ctx, args.Key)
		if err != nil {
			return types.Huddle{}, err
		}

		h, err := st.LookupHuddle(ctx, k.HuddleID)
		if err != nil {
			return types.Huddle{}, err
		}

		return h, nil
	}

	if args.HuddleID == "" {
		return types.Huddle{}, errKeyOrHuddleRequired
	}

	h, err := st.LookupHuddle(ctx, args.HuddleID)
	if err != nil {
		return types.Huddle{}, err
	}

	return h, nil
}
