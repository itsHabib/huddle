// Package main is a tiny CLI wrapper around the huddle MCP server's
// seat-side verbs: read, post, who-else. It spawns a fresh huddle binary
// as a subprocess per invocation, so it sees the latest persisted state
// without depending on any long-running MCP process. Useful when you
// want to act as a seat from outside Claude Code (e.g., scripts) or
// when a long-running MCP's view has gone stale.
//
// Usage:
//
//	seat read     --key K_…
//	seat post     --key K_… --body "..."
//	seat who-else --key K_…
//
// Environment: HUDDLE_SLACK_BOT_TOKEN is required for `read` and `post`
// (both hit Slack). `who-else` only touches local state and works
// tokenless; the spawned MCP subprocess boots either way and the slack
// adapter errors out at call time for Slack-touching verbs when the
// token is unset.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: seat <read|post|who-else> --key K_... [--body BODY]")
	}

	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	key := fs.String("key", "", "seat key (required)")
	body := fs.String("body", "", "message body (post only)")
	limit := fs.Int("limit", 20, "read limit (read only)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		log.Fatal(err)
	}
	if *key == "" {
		log.Fatal("--key is required")
	}

	if err := run(cmd, *key, *body, *limit); err != nil {
		log.Fatalf("seat %s failed: %v", cmd, err)
	}
}

func run(verb, key, body string, limit int) error {
	// Slack-touching verbs (read / post) need HUDDLE_SLACK_BOT_TOKEN; gate
	// only those. who-else only touches local state — the spawned MCP
	// boots regardless and serves it without a token.
	// TrimSpace to match config.Load's normalization so a token of pure
	// whitespace fails fast here with a clear message instead of passing
	// the gate and then having the spawned server return ErrNoToken.
	if (verb == "read" || verb == "post") && strings.TrimSpace(os.Getenv("HUDDLE_SLACK_BOT_TOKEN")) == "" {
		return errors.New("HUDDLE_SLACK_BOT_TOKEN must be set in the env for " + verb)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "./cmd/huddle")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "seat", Version: "v0.0.1"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = sess.Close() }()

	var (
		name string
		args map[string]any
	)
	switch verb {
	case "read":
		name = "huddle.read"
		args = map[string]any{"key": key, "limit": limit}
	case "post":
		if body == "" {
			return errors.New("--body is required for post")
		}
		name = "huddle.post"
		args = map[string]any{"key": key, "body": body}
	case "who-else":
		name = "huddle.who_else"
		args = map[string]any{"key": key}
	default:
		return fmt.Errorf("unknown verb %q (use read | post | who-else)", verb)
	}

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err
	}
	if res.IsError {
		var b strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		return fmt.Errorf("%s returned error: %s", name, b.String())
	}

	buf, _ := json.MarshalIndent(res.StructuredContent, "", "  ")
	fmt.Println(string(buf))
	return nil
}
