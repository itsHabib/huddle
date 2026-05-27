// Package main spins up a real, long-lived huddle for hand-off to other
// Claude Code sessions. Useful when an operator wants to set up a
// multi-agent coordination room and then walk over to other sessions to
// take seats. Unlike cmd/smoke, this does NOT post anything or archive
// the channel — it just creates and prints the huddle id + seat keys in
// a copy-paste-friendly form, then exits.
//
// Environment:
//
//   - HUDDLE_SLACK_BOT_TOKEN (required) — same token the huddle MCP needs.
//   - HUDDLE_ORCHESTRATOR_SLACK_USER_ID (optional) — Slack user id to
//     auto-invite to the channel; warns to stderr when unset.
//   - HUDDLE_ORCHESTRATOR_ID (optional, default "operator") — stable id
//     persisted on the huddle row and surfaced via huddle.who_else.
//   - HUDDLE_ORCHESTRATOR_DISPLAY_NAME (optional, defaults to ORCHESTRATOR_ID)
//     — display name shown in Slack messages.
//
// Other huddle env vars are inherited from the parent process and apply
// to the long-lived state — pass the same HUDDLE_STATE_DIR that your
// Claude Code MCP uses so the resulting huddle is also visible to other
// huddle MCP clients on this machine.
//
// Usage:
//
//	go run ./cmd/seed-huddle <purpose> <seat-id> [<seat-id> ...]
//
// Example:
//
//	go run ./cmd/seed-huddle "pair on filter UX" designer implementor
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
	if err := run(os.Args); err != nil {
		log.Fatalf("seed-huddle FAILED: %v", err)
	}
}

type seedOut struct {
	HuddleID     string `json:"huddleId"`
	Channel      string `json:"channel"`
	Orchestrator struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	} `json:"orchestrator"`
	Seats []struct {
		ID          string `json:"id"`
		Key         string `json:"key"`
		DisplayName string `json:"displayName"`
	} `json:"seats"`
}

func run(argv []string) error {
	purpose, seats, err := parseSeedArgs(argv)
	if err != nil {
		return err
	}

	if err := requireSeedEnv(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sess, cleanup, err := dialHuddle(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	orchID, orchName := orchestratorFromEnv()
	out, err := callCreate(ctx, sess, purpose, orchID, orchName, seats)
	if err != nil {
		return err
	}

	printSeedSummary(out)
	return nil
}

func parseSeedArgs(argv []string) (string, []map[string]any, error) {
	if len(argv) < 3 {
		return "", nil, errors.New("usage: seed-huddle <purpose> <seat-id> [<seat-id> ...]")
	}
	purpose := strings.TrimSpace(argv[1])
	if purpose == "" {
		return "", nil, errors.New("purpose must be non-empty")
	}

	seatIDs := argv[2:]
	seats := make([]map[string]any, 0, len(seatIDs))
	for _, id := range seatIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return "", nil, errors.New("seat id must be non-empty")
		}
		seats = append(seats, map[string]any{
			"id":          id,
			"displayName": id,
		})
	}
	return purpose, seats, nil
}

func requireSeedEnv() error {
	if os.Getenv("HUDDLE_SLACK_BOT_TOKEN") == "" {
		return errors.New("HUDDLE_SLACK_BOT_TOKEN must be set in the env")
	}
	if os.Getenv("HUDDLE_ORCHESTRATOR_SLACK_USER_ID") == "" {
		fmt.Fprintln(os.Stderr, "WARN: HUDDLE_ORCHESTRATOR_SLACK_USER_ID is not set; you will not be auto-invited.")
	}
	return nil
}

func orchestratorFromEnv() (string, string) {
	orchID := strings.TrimSpace(os.Getenv("HUDDLE_ORCHESTRATOR_ID"))
	if orchID == "" {
		orchID = "operator"
	}
	orchName := strings.TrimSpace(os.Getenv("HUDDLE_ORCHESTRATOR_DISPLAY_NAME"))
	if orchName == "" {
		orchName = orchID
	}
	return orchID, orchName
}

func dialHuddle(ctx context.Context) (*mcp.ClientSession, func(), error) {
	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/huddle")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "seed-huddle", Version: "v0.0.1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, func() {}, fmt.Errorf("connect: %w", err)
	}
	return sess, func() { _ = sess.Close() }, nil
}

func callCreate(ctx context.Context, sess *mcp.ClientSession, purpose, orchID, orchName string, seats []map[string]any) (seedOut, error) {
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.create",
		Arguments: map[string]any{
			"purpose":      purpose,
			"orchestrator": map[string]any{"id": orchID, "displayName": orchName},
			"seats":        seats,
		},
	})
	if err != nil {
		return seedOut{}, fmt.Errorf("huddle.create: %w", err)
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return seedOut{}, fmt.Errorf("huddle.create returned error: %s", b.String())
	}

	buf, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return seedOut{}, fmt.Errorf("marshal: %w", err)
	}
	var out seedOut
	if err := json.Unmarshal(buf, &out); err != nil {
		return seedOut{}, fmt.Errorf("unmarshal: %w", err)
	}
	return out, nil
}

func printSeedSummary(out seedOut) {
	fmt.Println()
	fmt.Println("Huddle created. Hand a key to each seat agent.")
	fmt.Println()
	fmt.Printf("  huddleId : %s\n", out.HuddleID)
	fmt.Printf("  channel  : %s\n", out.Channel)
	fmt.Printf("  orch     : %s (%s)\n", out.Orchestrator.DisplayName, out.Orchestrator.ID)
	fmt.Println()
	fmt.Println("  Seats:")
	for _, s := range out.Seats {
		fmt.Printf("    - id=%s  key=%s\n", s.ID, s.Key)
	}
	fmt.Println()
	fmt.Println("In each seat's Claude Code session, paste the seat's key and ask the agent to:")
	fmt.Println("  - call mcp__huddle__huddle_who_else { key: \"<their key>\" } to learn who else is in the room")
	fmt.Println("  - call mcp__huddle__huddle_read    { key: \"<their key>\", limit: 20 } to catch up")
	fmt.Println("  - call mcp__huddle__huddle_post    { key: \"<their key>\", body: \"...\" } to post")
	fmt.Println()
	fmt.Println("The huddle stays open until someone calls mcp__huddle__huddle_close { huddleId: \"" + out.HuddleID + "\" }.")
}
