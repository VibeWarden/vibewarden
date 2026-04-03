package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	scaffoldadapter "github.com/vibewarden/vibewarden/internal/adapters/scaffold"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// NewDevCmd creates the "vibew dev" subcommand.
//
// The command generates runtime config files under .vibewarden/generated/,
// then starts the Docker Compose dev environment in detached mode and
// prints the running service URLs.  Pass --observability to also start the
// Prometheus + Grafana observability stack.  Pass --watch to watch
// vibewarden.yaml for changes and auto-regenerate + restart the stack.
func NewDevCmd() *cobra.Command {
	var (
		observability bool
		watch         bool
		configPath    string
	)

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Start the local dev environment",
		Long: `Start the VibeWarden Docker Compose dev environment in detached mode.

When vibewarden.yaml is present, VibeWarden generates runtime configuration
files under .vibewarden/generated/ before starting the stack.

The baseline stack includes:
  - VibeWarden proxy (port 8443, HTTPS with self-signed certificate)
  - Ory Kratos identity server (ports 4433, 4434)
  - PostgreSQL
  - Mailslurper (email sink)

Pass --observability to also start Prometheus and Grafana.
Pass --watch to watch vibewarden.yaml for changes and automatically
regenerate config files and restart the stack (blocks until Ctrl+C).

Examples:
  vibew dev
  vibew dev --observability
  vibew dev --watch
  vibew dev --config ./my-vibewarden.yaml`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			compose := opsadapter.NewComposeAdapter()
			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			)

			var svc *opsapp.DevService
			if watch {
				watcher := opsadapter.NewFsnotifyWatcher()
				svc = opsapp.NewDevServiceWithWatcher(compose, generator, watcher)
			} else {
				svc = opsapp.NewDevServiceWithGenerator(compose, generator)
			}

			// Wire the image checker so that `vibew dev` fails early with a
			// helpful message when the app image has not been built yet.
			svc = svc.WithImageChecker(opsadapter.NewImageCheckerAdapter())

			// Detect the project language to provide language-specific build
			// instructions when the image is missing.
			detectedLang := detectProjectLang(".")

			opts := opsapp.DevOptions{
				Observability: observability,
				Watch:         watch,
				ConfigPath:    configPath,
				DetectedLang:  detectedLang,
			}

			return svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&observability, "observability", false, "start Prometheus and Grafana alongside the core stack")
	cmd.Flags().BoolVar(&watch, "watch", false, "watch vibewarden.yaml for changes and auto-regenerate + restart")
	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		// registration can only fail when called on a non-existent flag; safe to ignore
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}

// detectProjectLang uses the scaffold Detector to infer the project language
// from well-known indicator files in dir. Returns the language string expected
// by DevOptions.DetectedLang ("go", "kotlin", "typescript", or "").
func detectProjectLang(dir string) string {
	d := scaffoldadapter.NewDetector()
	proj, err := d.Detect(dir)
	if err != nil {
		return ""
	}
	switch proj.Type {
	case scaffold.ProjectTypeGo:
		return "go"
	case scaffold.ProjectTypeNode:
		// The scaffold detector uses "node" for all JS/TS projects; map to
		// "typescript" when a tsconfig.json is present, otherwise "node".
		if fileExistsAt(dir, "tsconfig.json") {
			return "typescript"
		}
		return "node"
	default:
		// Kotlin is not currently detected by the scaffold Detector; fall back
		// to file-based heuristic.
		if fileExistsAt(dir, "build.gradle.kts") || fileExistsAt(dir, "build.gradle") {
			return "kotlin"
		}
		return ""
	}
}

// fileExistsAt returns true when the named file exists inside dir.
func fileExistsAt(dir, name string) bool {
	info, err := os.Stat(fmt.Sprintf("%s/%s", dir, name))
	return err == nil && !info.IsDir()
}
