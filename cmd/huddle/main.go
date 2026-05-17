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
		var validation *config.ValidationError
		if errors.As(err, &validation) {
			fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)

			return 2
		}

		fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)

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
