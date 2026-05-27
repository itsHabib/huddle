// Package main is the huddle MCP server entry point.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/itsHabib/huddle/internal/config"
	"github.com/itsHabib/huddle/internal/server"
	"github.com/itsHabib/huddle/internal/slack"
	"github.com/itsHabib/huddle/internal/store"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const version = "v0.0.1"

func main() {
	// jsonschema-go publishes `"type": ["null", "T"]` for slice and pointer
	// fields. That's valid JSON Schema, but Claude Code's MCP harness only
	// understands singular `"type": "T"` and falls back to sending the value
	// as a string when it sees a type union — which fails server-side
	// validation. The library exposes a debug env that switches slice fields
	// to singular `"type": "array"`; setting it here lets the multi-seat
	// huddle.create call through the harness. Pointer fields (TTLHours,
	// ListArgs.Active, ReadArgs.Since) still publish `["null", "T"]`, but
	// they're all optional — clients just omit them.
	// Only set the env if the operator hasn't already configured it, so a
	// caller running with a custom JSONSCHEMAGODEBUG (e.g. for unrelated
	// debugging) isn't silently clobbered.
	// TODO: replace with explicit InputSchema overrides per tool when we're
	// willing to maintain hand-rolled schemas.
	if os.Getenv("JSONSCHEMAGODEBUG") == "" {
		_ = os.Setenv("JSONSCHEMAGODEBUG", "typeschemasnull=1")
	}
	os.Exit(run(os.Args))
}

func run(args []string) int {
	for _, arg := range args[1:] {
		switch arg {
		case "--version", "-version":
			_, _ = fmt.Fprintf(os.Stdout, "huddle %s\n", version)

			return 0
		}
	}

	cfg, err := config.Load()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)

		return 2
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	st, err := store.New(cfg.StateDir)
	if err != nil {
		log.Error("open store", slog.String("error", err.Error()))

		return 1
	}

	defer func() {
		if closeErr := st.Close(); closeErr != nil {
			log.Error("close store", slog.String("error", closeErr.Error()))
		}
	}()

	slackAdapter := slack.NewAdapter(cfg)
	deps := server.Deps{
		Slack: slackAdapter,
		Store: st,
		Cfg:   cfg,
		Log:   log,
	}

	srv := mcp.NewServer(&mcp.Implementation{Name: "huddle", Version: version}, nil)
	server.RegisterVerbStubs(srv, deps)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("huddle MCP listening on stdio")
	if runErr := srv.Run(ctx, &mcp.StdioTransport{}); runErr != nil && !errors.Is(runErr, context.Canceled) {
		log.Error("server stopped", slog.String("error", runErr.Error()))

		return 1
	}

	return 0
}
