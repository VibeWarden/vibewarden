package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewDoctorCmd creates the "vibewarden doctor" subcommand.
//
// The command runs a series of independent diagnostics and reports problems.
// It exits with status 1 when any check fails so it can be used in scripts.
func NewDoctorCmd() *cobra.Command {
	var (
		configPath string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common configuration and environment issues",
		Long: `Run a series of independent diagnostics and report any issues found.

Checks performed (in order):
  - vibewarden.yaml is present and parses without errors
  - Docker daemon is reachable (docker info)
  - Docker Compose v2+ is available (docker compose version)
  - Required ports are available (proxy port)
  - Generated files are present (.vibewarden/generated/docker-compose.yml)
  - If the stack is running: containers are healthy (docker compose ps)

Each check runs independently — a failure does not stop subsequent checks.
Exit code is 1 when any check fails.

Examples:
  vibewarden doctor
  vibewarden doctor --config ./my-vibewarden.yaml
  vibewarden doctor --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Load config — pass nil-safe; doctor will report missing config.
			cfg, loadErr := config.Load(configPath)
			if loadErr != nil {
				// Report but don't abort — doctor can still run Docker checks.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load config: %v\n", loadErr)
				cfg = nil
			}

			// If cfg is nil we use zero-value defaults so the service doesn't panic.
			if cfg == nil {
				cfg = &config.Config{}
			}

			workDir, err := os.Getwd()
			if err != nil {
				workDir = "."
			}

			compose := opsadapter.NewComposeAdapter()
			portChecker := opsadapter.NewNetPortChecker()
			httpClient := &http.Client{Timeout: 5 * time.Second}
			healthChecker := opsadapter.NewHTTPHealthChecker(httpClient)
			svc := opsapp.NewDoctorService(compose, portChecker, healthChecker)

			label := configPath
			if label == "" {
				label = "vibewarden.yaml"
			}

			opts := opsapp.DoctorOptions{
				ConfigPath: label,
				WorkDir:    workDir,
				JSON:       jsonOutput,
			}

			allOK, err := svc.Run(cmd.Context(), cfg, opts, cmd.OutOrStdout())
			if err != nil {
				return err
			}

			if !allOK {
				return errors.New("one or more checks failed")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output results as JSON")

	return cmd
}
