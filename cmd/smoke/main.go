// Package main is a smoke harness that drives the huddle MCP binary as a
// subprocess and exercises the v0 verb surface end-to-end against a real
// Slack workspace. Intended for manual runs ("does this work hand-on?"),
// not the CI test suite.
//
// Requires HUDDLE_SLACK_BOT_TOKEN in the env (a real xoxb- token with the
// scopes the slack adapter needs). Other huddle env vars are overridden to
// keep smoke runs isolated from a developer's live MCP state.
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

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "huddle-smoke-*")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cmd := exec.Command("go", "run", "./cmd/huddle")
	cmd.Env = append(os.Environ(),
		"HUDDLE_STATE_DIR="+tmpDir,
		"HUDDLE_CHANNEL_PREFIX=huddle-smoke-",
		"HUDDLE_LOG_LEVEL=info",
	)
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "smoke", Version: "v0.0.1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = sess.Close() }()

	step("huddle.create (designer + implementor)")
	// Note: purpose deliberately does not include the word "smoke" — the
	// channel name is "<prefix>-<slug>-<short id>", and the configured
	// HUDDLE_CHANNEL_PREFIX already encodes that, so leading "smoke" in
	// the purpose would produce an awkward "huddle-smoke-smoke-…" name.
	createRes, err := callJSON(ctx, sess, "huddle.create", map[string]any{
		"purpose": "designer + implementor pairing on search filter UX",
		"orchestrator": map[string]any{
			"id":          "michael",
			"displayName": "Michael (orchestrator)",
		},
		"seats": []map[string]any{
			{"id": "designer", "displayName": "Designer Agent"},
			{"id": "implementor", "displayName": "Implementor Agent"},
		},
	})
	if err != nil {
		return fmt.Errorf("huddle.create: %w", err)
	}
	dump(createRes)

	huddleID, _ := createRes["huddleId"].(string)
	seats := extractSeats(createRes)
	if huddleID == "" || len(seats) != 2 {
		return fmt.Errorf("create result missing huddleId or seats: %+v", createRes)
	}
	if got := orchestratorID(createRes); got != "michael" {
		return fmt.Errorf("create result orchestrator.id mismatch: got %q, want %q", got, "michael")
	}
	designer, implementor := seats[0], seats[1]
	huddleIDShort := huddleID
	if len(huddleIDShort) > 16 {
		huddleIDShort = huddleIDShort[:16] + "..."
	}
	fmt.Printf("    huddle %s, channel %v\n", huddleIDShort, createRes["channel"])

	// At this point a real Slack channel exists. Try to archive it on any
	// later failure so we don't leak public channels into the workspace.
	closeOnDefer := true
	defer func() {
		if !closeOnDefer {
			return
		}
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		if _, cerr := callJSON(cctx, sess, "huddle.close", map[string]any{"huddleId": huddleID}); cerr != nil {
			fmt.Fprintf(os.Stderr, "WARN: deferred huddle.close failed: %v\n", cerr)
		}
	}()

	for _, s := range seats {
		step(fmt.Sprintf("huddle.who_else (%s)", s.ID))
		res, err := callJSON(ctx, sess, "huddle.who_else", map[string]any{"key": s.Key})
		if err != nil {
			return fmt.Errorf("who_else(%s): %w", s.ID, err)
		}
		if got := orchestratorID(res); got != "michael" {
			return fmt.Errorf("who_else(%s) orchestrator.id mismatch: got %q, want %q", s.ID, got, "michael")
		}
		dump(res)
	}

	posts := []struct {
		label string
		args  map[string]any
	}{
		{"orchestrator", map[string]any{
			"huddleId": huddleID,
			"body":     "kickoff: designer + implementor pairing on the search filter pill. who's driving what?",
		}},
		{"designer", map[string]any{
			"key":  designer.Key,
			"body": "i'll mock the sticky pill above the results list and share a screenshot in a few",
		}},
		{"implementor", map[string]any{
			"key":  implementor.Key,
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

	step("huddle.read (designer perspective, limit 20)")
	readRes, err := callJSON(ctx, sess, "huddle.read", map[string]any{
		"key":   designer.Key,
		"limit": 20,
	})
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	dump(readRes)

	step("huddle.close")
	closeRes, err := callJSON(ctx, sess, "huddle.close", map[string]any{"huddleId": huddleID})
	if err != nil {
		return fmt.Errorf("close: %w", err)
	}
	dump(closeRes)
	closeOnDefer = false

	return nil
}

func callJSON(ctx context.Context, sess *mcp.ClientSession, name string, args map[string]any) (map[string]any, error) {
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return nil, err
	}
	if res.IsError {
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg += tc.Text
			}
		}
		return nil, fmt.Errorf("tool returned error: %s", msg)
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

func step(name string) {
	fmt.Printf("\n--- %s ---\n", name)
}

func dump(v any) {
	b, _ := json.MarshalIndent(v, "  ", "  ")
	fmt.Printf("  %s\n", b)
}
