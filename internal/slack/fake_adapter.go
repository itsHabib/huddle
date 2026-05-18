package slack

import (
	"context"
	"time"

	"github.com/itsHabib/huddle/internal/types"
)

// FakeAdapter satisfies Adapter for tests without dialing Slack Web API.
type FakeAdapter struct {
	Chan         Channel
	Hist         []types.Message
	Posts        [][]string // channelID, rendered text, threadTS
	CreatedNames []string
	ArchivedIDs  []string

	CreateErr  error
	HistErr    error
	PostErr    error
	ArchiveErr error

	ReturnedTS string
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

// History returns a copy of configured history messages or HistErr.
func (f *FakeAdapter) History(_ context.Context, _ string, _ *time.Time, _ int) ([]types.Message, error) {
	if f.HistErr != nil {
		return nil, f.HistErr
	}

	return append([]types.Message(nil), f.Hist...), nil
}
