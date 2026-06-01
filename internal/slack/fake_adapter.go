package slack

import (
	"context"
	"time"

	"github.com/itsHabib/huddle/internal/types"
)

// FakeLookupCall records one LookupUser invocation for test assertions.
type FakeLookupCall struct {
	Ref string
}

// FakeAdapter satisfies Adapter for tests without dialing Slack Web API.
type FakeAdapter struct {
	Chan         Channel
	Hist         []types.Message
	Posts        [][]string // channelID, rendered text, threadTS
	Invites      [][]string // channelID, userID
	CreatedNames []string
	ArchivedIDs  []string

	CreateErr  error
	HistErr    error
	PostErr    error
	ArchiveErr error
	InviteErr  error

	ReturnedTS string

	// BotUserIDValue is returned by BotUserID(); tests set this in setup.
	BotUserIDValue string

	// OrchestratorSlackUserIDValue mirrors slackGoAdapter's orchestrator field
	// for tests that verify decoder orchestrator-direct-Slack behavior.
	OrchestratorSlackUserIDValue string

	// UsersByRef drives LookupUser. Key is the ref passed to LookupUser.
	UsersByRef map[string]types.UserInfo

	// ChannelMembers drives ListChannelMembers. Key is channelID.
	ChannelMembers map[string][]string

	// LookupUserErr / ListChannelMembersErr override default behavior.
	LookupUserErr         error
	ListChannelMembersErr error

	// LookupUserErrByRef maps a ref to a per-call LookupUser error.
	LookupUserErrByRef map[string]error

	// LookupUserCalls records each LookupUser ref for assertions.
	LookupUserCalls []FakeLookupCall
}

// CreateChannel records the attempted name and returns canned or configured channel metadata.
func (f *FakeAdapter) CreateChannel(_ context.Context, name string) (Channel, error) {
	f.CreatedNames = append(f.CreatedNames, name)
	if f.CreateErr != nil {
		return Channel{}, f.CreateErr
	}

	if f.Chan.ID == "" {
		f.Chan = Channel{ID: "C-" + name, Name: name}
	}

	return Channel{ID: f.Chan.ID, Name: f.Chan.Name}, nil
}

// InviteUserToChannel records the (channelID, userID) pair and returns the configured error.
func (f *FakeAdapter) InviteUserToChannel(_ context.Context, channelID, userID string) error {
	f.Invites = append(f.Invites, []string{channelID, userID})
	if f.InviteErr != nil {
		return f.InviteErr
	}

	return nil
}

// ArchiveChannel records the channel id that would be archived.
func (f *FakeAdapter) ArchiveChannel(_ context.Context, channelID string) error {
	f.ArchivedIDs = append(f.ArchivedIDs, channelID)
	if f.ArchiveErr != nil {
		return f.ArchiveErr
	}

	return nil
}

// PostMessage records post calls and returns a deterministic timestamp unless configured.
func (f *FakeAdapter) PostMessage(_ context.Context, channelID, text, threadTS string) (string, error) {
	f.Posts = append(f.Posts, []string{channelID, text, threadTS})

	if f.PostErr != nil {
		return "", f.PostErr
	}

	ts := f.ReturnedTS
	if ts == "" {
		ts = "1738458123.000456"
	}

	return ts, nil
}

// History returns the seeded messages, filtering out system subtypes
// the same way the real adapter does so handler tests see consistent
// behavior across the fake and real Slack paths.
func (f *FakeAdapter) History(_ context.Context, _ string, _ *time.Time, _ int) ([]types.Message, error) {
	if f.HistErr != nil {
		return nil, f.HistErr
	}

	out := make([]types.Message, 0, len(f.Hist))
	for _, m := range f.Hist {
		if isSystemHistorySubType(m.SubType) {
			continue
		}

		out = append(out, m)
	}

	return out, nil
}

// BotUserID returns BotUserIDValue.
func (f *FakeAdapter) BotUserID() string {
	return f.BotUserIDValue
}

// LookupUser returns UsersByRef[ref] or configured errors.
func (f *FakeAdapter) LookupUser(_ context.Context, ref string) (types.UserInfo, error) {
	f.LookupUserCalls = append(f.LookupUserCalls, FakeLookupCall{Ref: ref})

	if f.LookupUserErrByRef != nil {
		if err, ok := f.LookupUserErrByRef[ref]; ok {
			return types.UserInfo{}, err
		}
	}

	if f.LookupUserErr != nil {
		return types.UserInfo{}, f.LookupUserErr
	}

	if info, ok := f.UsersByRef[ref]; ok {
		return info, nil
	}

	return types.UserInfo{}, ErrUserNotFound
}

// ListChannelMembers returns ChannelMembers[channelID].
func (f *FakeAdapter) ListChannelMembers(_ context.Context, channelID string) ([]string, error) {
	if f.ListChannelMembersErr != nil {
		return nil, f.ListChannelMembersErr
	}

	return f.ChannelMembers[channelID], nil
}

var _ Adapter = (*FakeAdapter)(nil)
