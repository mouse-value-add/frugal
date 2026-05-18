package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/frugalsh/frugal/internal/config"
	"github.com/frugalsh/frugal/internal/mcp"
	"github.com/frugalsh/frugal/internal/mcp/tools"
	"github.com/frugalsh/frugal/internal/provider/serper"
	"github.com/frugalsh/frugal/internal/provider/tavily"
	"github.com/frugalsh/frugal/internal/search"
)

// runMCP dispatches the `frugal mcp <subcommand>` family. Returns the
// process exit code.
//
// Subcommands:
//   - serve:   run Frugal as an MCP server (stdio default; --http :PORT for Streamable HTTP)
//   - install: write MCP server config into Claude Desktop / Cursor / Claude Code (Phase 1 PR 6)
//
// Anything else falls through to a usage error.
func runMCP(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: frugal mcp <serve|install> [flags]")
		return 2
	}
	switch args[0] {
	case "serve":
		return runMCPServe(args[1:])
	case "install":
		return stubNotImplemented("mcp install", "Phase 1 PR 6")
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp subcommand %q (want serve | install)\n", args[0])
		return 2
	}
}

// runMCPServe runs the MCP server. stdio is the default — what Claude
// Desktop, Claude Code, and Cursor consume for locally-installed servers.
// --http :PORT switches to Streamable HTTP for remote deployments and HTTP
// clients.
//
// Both transports honor SIGINT / SIGTERM with a graceful shutdown.
func runMCPServe(args []string) int {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	httpAddr := fs.String("http", "", "if set, serve over Streamable HTTP on this address (e.g. :8765) instead of stdio")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: frugal mcp serve [--http ADDR]")
		fmt.Fprintln(os.Stderr, "Run Frugal as an MCP server. Default transport is stdio")
		fmt.Fprintln(os.Stderr, "(what Claude Desktop, Claude Code, and Cursor consume).")
		fmt.Fprintln(os.Stderr, "Pass --http :PORT for Streamable HTTP (remote / HTTP clients).")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	configPath := "config/models.yaml"
	if p := os.Getenv("FRUGAL_CONFIG"); p != "" {
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		// stdio mode keeps stdout free of non-JSON bytes — failure logs go to
		// stderr, which is the contract every MCP client respects.
		fmt.Fprintf(os.Stderr, "frugal mcp serve: load config: %v\n", err)
		return 1
	}

	// stdio transport is single-session; logging to stderr is safe and
	// preserves the MCP newline-delimited JSON-RPC contract on stdout.
	srv := mcp.New("frugal", version(), slog.Default())

	searchers := buildSearchers(cfg)
	tools.RegisterSearch(srv.Inner, searchers)
	if len(searchers) == 0 {
		slog.Warn("mcp serve: no search providers configured — frugal__search will not be advertised. " +
			"Set TAVILY_API_KEY or SERPER_API_KEY to enable.")
	} else {
		names := make([]string, 0, len(searchers))
		for _, s := range searchers {
			names = append(names, s.Name())
		}
		slog.Info("mcp serve: frugal__search registered", "providers", names)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *httpAddr != "" {
		if err := srv.ServeHTTP(ctx, *httpAddr); err != nil {
			fmt.Fprintf(os.Stderr, "frugal mcp serve: %v\n", err)
			return 1
		}
		return 0
	}
	if err := srv.ServeStdio(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "frugal mcp serve: %v\n", err)
		return 1
	}
	return 0
}

// buildSearchers instantiates one search.Searcher per search_providers
// entry that has its API-key env var set. Unknown provider names log a
// warning and are skipped — operators can edit ~/.frugal/config/models.yaml
// to add new providers, but driver wiring lives here in code.
func buildSearchers(cfg *config.Config) []search.Searcher {
	var out []search.Searcher
	for name, sp := range cfg.SearchProviders {
		key := os.Getenv(sp.APIKeyEnv)
		if key == "" {
			continue
		}
		switch name {
		case "tavily":
			out = append(out, tavily.New(key, sp.BaseURL, sp.CostPerCall))
		case "serper":
			out = append(out, serper.New(key, sp.BaseURL, sp.CostPerCall))
		default:
			slog.Warn("mcp serve: unknown search provider in config; ignoring",
				"name", name, "hint", "add a driver in internal/provider/<name> and a switch case here")
		}
	}
	return out
}
