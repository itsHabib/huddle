package slack

import (
	"context"
	"errors"
	"testing"

	"github.com/itsHabib/huddle/internal/config"

	"github.com/stretchr/testify/require"
)

// TestNewAdapterTokenlessReturnsNoTokenAdapter verifies that constructing
// a Slack adapter without a token returns the sentinel no-token adapter
// rather than a real Slack client. This is the seam that lets the MCP
// server boot tokenless and still serve local-only verbs like
// huddle.who_else.
func TestNewAdapterTokenlessReturnsNoTokenAdapter(t *testing.T) {
	t.Parallel()

	a := NewAdapter(config.Config{SlackBotToken: ""})
	_, ok := a.(noTokenAdapter)
	require.True(t, ok, "expected noTokenAdapter, got %T", a)
}

// TestNoTokenAdapterEveryMethodErrors verifies every Adapter method
// surfaces ErrNoToken so the Slack-touching verbs (create/close/post/read)
// fail at call time with a stable, recognizable error.
func TestNoTokenAdapterEveryMethodErrors(t *testing.T) {
	t.Parallel()

	a := noTokenAdapter{}
	ctx := context.Background()

	_, err := a.CreateChannel(ctx, "any")
	require.ErrorIs(t, err, ErrNoToken)

	require.ErrorIs(t, a.ArchiveChannel(ctx, "C123"), ErrNoToken)
	require.ErrorIs(t, a.InviteUserToChannel(ctx, "C123", "U123"), ErrNoToken)

	_, err = a.PostMessage(ctx, "C123", "hi", "")
	require.ErrorIs(t, err, ErrNoToken)

	_, err = a.History(ctx, "C123", nil, 10)
	require.ErrorIs(t, err, ErrNoToken)
}

// TestErrNoTokenMessageDocumentsRemedy guards the error message's wording
// because it's surfaced verbatim to operators via MCP tool errors and the
// README quotes it. A docs-drift signal if anyone "improves" the wording.
func TestErrNoTokenMessageDocumentsRemedy(t *testing.T) {
	t.Parallel()

	require.Contains(t, ErrNoToken.Error(), "HUDDLE_SLACK_BOT_TOKEN")
	require.Contains(t, ErrNoToken.Error(), "set the env")
}

// Sanity: ErrNoToken is reachable via errors.Is from a wrapped form too.
func TestErrNoTokenWraps(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(errors.New("upstream"), ErrNoToken)
	require.ErrorIs(t, wrapped, ErrNoToken)
}
