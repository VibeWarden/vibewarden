package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/app/promptkickoff"
	clitemplates "github.com/vibewarden/vibewarden/internal/cli/templates"
)

// NewPromptTemplateCmd creates the "vibew prompt-template" command.
//
// It prints the canonical agent kickoff prompt to stdout. Two flavors are
// supported:
//
//   - Default (dev only): install + scaffold + vibew init + vibew add tls + vibew dev.
//   - --deploy: adds vibew bundle + scp + ssh + docker load + healthcheck.
//
// The output is pipeable directly into a chat session — it contains no log lines,
// no preamble, and no trailing "OK" message. The first line is always the
// version-stamped header so the agent can confirm it is running an aligned binary.
//
// Exit codes:
//   - 0: success
//   - 1: validation failure (missing required flags)
func NewPromptTemplateCmd() *cobra.Command {
	var (
		name     string
		describe string
		domain   string
		deploy   bool
	)

	cmd := &cobra.Command{
		Use:   "prompt-template",
		Short: "Print the canonical agent kickoff prompt to stdout",
		Long: `Print the canonical agent kickoff prompt to stdout.

The prompt is generated from a template embedded in this binary so it always
matches the version of vibew you have installed. Paste the output directly into
a chat session with an AI coding agent to set up VibeWarden correctly.

Two flavors are available:

  Default (dev only):
    vibew prompt-template --name <project> --describe "<description>"

  With deploy steps:
    vibew prompt-template --deploy --name <project> --describe "<description>" --domain <fqdn>

--domain is required when --deploy is set. Omitting it fails loudly with exit 1
rather than rendering a placeholder — the agent needs a real domain to configure TLS.

Examples:
  vibew prompt-template --name myapp --describe "todo list app"
  vibew prompt-template --deploy --name myapp --describe "todo list app" --domain myapp.example.com
  vibew prompt-template --name myapp --describe "todo list app" | pbcopy`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			version := cmd.Root().Version
			if version == "" {
				version = "dev"
			}

			renderer := templateadapter.NewRenderer(clitemplates.FS)
			svc := promptkickoff.NewService(renderer)

			opts := promptkickoff.Options{
				Name:         name,
				Describe:     describe,
				Domain:       domain,
				Deploy:       deploy,
				VibewVersion: version,
			}

			out, err := svc.Render(opts)
			if err != nil {
				return fmt.Errorf("error: %w", err)
			}

			_, err = fmt.Fprint(cmd.OutOrStdout(), string(out))
			return err
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "project name (required)")
	cmd.Flags().StringVar(&describe, "describe", "", "one-line project description (required)")
	cmd.Flags().StringVar(&domain, "domain", "", "FQDN the app will be served on (required with --deploy)")
	cmd.Flags().BoolVar(&deploy, "deploy", false, "include deploy steps (bundle + scp + ssh + healthcheck)")

	return cmd
}
