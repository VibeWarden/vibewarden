package cmd

import (
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

// NewInitCmd creates the `vibewarden init` subcommand.
//
// The command scaffolds a complete new project from language-specific templates.
// It requires --lang to be specified (currently only "go" is supported).
// When a project name is supplied as a positional argument, a subdirectory with
// that name is created in the current working directory.  When no argument is
// given, the current directory name is used as the project name and files are
// written to the current working directory.
//
// Usage:
//
//	vibewarden init --lang go myproject
//	vibewarden init --lang go              (uses current directory name)
//	vibewarden init --lang go --port 8080 myproject
//	vibewarden init --lang go --module github.com/org/myproject myproject
func NewInitCmd() *cobra.Command {
	var (
		lang    string
		module  string
		port    int
		force   bool
		version string
	)

	cmd := &cobra.Command{
		Use:   "init [project-name]",
		Short: "Create a new project with VibeWarden pre-configured",
		Long: `Scaffold a complete new project from language-specific templates.

The command creates a project directory containing:
  - Minimal app source code that compiles and runs immediately
  - vibewarden.yaml (TLS self-signed, rate limiting enabled)
  - vibew wrapper scripts (vibew, vibew.ps1, vibew.cmd)
  - .claude/agents/ with architect, developer, and reviewer agent files
  - CLAUDE.md with full vibew CLI reference
  - Dockerfile
  - .gitignore

Examples:
  vibewarden init --lang go myproject
  vibewarden init --lang go myproject --module github.com/org/myproject
  vibewarden init --lang go myproject --port 8080
  vibewarden init --lang go --force myproject`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if lang == "" {
				return fmt.Errorf(
					"--lang is required\n\nSupported languages:\n  %s\n\nExample:\n  vibewarden init --lang go myproject",
					strings.Join(supportedLanguages, ", "),
				)
			}

			language := domainscaffold.Language(lang)
			switch language {
			case domainscaffold.LanguageGo, domainscaffold.LanguageKotlin, domainscaffold.LanguageTypeScript:
				// supported
			default:
				return fmt.Errorf(
					"unsupported language %q\n\nSupported languages:\n  %s\n\nExample:\n  vibewarden init --lang go myproject",
					lang,
					strings.Join(supportedLanguages, ", "),
				)
			}

			// Determine project name and parent directory.
			var projectName string
			parentDir := "."

			if len(args) > 0 {
				projectName = args[0]
			} else {
				// Use the name of the current directory.
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("getting current directory: %w", err)
				}
				projectName = filepath.Base(cwd)
				// Write into the current directory rather than creating a subdir.
				parentDir = filepath.Dir(cwd)
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

	cmd.Flags().StringVar(&lang, "lang", "", `programming language (required; supported: "go", "kotlin", "typescript")`)
	cmd.Flags().StringVar(&module, "module", "", "Go module path (default: project name)")
	cmd.Flags().IntVar(&port, "port", 3000, "HTTP port the generated app listens on")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&version, "version", "", "VibeWarden version to pin in .vibewarden-version (default: latest)")

	return cmd
}

// printInitSuccessMessage writes next-steps guidance to cmd's output writer.
func printInitSuccessMessage(cmd *cobra.Command, projectName string, opts scaffoldapp.InitProjectOptions) {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Project %q created!\n", projectName)
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
	fmt.Fprintf(w, "  .claude/agents/          Architect, developer, reviewer agents\n")
	fmt.Fprintf(w, "  vibew                    Wrapper script (macOS/Linux)\n")
	fmt.Fprintf(w, "  vibew.ps1                Wrapper script (Windows)\n")
	fmt.Fprintf(w, "  Dockerfile               Container build file\n")
	fmt.Fprintf(w, "  .gitignore               Git ignore rules\n")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Next steps:")
	fmt.Fprintf(w, "  cd %s\n", projectName)
	fmt.Fprintln(w, "  ./vibew dev              Start dev environment (app + sidecar)")
	fmt.Fprintln(w, "  ./vibew status           Check component health")
	fmt.Fprintln(w, "  ./vibew doctor           Diagnose common issues")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "App runs on port %d, access via sidecar at https://localhost:8443\n", opts.Port)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Documentation: https://vibewarden.dev/docs/quickstart")
}
