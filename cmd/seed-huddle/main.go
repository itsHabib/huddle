// Package main spins up a real, long-lived huddle for hand-off to other
// Claude Code sessions. Useful when an operator wants to set up a
// multi-agent coordination room and then walk over to other sessions to
// take seats. Unlike cmd/smoke, this does NOT post anything or archive
// the channel — it just creates and prints the huddle id + seat keys in
// a copy-paste-friendly form, then exits.
//
// Requires HUDDLE_SLACK_BOT_TOKEN in the env. Honors
// HUDDLE_ORCHESTRATOR_SLACK_USER_ID for the auto-invite. Other huddle
// env vars are inherited from the parent process and apply to the
// long-lived state — pass the same HUDDLE_STATE_DIR that your Claude
// Code MCP uses so the resulting huddle is also visible to other
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

func run(argv []string) error {
	if len(argv) < 3 {
		return errors.New("usage: seed-huddle <purpose> <seat-id> [<seat-id> ...]")
	}
	purpose := strings.TrimSpace(argv[1])
	if purpose == "" {
		return errors.New("purpose must be non-empty")
	}

	seatIDs := argv[2:]
	seats := make([]map[string]any, 0, len(seatIDs))
	for _, id := range seatIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return errors.New("seat id must be non-empty")
		}
		seats = append(seats, map[string]any{
			"id":          id,
			"displayName": id,
		})
	}

	if os.Getenv("HUDDLE_SLACK_BOT_TOKEN") == "" {
		return errors.New("HUDDLE_SLACK_BOT_TOKEN must be set in the env")
	}
	if os.Getenv("HUDDLE_ORCHESTRATOR_SLACK_USER_ID") == "" {
		fmt.Fprintln(os.Stderr, "WARN: HUDDLE_ORCHESTRATOR_SLACK_USER_ID is not set; you will not be auto-invited.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.Command("go", "run", "./cmd/huddle")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "seed-huddle", Version: "v0.0.1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = sess.Close() }()

	orchID := strings.TrimSpace(os.Getenv("HUDDLE_ORCHESTRATOR_ID"))
	if orchID == "" {
		orchID = "operator"
	}
	orchName := strings.TrimSpace(os.Getenv("HUDDLE_ORCHESTRATOR_DISPLAY_NAME"))
	if orchName == "" {
		orchName = orchID
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: "huddle.create",
		Arguments: map[string]any{
			"purpose":      purpose,
			"orchestrator": map[string]any{"id": orchID, "displayName": orchName},
			"seats":        seats,
		},
	})
	if err != nil {
		return fmt.Errorf("huddle.create: %w", err)
	}
	if res.IsError {
		var msg string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg += tc.Text
			}
		}
		return fmt.Errorf("huddle.create returned error: %s", msg)
	}

	buf, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var out struct {
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
	if err := json.Unmarshal(buf, &out); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

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

	return nil
}
