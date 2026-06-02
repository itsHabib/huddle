// Package main is a smoke harness that drives the huddle MCP binary as a
// subprocess and exercises the v0 verb surface end-to-end against a real
// Slack workspace. Intended for manual runs ("does this work hands-on?"),
// not the CI test suite.
//
// Requires HUDDLE_SLACK_BOT_TOKEN in the env (a real xoxb- token with the
// scopes the slack adapter needs). Other huddle env vars are overridden to
// keep smoke runs isolated from a developer's live MCP state.
//
// Optional HUDDLE_SMOKE_HUMAN_REF (Slack user ID or email) exercises
// create-with-humans and huddle.invite_human; when unset those steps are
// skipped but who_else.humans is still asserted.
//
// Usage:
//
//	go run ./cmd/smoke
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("smoke FAILED: %v", err)
	}
	fmt.Println("\nsmoke PASSED")
}

func run() error {
	if os.Getenv("HUDDLE_SLACK_BOT_TOKEN") == "" {
		return errors.New("HUDDLE_SLACK_BOT_TOKEN must be set in the env")
	}

	if os.Getenv("HUDDLE_ORCHESTRATOR_SLACK_USER_ID") == "" {
		fmt.Fprintln(os.Stderr, "WARN: HUDDLE_ORCHESTRATOR_SLACK_USER_ID is not set; you will not be auto-invited to the smoke channel. Export it to receive an invite.")
	}

	humanRef := strings.TrimSpace(os.Getenv("HUDDLE_SMOKE_HUMAN_REF"))
	if humanRef == "" {
		fmt.Fprintln(os.Stderr, "WARN: HUDDLE_SMOKE_HUMAN_REF is not set; skipping create-with-humans / invite_human steps. Export a Slack user ID or email to exercise the human-participant surface.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "huddle-smoke-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/huddle")
	cmd.Env = envWithOverrides(os.Environ(), map[string]string{
		"HUDDLE_STATE_DIR":      tmpDir,
		"HUDDLE_CHANNEL_PREFIX": "huddle-smoke-",
		"HUDDLE_LOG_LEVEL":      "info",
	})
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "smoke", Version: "v0.0.1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = sess.Close() }()

	return runScenario(ctx, sess, humanRef)
}

// runScenario walks the v0 verb tour against an already-connected MCP
// session: create → who_else (per seat) → post (3) → read → invite_human
// (when humanRef set) → close. Lifted out of run() so each layer is short
// enough for the cognitive-complexity gate.
func runScenario(ctx context.Context, sess *mcp.ClientSession, humanRef string) error {
	createRes, err := smokeCreate(ctx, sess, humanRef)
	if err != nil {
		return err
	}

	huddleID, _ := createRes["huddleId"].(string)
	seats := extractSeats(createRes)
	if huddleID == "" || len(seats) != 2 {
		return fmt.Errorf("create result missing huddleId or seats: %+v", createRes)
	}

	if got := orchestratorID(createRes); got != "michael" {
		return fmt.Errorf("create result orchestrator.id mismatch: got %q, want %q", got, "michael")
	}

	designer, implementor := seats[0], seats[1]
	logCreated(huddleID, createRes["channel"])

	// At this point a real Slack channel exists. Try to archive it on any
	// later failure so we don't leak public channels into the workspace.
	// context.WithoutCancel preserves any deadlines / values from ctx for
	// observability but detaches cancellation so the cleanup still runs
	// when the parent ctx (with its 90s timeout) has expired.
	closeOnDefer := true
	defer func() {
		if !closeOnDefer {
			return
		}
		cctx, ccancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer ccancel()
		if _, cerr := callJSON(cctx, sess, "huddle.close", map[string]any{"huddleId": huddleID}); cerr != nil {
			fmt.Fprintf(os.Stderr, "WARN: deferred huddle.close failed: %v\n", cerr)
		}
	}()

	if err := smokeWhoElse(ctx, sess, seats); err != nil {
		return err
	}

	if err := smokePosts(ctx, sess, huddleID, designer.Key, implementor.Key); err != nil {
		return err
	}

	if err := smokeRead(ctx, sess, designer.Key); err != nil {
		return err
	}

	if humanRef != "" {
		if err := smokeInviteHuman(ctx, sess, huddleID, humanRef); err != nil {
			return err
		}
	}

	if err := smokeClose(ctx, sess, huddleID); err != nil {
		return err
	}

	closeOnDefer = false

	return nil
}

func smokeCreate(ctx context.Context, sess *mcp.ClientSession, humanRef string) (map[string]any, error) {
	step("huddle.create (designer + implementor)")
	// Note: purpose deliberately does not include the word "smoke" — the
	// channel name is "<prefix>-<slug>-<short id>", and the configured
	// HUDDLE_CHANNEL_PREFIX already encodes that, so leading "smoke" in
	// the purpose would produce an awkward "huddle-smoke-smoke-…" name.
	args := map[string]any{
		"purpose": "designer + implementor pairing on search filter UX",
		"orchestrator": map[string]any{
			"id":          "michael",
			"displayName": "Michael (orchestrator)",
		},
		"seats": []map[string]any{
			{"id": "designer", "displayName": "Designer Agent"},
			{"id": "implementor", "displayName": "Implementor Agent"},
		},
	}
	if humanRef != "" {
		args["humans"] = []string{humanRef}
	}
	createRes, err := callJSON(ctx, sess, "huddle.create", args)
	if err != nil {
		return nil, fmt.Errorf("huddle.create: %w", err)
	}
	dump(createRes)
	if _, ok := createRes["humans"].([]any); !ok {
		return nil, fmt.Errorf("create result missing humans array: %+v", createRes)
	}
	return createRes, nil
}

func smokeWhoElse(ctx context.Context, sess *mcp.ClientSession, seats []smokeSeat) error {
	for _, s := range seats {
		step(fmt.Sprintf("huddle.who_else (%s)", s.ID))
		res, err := callJSON(ctx, sess, "huddle.who_else", map[string]any{"key": s.Key})
		if err != nil {
			return fmt.Errorf("who_else(%s): %w", s.ID, err)
		}
		if got := orchestratorID(res); got != "michael" {
			return fmt.Errorf("who_else(%s) orchestrator.id mismatch: got %q, want %q", s.ID, got, "michael")
		}
		if _, ok := res["humans"].([]any); !ok {
			return fmt.Errorf("who_else(%s) missing humans array: %+v", s.ID, res)
		}
		dump(res)
	}
	return nil
}

func smokePosts(ctx context.Context, sess *mcp.ClientSession, huddleID, designerKey, implementorKey string) error {
	posts := []struct {
		label string
		args  map[string]any
	}{
		{"orchestrator", map[string]any{
			"huddleId": huddleID,
			"body":     "kickoff: designer + implementor pairing on the search filter pill. who's driving what?",
		}},
		{"designer", map[string]any{
			"key":  designerKey,
			"body": "i'll mock the sticky pill above the results list and share a screenshot in a few",
		}},
		{"implementor", map[string]any{
			"key":  implementorKey,
			"body": "works for me — once the mock lands i can wire it into the existing filter state",
		}},
	}
	for _, p := range posts {
		step("huddle.post (" + p.label + ")")
		res, err := callJSON(ctx, sess, "huddle.post", p.args)
		if err != nil {
			return fmt.Errorf("post(%s): %w", p.label, err)
		}
		dump(res)
	}
	return nil
}

func smokeRead(ctx context.Context, sess *mcp.ClientSession, designerKey string) error {
	step("huddle.read (designer perspective, limit 20)")
	readRes, err := callJSON(ctx, sess, "huddle.read", map[string]any{
		"key":   designerKey,
		"limit": 20,
	})
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	dump(readRes)
	return nil
}

func smokeInviteHuman(ctx context.Context, sess *mcp.ClientSession, huddleID, humanRef string) error {
	step("huddle.invite_human (" + humanRef + ")")
	res, err := callJSON(ctx, sess, "huddle.invite_human", map[string]any{
		"huddleId": huddleID,
		"humans":   []string{humanRef},
	})
	if err != nil {
		return fmt.Errorf("invite_human: %w", err)
	}
	dump(res)
	return nil
}

func smokeClose(ctx context.Context, sess *mcp.ClientSession, huddleID string) error {
	step("huddle.close")
	closeRes, err := callJSON(ctx, sess, "huddle.close", map[string]any{"huddleId": huddleID})
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}
	dump(closeRes)
	return nil
}

func logCreated(huddleID string, channel any) {
	huddleIDShort := huddleID
	if len(huddleIDShort) > 16 {
		huddleIDShort = huddleIDShort[:16] + "..."
	}
	fmt.Printf("    huddle %s, channel %v\n", huddleIDShort, channel)
}

func callJSON(ctx context.Context, sess *mcp.ClientSession, name string, args map[string]any) (map[string]any, error) {
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return nil, fmt.Errorf("tool returned error: %s", b.String())
	}
	if res.StructuredContent == nil {
		return map[string]any{}, nil
	}
	buf, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return nil, fmt.Errorf("marshal structured: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, fmt.Errorf("unmarshal structured: %w", err)
	}
	return m, nil
}

type smokeSeat struct {
	ID, Key, DisplayName string
}

func extractSeats(m map[string]any) []smokeSeat {
	raw, _ := m["seats"].([]any)
	out := make([]smokeSeat, 0, len(raw))
	for _, r := range raw {
		sm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, smokeSeat{
			ID:          asString(sm["id"]),
			Key:         asString(sm["key"]),
			DisplayName: asString(sm["displayName"]),
		})
	}
	return out
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func orchestratorID(m map[string]any) string {
	orch, _ := m["orchestrator"].(map[string]any)
	return asString(orch["id"])
}

// envWithOverrides returns base with any keys present in overrides removed,
// followed by the overrides as KEY=value entries. Naive append(os.Environ(),
// "KEY=val") leaves the parent's KEY=... entry intact; on Linux getenv
// returns the first match, so the override is silently ignored. Filtering
// the keys out of base first makes the override actually apply.
func envWithOverrides(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if _, skip := overrides[kv[:eq]]; skip {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func step(name string) {
	fmt.Printf("\n--- %s ---\n", name)
}

func dump(v any) {
	b, _ := json.MarshalIndent(v, "  ", "  ")
	fmt.Printf("  %s\n", b)
}
