package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// IsTTY reports whether fd is connected to a terminal.
// It uses term.IsTerminal (ioctl-based) which is more reliable than os.File.Stat
// for detecting non-TTY contexts such as CI, piped stdin, or AI agent sessions.
// The function is a package-level variable so that tests can replace it without
// build-tag gymnastics.
var IsTTY = func(fd *os.File) bool {
	return term.IsTerminal(int(fd.Fd()))
}

// promptString writes prompt to w and reads a single line of input from r.
// Leading/trailing whitespace is trimmed from the response.
// If the user just presses Enter, defaultVal is returned.
func promptString(w *os.File, r *bufio.Reader, prompt, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(w, "%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Fprintf(w, "%s: ", prompt)
	}
	line, err := r.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// NewInitCmd creates the `vibew init` subcommand.
//
// The command scaffolds a new VibeWarden project in the current working
// directory. The project name is derived from the directory's base name
// (e.g., running in ~/projects/myapp produces project name "myapp").
//
// This matches `vibew wrap`'s convention: cd into the directory first,
// then run the command. No subdirectory creation.
//
// Usage:
//
//	mkdir myapp && cd myapp
//	vibew init
//	vibew init --port 8080
//	vibew init --name myapp
//	vibew init --describe "a task management API"
func NewInitCmd() *cobra.Command {
	var (
		port     int
		force    bool
		version  string
		describe string
		name     string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new VibeWarden project in the current directory",
		Long: `Scaffold a new project with VibeWarden security pre-configured.

The command scaffolds into the current working directory, creating:
  - vibewarden.yaml (local dev config: TLS self-signed, port 8443)
  - vibewarden.production.yaml (production overrides: letsencrypt, port 443)
  - .vibewarden-version (pins the vibew version for this project)
  - AGENTS-VIBEWARDEN.md with all agent instructions (auto-generated, vibew-owned)
  - AGENTS.md with a reference to AGENTS-VIBEWARDEN.md (user-owned)
  - PROJECT.md with project description (when --describe is given)
  - Dockerfile (generic placeholder with examples for common stacks)
  - .gitignore

vibewarden.yaml is your LOCAL dev config. vibewarden.production.yaml contains
production overrides. vibew bundle deep-merges the production overrides into
the self-contained deploy artifact under .vibewarden/bundle/. Never put
production-only config in vibewarden.yaml.

The project name is derived from the current directory's base name.
Use --name to set an explicit project name for Docker Compose image
naming and deploy directories.

Examples:
  mkdir myapp && cd myapp && vibew init
  vibew init --port 8080
  vibew init --name my-custom-project
  vibew init --describe "a task management API"
  vibew init --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			interactive := IsTTY(os.Stdin)

			stdinReader := bufio.NewReader(os.Stdin)

			// Always scaffold in the current directory. Derive project name
			// from the directory's base name (same convention as vibew wrap).
			// The --name flag only sets the name: field in vibewarden.yaml
			// for Docker Compose project scoping; it does not change the
			// scaffold directory.
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting current directory: %w", err)
			}
			projectName := filepath.Base(cwd)
			parentDir := filepath.Dir(cwd)

			// Resolve description: --describe flag > interactive prompt > empty.
			if describe == "" && interactive {
				chosen, err := promptString(os.Stderr, stdinReader, "Project description (optional)", "")
				if err != nil {
					return fmt.Errorf("prompting for description: %w", err)
				}
				describe = chosen
			}

			renderer := templateadapter.NewRenderer(templates.FS)
			svc := scaffoldapp.NewInitProjectService(renderer, nil)

			opts := scaffoldapp.InitProjectOptions{
				ProjectName: projectName,
				Port:        port,
				Force:       force,
				Version:     version,
				Description: describe,
				Name:        name,
			}

			if err := svc.InitProject(context.Background(), parentDir, opts); err != nil {
				if errors.Is(err, domainscaffold.ErrInsideExistingGitRepo) {
					return fmt.Errorf("%w\n\nUse --force to scaffold inside this git repository.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
				}
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
				}
				return err
			}

			printInitSuccessMessage(cmd, projectName, opts)
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 3000, "HTTP port the app listens on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&version, "version", "", "VibeWarden version to pin in .vibewarden-version (default: latest)")
	cmd.Flags().StringVar(&describe, "describe", "", "one-line description of what the project builds; written to PROJECT.md and injected into agent files")
	cmd.Flags().StringVar(&name, "name", "", "project name for Docker Compose project naming and deploy directories (default: current directory name)")

	return cmd
}

// printInitSuccessMessage writes next-steps guidance to cmd's output writer.
func printInitSuccessMessage(cmd *cobra.Command, projectName string, opts scaffoldapp.InitProjectOptions) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Project %q created!\n", projectName)
	if opts.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", opts.Description)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Files created:")

	fmt.Fprintf(w, "  vibewarden.yaml               Local dev config (self-signed TLS, port 8443)\n")
	fmt.Fprintf(w, "  vibewarden.production.yaml    Production overrides (letsencrypt, port 443)\n")
	if opts.Description != "" {
		fmt.Fprintf(w, "  PROJECT.md               Project description\n")
	}
	fmt.Fprintf(w, "  AGENTS-VIBEWARDEN.md     Agent instructions (vibew-owned, auto-generated)\n")
	fmt.Fprintf(w, "  AGENTS.md                Agent instructions entry point (user-owned)\n")
	fmt.Fprintf(w, "  Dockerfile               Container build file (generic placeholder)\n")
	fmt.Fprintf(w, "  .gitignore               Git ignore rules\n")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintln(w, "  vibew dev                Start dev environment (app + sidecar)")
	fmt.Fprintln(w, "  vibew status             Check component health")
	fmt.Fprintln(w, "  vibew doctor             Diagnose common issues")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "App runs on port %d, access via sidecar at https://localhost:8443\n", opts.Port)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
