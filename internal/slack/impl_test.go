package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/types"

	slackapi "github.com/slack-go/slack"
	"github.com/stretchr/testify/require"
)

func TestNewAdapterTokenlessReturnsNoTokenAdapter(t *testing.T) {
	t.Parallel()

	a := NewAdapter(config.Config{SlackBotToken: ""})
	_, ok := a.(noTokenAdapter)
	require.True(t, ok, "expected noTokenAdapter, got %T", a)
}

func TestNewAdapterCachesBotUserID(t *testing.T) {
	t.Parallel()

	const botID = "UBOT12345"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"` + botID + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	client := slackapi.New("xoxb-test",
		slackapi.OptionAPIURL(srv.URL+"/"),
		slackapi.OptionHTTPClient(srv.Client()),
	)
	a := newAdapterFromClient(client, "")
	adapter, ok := a.(*slackGoAdapter)
	require.True(t, ok)
	require.Equal(t, botID, adapter.BotUserID())
}

func TestNewAdapterAuthTestFailureReturnsNoTokenAdapter(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	t.Cleanup(srv.Close)

	client := slackapi.New("xoxb-bad",
		slackapi.OptionAPIURL(srv.URL+"/"),
		slackapi.OptionHTTPClient(srv.Client()),
	)

	a := newAdapterFromClient(client, "")
	_, ok := a.(noTokenAdapter)
	require.True(t, ok, "expected noTokenAdapter after auth.test failure, got %T", a)
}

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

	require.Empty(t, a.BotUserID())

	_, err = a.ListChannelMembers(ctx, "C123")
	require.ErrorIs(t, err, ErrNoToken)

	_, err = a.LookupUser(ctx, "U12345678")
	require.ErrorIs(t, err, ErrNoToken)
}

func TestErrNoTokenMessageDocumentsRemedy(t *testing.T) {
	t.Parallel()

	require.Contains(t, ErrNoToken.Error(), "HUDDLE_SLACK_BOT_TOKEN")
	require.Contains(t, ErrNoToken.Error(), "set the env")
}

func TestErrNoTokenWraps(t *testing.T) {
	t.Parallel()

	wrapped := errors.Join(errors.New("upstream"), ErrNoToken)
	require.ErrorIs(t, wrapped, ErrNoToken)
}

func TestLookupUserRefDispatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("user id path", func(t *testing.T) {
		t.Parallel()

		a := testSlackAdapter(t, testSlackHandlers{
			userInfo: func(w http.ResponseWriter, userID string) {
				writeUserInfo(w, userID, "Alice")
			},
		})

		info, err := a.LookupUser(ctx, "U0ABC12345")
		require.NoError(t, err)
		require.Equal(t, "U0ABC12345", info.UserID)
		require.Equal(t, "Alice", info.DisplayName)
	})

	t.Run("email path", func(t *testing.T) {
		t.Parallel()

		a := testSlackAdapter(t, testSlackHandlers{
			lookupByEmail: func(w http.ResponseWriter, email string) {
				require.Equal(t, "joe@company.com", email)
				writeUserInfo(w, "UEMAIL01", "Joe")
			},
			userInfo: func(w http.ResponseWriter, userID string) {
				writeUserInfo(w, userID, "Joe Smith")
			},
		})

		info, err := a.LookupUser(ctx, "joe@company.com")
		require.NoError(t, err)
		require.Equal(t, "UEMAIL01", info.UserID)
		require.Equal(t, "Joe Smith", info.DisplayName)
	})

	t.Run("nonsense ref", func(t *testing.T) {
		t.Parallel()

		a := testSlackAdapter(t, testSlackHandlers{})
		_, err := a.LookupUser(ctx, "nonsense")
		require.ErrorIs(t, err, ErrInvalidUserRef)
	})

	t.Run("at-handle ref", func(t *testing.T) {
		t.Parallel()

		a := testSlackAdapter(t, testSlackHandlers{})
		_, err := a.LookupUser(ctx, "@joe")
		require.ErrorIs(t, err, ErrInvalidUserRef)
	})
}

func TestLookupUserErrMissingEmailScope(t *testing.T) {
	t.Parallel()

	a := testSlackAdapter(t, testSlackHandlers{
		lookupByEmail: func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
		},
	})

	_, err := a.LookupUser(context.Background(), "joe@company.com")
	require.ErrorIs(t, err, ErrMissingEmailScope)
}

func TestLookupUserErrRateLimited(t *testing.T) {
	t.Parallel()

	a := testSlackAdapter(t, testSlackHandlers{
		userInfo: func(w http.ResponseWriter, _ string) {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		},
	}, slackapi.OptionRetry(0))

	_, err := a.LookupUser(context.Background(), "U0ABC12345")
	require.ErrorIs(t, err, ErrRateLimited)
}

func TestLookupUserCacheTTL(t *testing.T) {
	t.Parallel()

	var userInfoCalls atomic.Int32

	a := testSlackAdapter(t, testSlackHandlers{
		userInfo: func(w http.ResponseWriter, userID string) {
			userInfoCalls.Add(1)
			writeUserInfo(w, userID, "Cached Name")
		},
	})
	a.userCache = newUserCache(50 * time.Millisecond)

	ctx := context.Background()

	info1, err := a.LookupUser(ctx, "U0CACHE01")
	require.NoError(t, err)
	require.Equal(t, "Cached Name", info1.DisplayName)
	require.Equal(t, int32(1), userInfoCalls.Load())

	info2, err := a.LookupUser(ctx, "U0CACHE01")
	require.NoError(t, err)
	require.Equal(t, info1, info2)
	require.Equal(t, int32(1), userInfoCalls.Load(), "second call within TTL should not hit Slack")

	time.Sleep(60 * time.Millisecond)

	info3, err := a.LookupUser(ctx, "U0CACHE01")
	require.NoError(t, err)
	require.Equal(t, "Cached Name", info3.DisplayName)
	require.Equal(t, int32(2), userInfoCalls.Load(), "post-TTL call should re-fetch")
}

func TestLookupUserSingleflight(t *testing.T) {
	t.Parallel()

	var userInfoCalls atomic.Int32
	start := make(chan struct{})

	a := testSlackAdapter(t, testSlackHandlers{
		userInfo: func(w http.ResponseWriter, userID string) {
			<-start
			userInfoCalls.Add(1)
			writeUserInfo(w, userID, "Deduped")
		},
	})

	ctx := context.Background()
	const n = 8
	errs := make(chan error, n)
	infos := make(chan types.UserInfo, n)

	for range n {
		go func() {
			info, err := a.LookupUser(ctx, "U0DEDUP01")
			infos <- info
			errs <- err
		}()
	}

	close(start)

	for range n {
		require.NoError(t, <-errs)
	}

	want := <-infos
	for range n - 1 {
		got := <-infos
		require.Equal(t, want, got)
	}

	require.Equal(t, int32(1), userInfoCalls.Load())
}

func TestListChannelMembers(t *testing.T) {
	t.Parallel()

	a := testSlackAdapter(t, testSlackHandlers{
		channelMembers: func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"members":["U1","U2"]}`))
		},
	})

	members, err := a.ListChannelMembers(context.Background(), "C123")
	require.NoError(t, err)
	require.Equal(t, []string{"U1", "U2"}, members)
}

// --- test helpers ---

type testSlackHandlers struct {
	authTest       func(http.ResponseWriter)
	userInfo       func(http.ResponseWriter, string)
	lookupByEmail  func(http.ResponseWriter, string)
	channelMembers func(http.ResponseWriter, string)
}

func testSlackAdapter(t *testing.T, h testSlackHandlers, extraOpts ...slackapi.Option) *slackGoAdapter {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth.test":
			if h.authTest != nil {
				h.authTest(w)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"UBOTTEST1"}`))
		case "/users.info":
			if h.userInfo != nil {
				h.userInfo(w, r.FormValue("user"))
				return
			}
			http.NotFound(w, r)
		case "/users.lookupByEmail":
			if h.lookupByEmail != nil {
				h.lookupByEmail(w, r.FormValue("email"))
				return
			}
			http.NotFound(w, r)
		case "/conversations.members":
			if h.channelMembers != nil {
				h.channelMembers(w, r.FormValue("channel"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	opts := []slackapi.Option{
		slackapi.OptionAPIURL(srv.URL + "/"),
		slackapi.OptionHTTPClient(srv.Client()),
	}
	opts = append(opts, extraOpts...)

	client := slackapi.New("xoxb-test", opts...)

	return &slackGoAdapter{
		client:    client,
		botUserID: "UBOTTEST1",
		userCache: newUserCache(defaultUserCacheTTL),
	}
}

func writeUserInfo(w http.ResponseWriter, userID, displayName string) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"ok": true,
		"user": map[string]any{
			"id":      userID,
			"is_bot":  false,
			"deleted": false,
			"profile": map[string]string{
				"display_name": displayName,
				"real_name":    displayName + " Real",
			},
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
