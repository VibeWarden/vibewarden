package cmd

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/mcp"
)

// NewMCPCmd creates the "vibew mcp" subcommand.
//
// The command starts a Model Context Protocol (MCP) server on stdio.
// It reads JSON-RPC 2.0 requests from stdin and writes responses to stdout.
// All diagnostic output goes to stderr so it does not pollute the MCP stream.
//
// The server exposes four tools:
//   - vibewarden_status  — check whether the sidecar is running
//   - vibewarden_doctor  — run health checks
//   - vibewarden_validate — validate vibewarden.yaml
//   - vibewarden_explain  — explain what a config does
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
		Long: `Start a Model Context Protocol (MCP) server on stdio.

The server reads JSON-RPC 2.0 messages from stdin and writes responses to
stdout, following the MCP 2024-11-05 specification. All diagnostic output
goes to stderr.

Available tools:
  vibewarden_status   — check whether the VibeWarden sidecar is running
  vibewarden_doctor   — run the full diagnostics suite
  vibewarden_validate — validate a vibewarden.yaml configuration file
  vibewarden_explain  — describe what a configuration does in plain language

Configure in your AI agent / IDE:

  {
    "mcpServers": {
      "vibewarden": {
        "command": "vibew",
        "args": ["mcp"]
      }
    }
  }`,
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
