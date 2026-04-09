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

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	scaffoldapp "github.com/vibewarden/vibewarden/internal/app/scaffold"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
)

// IsTTY reports whether fd is connected to a terminal.
// It calls the os.File.Stat method and checks for ModeCharDevice, which is set
// on UNIX ttys and on Windows console handles. The function is a package-level
// variable so that tests can replace it without build-tag gymnastics.
var IsTTY = func(fd *os.File) bool {
	info, err := fd.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
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
// The command scaffolds a new project with VibeWarden pre-configured.
// In interactive mode (TTY detected) the user is prompted for project name
// and description. In non-interactive mode a project name is required via
// positional argument or --name.
//
// When a project name is supplied as a positional argument or via --name, a
// subdirectory with that name is created inside the current working directory.
// The special name "." scaffolds into the current working directory itself,
// deriving the project name from the current directory's base name.
// When neither is given, the current directory name is used.
//
// Usage:
//
//	vibew init myproject
//	vibew init .                   (scaffold in current directory)
//	vibew init                     (uses current directory name)
//	vibew init --port 8080 myproject
//	vibew init --describe "a task management API" myproject
//	vibew init --name myproject --describe "a task management API"
//	vibew init --name .            (scaffold in current directory)
func NewInitCmd() *cobra.Command {
	var (
		port     int
		force    bool
		version  string
		nameFlag string
		describe string
	)

	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: "Create a new project with VibeWarden pre-configured",
		Long: `Scaffold a new project with VibeWarden security pre-configured.

The command creates a project directory containing:
  - vibewarden.yaml (TLS self-signed, rate limiting enabled)
  - .vibewarden-version (pins the vibew version for this project)
  - AGENTS-VIBEWARDEN.md with all agent instructions (auto-generated, vibew-owned)
  - AGENTS.md with a reference to AGENTS-VIBEWARDEN.md (user-owned)
  - PROJECT.md with project description (when --describe is given)
  - Dockerfile (generic placeholder with examples for common stacks)
  - .gitignore

In interactive mode (terminal detected) you will be prompted for project name
and description. In non-interactive mode (piped/CI) a project name is required.

Use "." as the project name to scaffold into the current working directory.
The project name is derived from the current directory's base name.

Examples:
  vibew init myproject
  vibew init myproject --port 8080
  vibew init --describe "a task management API" myproject
  vibew init --force myproject
  vibew init --name myproject --describe "a task management API"
  vibew init .`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := IsTTY(os.Stdin)

			// A single bufio.Reader wraps os.Stdin for the entire interactive
			// session. Using multiple readers over the same fd loses buffered
			// bytes; create one here and pass it to every prompt helper.
			stdinReader := bufio.NewReader(os.Stdin)

			// Determine project name: positional arg > --name flag > interactive prompt > cwd name.
			var projectName string
			parentDir := "."
			// inCurrentDir tracks whether we are scaffolding into the existing cwd
			// rather than creating a new subdirectory. Used to suppress the "cd"
			// step in the success message.
			inCurrentDir := false

			if len(args) > 0 {
				projectName = args[0]
			} else if nameFlag != "" {
				projectName = nameFlag
			} else if interactive {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				defaultName := filepath.Base(cwd)
				chosen, err := promptString(os.Stderr, stdinReader, "Project name", defaultName)
				if err != nil {
					return fmt.Errorf("prompting for project name: %w", err)
				}
				projectName = chosen
			} else {
				// Non-interactive, no positional arg, no --name: use cwd name.
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				projectName = filepath.Base(cwd)
				parentDir = filepath.Dir(cwd)
			}

			// When the user passes "." as the project name (via positional arg or
			// --name), scaffold into the current working directory and derive the
			// project name from the directory's base name.
			if projectName == "." {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				projectName = filepath.Base(cwd)
				parentDir = filepath.Dir(cwd)
				inCurrentDir = true
			}

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
			}

			if err := svc.InitProject(context.Background(), parentDir, opts); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
				}
				return err
			}

			printInitSuccessMessage(cmd, projectName, opts, inCurrentDir)
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 3000, "HTTP port the app listens on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&version, "version", "", "VibeWarden version to pin in .vibewarden-version (default: latest)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "project name (alternative to positional argument)")
	cmd.Flags().StringVar(&describe, "describe", "", "one-line description of what the project builds; written to PROJECT.md and injected into agent files")

	return cmd
}

// printInitSuccessMessage writes next-steps guidance to cmd's output writer.
// inCurrentDir indicates that files were scaffolded into the working directory
// rather than a new subdirectory; when true the "cd <project>" step is omitted.
func printInitSuccessMessage(cmd *cobra.Command, projectName string, opts scaffoldapp.InitProjectOptions, inCurrentDir bool) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Project %q created!\n", projectName)
	if opts.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", opts.Description)
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Files created:")

	fmt.Fprintf(w, "  vibewarden.yaml          Security sidecar config\n")
	if opts.Description != "" {
		fmt.Fprintf(w, "  PROJECT.md               Project description\n")
	}
	fmt.Fprintf(w, "  AGENTS-VIBEWARDEN.md     Agent instructions (vibew-owned, auto-generated)\n")
	fmt.Fprintf(w, "  AGENTS.md                Agent instructions entry point (user-owned)\n")
	fmt.Fprintf(w, "  Dockerfile               Container build file (generic placeholder)\n")
	fmt.Fprintf(w, "  .gitignore               Git ignore rules\n")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	if !inCurrentDir {
		fmt.Fprintf(w, "  cd %s\n", projectName)
	}
	fmt.Fprintln(w, "  vibew dev                Start dev environment (app + sidecar)")
	fmt.Fprintln(w, "  vibew status             Check component health")
	fmt.Fprintln(w, "  vibew doctor             Diagnose common issues")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "App runs on port %d, access via sidecar at https://localhost:8443\n", opts.Port)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
