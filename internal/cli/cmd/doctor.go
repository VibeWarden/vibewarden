package cmd

import (
	"errors"
	"fmt"
	"net/http"
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
	var configPath string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common configuration and environment issues",
		Long: `Run a series of independent diagnostics and report any issues found.

Checks performed:
  - Is Docker running?
  - Is Docker Compose (v2) available?
  - Is vibewarden.yaml present and valid?
  - Are required ports available?
  - Can VibeWarden reach the upstream app?

Each check runs independently — a failure does not stop subsequent checks.
Exit code is 1 when any check fails.

Examples:
  vibewarden doctor
  vibewarden doctor --config ./my-vibewarden.yaml`,
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

			compose := opsadapter.NewComposeAdapter()
			portChecker := opsadapter.NewNetPortChecker()
			httpClient := &http.Client{Timeout: 5 * time.Second}
			healthChecker := opsadapter.NewHTTPHealthChecker(httpClient)
			svc := opsapp.NewDoctorService(compose, portChecker, healthChecker)

			label := configPath
			if label == "" {
				label = "vibewarden.yaml"
			}

			allOK, err := svc.Run(cmd.Context(), cfg, label, cmd.OutOrStdout())
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

	return cmd
}
