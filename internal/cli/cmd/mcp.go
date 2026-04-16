package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/mcp"
)

// NewMCPCmd creates the "vibew mcp" subcommand.
//
// The command starts a Model Context Protocol (MCP) server on stdio.
// It reads JSON-RPC 2.0 requests from stdin and writes responses to stdout.
// All diagnostic output goes to stderr so it does not pollute the MCP stream.
//
// The tool list in --help is generated at command-construction time from the
// live registry (see buildMCPLongHelp and mcpFirstSentence). Adding a new
// tool in internal/mcp/ automatically shows up in "vibew mcp --help" — the
// list cannot drift out of sync with the registered handlers.
//
// Intended usage in an AI agent / IDE MCP configuration:
//
//	{
//	  "mcpServers": {
//	    "vibewarden": {
//	      "command": "vibew",
//	      "args": ["mcp"]
//	    }
//	  }
//	}
func NewMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start the VibeWarden MCP server (stdio JSON-RPC 2.0)",
		Long:  buildMCPLongHelp(),
		// SilenceUsage prevents cobra from printing usage on errors produced
		// inside the MCP loop — those go to stderr as slog messages.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			version := cmd.Root().Version
			if version == "" {
				version = "dev"
			}

			srv := mcp.NewServer("vibewarden", version, logger)
			mcp.RegisterDefaultTools(srv)

			logger.Info("vibewarden MCP server starting", "version", version)
			return srv.Serve(cmd.Context(), os.Stdin, os.Stdout)
		},
	}

	return cmd
}

// buildMCPLongHelp constructs the "vibew mcp --help" text from the live
// default-tools registry, so the list stays in sync with whatever
// RegisterDefaultTools registers. The throwaway server is only used to
// enumerate tool metadata; it is never served.
func buildMCPLongHelp() string {
	discardLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tmp := mcp.NewServer("vibewarden", "help", discardLogger)
	mcp.RegisterDefaultTools(tmp)

	var tools strings.Builder
	for _, t := range tmp.Tools() {
		fmt.Fprintf(&tools, "  %-36s  %s\n", t.Name, mcpFirstSentence(t.Description))
	}

	return fmt.Sprintf(`Start a Model Context Protocol (MCP) server on stdio.

The server reads JSON-RPC 2.0 messages from stdin and writes responses to
stdout, following the MCP 2024-11-05 specification. All diagnostic output
goes to stderr.

Available tools:
%s
Configure in your AI agent / IDE:

  {
    "mcpServers": {
      "vibewarden": {
        "command": "vibew",
        "args": ["mcp"]
      }
    }
  }`, tools.String())
}

// mcpFirstSentence returns the first sentence of a tool description, for
// the compact listing in --help. If no sentence delimiter is found the
// description is returned unchanged.
func mcpFirstSentence(desc string) string {
	desc = strings.TrimSpace(desc)
	if i := strings.Index(desc, ". "); i > 0 {
		return desc[:i+1]
	}
	return desc
}
