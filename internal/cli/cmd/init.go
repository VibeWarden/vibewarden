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
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// supportedLanguages lists every language the init command accepts.
// Add new entries here as new language templates are introduced.
var supportedLanguages = []string{"go", "kotlin", "typescript"}

// IsTTY reports whether fd is connected to a terminal.
// It calls the os.File.Stat method and checks for ModeCharDevice, which is set
// on UNIX ttys and on Windows console handles.  The function is a package-level
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

// promptLang prompts the user to choose a language from supportedLanguages.
// It re-prompts on invalid input.
func promptLang(w *os.File, r *bufio.Reader) (string, error) {
	for {
		fmt.Fprintf(w, "Language (%s): ", strings.Join(supportedLanguages, "/"))
		line, err := r.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("reading language input: %w", err)
		}
		line = strings.TrimSpace(line)
		for _, l := range supportedLanguages {
			if strings.EqualFold(line, l) {
				return l, nil
			}
		}
		fmt.Fprintf(w, "Unknown language %q. Supported: %s\n", line, strings.Join(supportedLanguages, ", "))
	}
}

// NewInitCmd creates the `vibew init` subcommand.
//
// The command scaffolds a complete new project from language-specific templates.
// In interactive mode (TTY detected and --lang omitted) the user is prompted for
// language, project name, and description. In non-interactive mode (pipe/CI) --lang
// is required.
//
// When a project name is supplied as a positional argument or via --name, a
// subdirectory with that name is created inside the current working directory.
// When neither is given, the current directory name is used and files are written
// into the current directory.
//
// Usage:
//
//	vibew init --lang go myproject
//	vibew init --lang go                (uses current directory name)
//	vibew init --lang go --port 8080 myproject
//	vibew init --lang go --module github.com/org/myproject myproject
//	vibew init --lang go --describe "a task management API" myproject
//	vibew init --name myproject --describe "a task management API"
func NewInitCmd() *cobra.Command {
	var (
		lang     string
		module   string
		port     int
		force    bool
		version  string
		nameFlag string
		describe string
		group    string
	)

	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: "Create a new project with VibeWarden pre-configured",
		Long: `Scaffold a complete new project from language-specific templates.

The command creates a project directory containing:
  - Minimal app source code that compiles and runs immediately
  - vibewarden.yaml (TLS self-signed, rate limiting enabled)
  - .vibewarden-version (pins the vibew version for this project)
  - .claude/agents/ with architect, developer, and reviewer agent files
  - CLAUDE.md with full vibew CLI reference
  - PROJECT.md with project description (when --describe is given)
  - Dockerfile
  - .gitignore

In interactive mode (terminal detected, --lang not set) you will be prompted for
language, project name, and description.  In non-interactive mode (piped/CI) you
must supply --lang.

Examples:
  vibew init --lang go myproject
  vibew init --lang go myproject --module github.com/org/myproject
  vibew init --lang go myproject --port 8080
  vibew init --lang go --describe "a task management API" myproject
  vibew init --lang go --force myproject
  vibew init --name myproject --describe "a task management API"`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := lang == "" && IsTTY(os.Stdin)

			// A single bufio.Reader wraps os.Stdin for the entire interactive
			// session.  Using multiple readers over the same fd loses buffered
			// bytes; create one here and pass it to every prompt helper.
			stdinReader := bufio.NewReader(os.Stdin)

			// Resolve language.
			if lang == "" {
				if !interactive {
					return fmt.Errorf(
						"--lang is required in non-interactive mode\n\nSupported languages:\n  %s\n\nExample:\n  vibew init --lang go myproject",
						strings.Join(supportedLanguages, ", "),
					)
				}
				// Interactive: prompt for language.
				chosen, err := promptLang(os.Stderr, stdinReader)
				if err != nil {
					return fmt.Errorf("prompting for language: %w", err)
				}
				lang = chosen
			}

			language := domainscaffold.Language(lang)
			switch language {
			case domainscaffold.LanguageGo, domainscaffold.LanguageKotlin, domainscaffold.LanguageTypeScript:
				// supported
			default:
				return fmt.Errorf(
					"unsupported language %q\n\nSupported languages:\n  %s\n\nExample:\n  vibew init --lang go myproject",
					lang,
					strings.Join(supportedLanguages, ", "),
				)
			}

			// Determine project name: positional arg > --name flag > interactive prompt > cwd name.
			var projectName string
			parentDir := "."

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

			// Resolve description: --describe flag > interactive prompt > empty.
			if describe == "" && interactive {
				chosen, err := promptString(os.Stderr, stdinReader, "Project description (optional)", "")
				if err != nil {
					return fmt.Errorf("prompting for description: %w", err)
				}
				describe = chosen
			}

			renderer := templateadapter.NewRenderer(templates.FS)
			svc := scaffoldapp.NewInitProjectService(renderer)

			opts := scaffoldapp.InitProjectOptions{
				ProjectName: projectName,
				ModulePath:  module,
				Port:        port,
				Language:    language,
				Force:       force,
				Version:     version,
				Description: describe,
				GroupID:     group,
			}

			if err := svc.InitProject(context.Background(), parentDir, opts); err != nil {
				if errors.Is(err, os.ErrExist) {
					return fmt.Errorf("%w\n\nRun with --force to overwrite existing files.", err) //nolint:revive,staticcheck // user-facing CLI hint: intentional newline and trailing period
				}
				return err
			}

			printInitSuccessMessage(cmd, projectName, opts)
			return nil
		},
	}

	cmd.Flags().StringVar(&lang, "lang", "", `programming language (required in non-interactive mode; supported: "go", "kotlin", "typescript")`)
	cmd.Flags().StringVar(&module, "module", "", "Go module path (default: project name)")
	cmd.Flags().IntVar(&port, "port", 3000, "HTTP port the generated app listens on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&version, "version", "", "VibeWarden version to pin in .vibewarden-version (default: latest)")
	cmd.Flags().StringVar(&nameFlag, "name", "", "project name (alternative to positional argument)")
	cmd.Flags().StringVar(&describe, "describe", "", "one-line description of what the project builds; written to PROJECT.md and injected into agent files")
	cmd.Flags().StringVar(&group, "group", "", "JVM group identifier for Kotlin projects (e.g., com.mycompany); defaults to sanitized project name")

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

	switch opts.Language {
	case domainscaffold.LanguageKotlin:
		fmt.Fprintf(w, "  src/main/kotlin/.../Application.kt  App entry point (Ktor)\n")
		fmt.Fprintf(w, "  build.gradle.kts                     Gradle build file\n")
		fmt.Fprintf(w, "  settings.gradle.kts                  Gradle settings\n")
	case domainscaffold.LanguageTypeScript:
		fmt.Fprintf(w, "  src/index.ts             App entry point (Express)\n")
		fmt.Fprintf(w, "  package.json             Node.js package manifest\n")
		fmt.Fprintf(w, "  tsconfig.json            TypeScript compiler options\n")
	default: // go
		fmt.Fprintf(w, "  cmd/%s/main.go         App entry point\n", projectName)
		modDisplay := opts.ModulePath
		if modDisplay == "" {
			modDisplay = projectName
		}
		fmt.Fprintf(w, "  go.mod                   Go module (path: %s)\n", modDisplay)
	}

	fmt.Fprintf(w, "  vibewarden.yaml          Security sidecar config\n")
	fmt.Fprintf(w, "  CLAUDE.md                Project instructions for AI agents\n")
	if opts.Description != "" {
		fmt.Fprintf(w, "  PROJECT.md               Project description\n")
	}
	fmt.Fprintf(w, "  .claude/agents/          Architect, developer, reviewer agents\n")
	fmt.Fprintf(w, "  Dockerfile               Container build file\n")
	fmt.Fprintf(w, "  .gitignore               Git ignore rules\n")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  cd %s\n", projectName)
	fmt.Fprintln(w, "  vibew dev                Start dev environment (app + sidecar)")
	fmt.Fprintln(w, "  vibew status             Check component health")
	fmt.Fprintln(w, "  vibew doctor             Diagnose common issues")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "App runs on port %d, access via sidecar at https://localhost:8443\n", opts.Port)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
