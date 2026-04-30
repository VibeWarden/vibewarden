package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	scaffoldadapter "github.com/vibewarden/vibewarden/internal/adapters/scaffold"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// NewDevCmd creates the "vibew dev" subcommand.
//
// The command generates runtime config files under .vibewarden/generated/,
// then starts the Docker Compose dev environment in detached mode and
// prints the running service URLs.  Pass --watch to watch vibewarden.yaml
// for changes and auto-regenerate + restart the stack.
// To also start the Prometheus + Grafana observability stack run:
//
//	vibew obs up
func NewDevCmd() *cobra.Command {
	var (
		watch          bool
		configPath     string
		verbose        bool
		rebuild        bool
		rebuildVolumes bool
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

To also start Prometheus and Grafana, run 'vibew obs up' after the stack is up.
Pass --watch to watch vibewarden.yaml for changes and automatically
regenerate config files and restart the stack (blocks until Ctrl+C).

Pass --rebuild to stop the stack, remove the app image, rebuild via vibew build,
and start the stack again. This is the recovery path for the image-identity
mismatch error introduced in v0.18.3. --rebuild and --watch are mutually exclusive.
Pass --rebuild --volumes to also remove named volumes (Postgres data, LE certs, etc.).

Examples:
  vibew dev
  vibew dev --watch
  vibew dev --config ./my-vibewarden.yaml
  vibew dev --rebuild
  vibew dev --rebuild --volumes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Flag-conflict validation before doing any I/O.
			if rebuild && watch {
				return errors.New("--rebuild cannot be combined with --watch")
			}
			if rebuildVolumes && !rebuild {
				return errors.New("--volumes requires --rebuild")
			}

			if err := requireScaffolding(); err != nil {
				return err
			}
			if err := requireConfig(configPath); err != nil {
				return err
			}

			cfg, err := loadAndResolve(cmd.Context(), configPath)
			if err != nil {
				return err
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

			// Wire the image inspector so that `vibew dev` blocks when the app
			// image was built from a different project (ADR-100).
			svc = svc.WithImageInspector(opsadapter.NewImageInspectAdapter())

			// Wire the image remover for the --rebuild path.
			svc = svc.WithImageRemover(opsadapter.NewImageRemoveAdapter())

			// Detect the project language to provide language-specific build
			// instructions when the image is missing.
			detectedLang := detectProjectLang(".")

			opts := opsapp.DevOptions{
				Watch:          watch,
				ConfigPath:     configPath,
				DetectedLang:   detectedLang,
				Verbose:        verbose,
				Rebuild:        rebuild,
				RebuildVolumes: rebuildVolumes,
			}

			var runErr error
			if rebuild {
				// --rebuild path: stop → rmi → build → start.
				// Pass the DockerBuilder port directly — Rebuild stamps identity
				// labels via BuildLabels internally, so BuildService is not needed.
				runErr = svc.Rebuild(cmd.Context(), cfg, opts, opsadapter.NewBuildAdapter(), cmd.OutOrStdout())
			} else {
				runErr = svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout())
			}

			if runErr != nil && errors.Is(runErr, ports.ErrDockerUnavailable) {
				renderDockerUnavailable(cmd.ErrOrStderr(), runErr)
				os.Exit(3) //nolint:gocritic // intentional: semantic exit code 3 for docker unavailable
			}
			return runErr
		},
	}

	cmd.Flags().BoolVar(&watch, "watch", false, "watch vibewarden.yaml for changes and auto-regenerate + restart")
	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "stream docker compose stderr during successful startup (always streamed on failure)")
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "stop stack, remove app image, rebuild via vibew build, and start (recovery for image-identity mismatch)")
	cmd.Flags().BoolVar(&rebuildVolumes, "volumes", false, "remove named volumes during --rebuild (requires --rebuild; also removes Postgres data and LE certs)")

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
