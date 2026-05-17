package slack

import (
	"context"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/types"

	"github.com/stretchr/testify/require"
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

func (f *FakeAdapter) ArchiveChannel(_ context.Context, channelID string) error {
	f.ArchivedIDs = append(f.ArchivedIDs, channelID)
	if f.ArchiveErr != nil {
		return f.ArchiveErr
	}

	return nil
}

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

func (f *FakeAdapter) History(_ context.Context, _ string, _ *time.Time, _ int) ([]types.Message, error) {
	if f.HistErr != nil {
		return nil, f.HistErr
	}

	return append([]types.Message(nil), f.Hist...), nil
}

func TestFakeAdapterCapturesRenderedPostMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ad := FakeAdapter{}

	text := Encode(types.Identity{Kind: types.IdentityKindOrchestrator, DisplayName: "orch"}, "hello")
	ts, err := ad.PostMessage(ctx, "C-demo", text, "")
	require.NoError(t, err)
	require.Equal(t, "1738458123.000456", ts)
	require.Len(t, ad.Posts, 1)
	require.Equal(t, "C-demo", ad.Posts[0][0])
	require.Equal(t, text, ad.Posts[0][1])
	require.Empty(t, ad.Posts[0][2])
}

func TestFakeAdapterHistoryPassesThrough(t *testing.T) {
	t.Parallel()

	msgs := []types.Message{{ID: "1", Body: "b"}}
	ad := FakeAdapter{Hist: msgs}
	out, err := ad.History(context.Background(), "C-any", nil, 2)
	require.NoError(t, err)
	require.Len(t, out, 1)
}
