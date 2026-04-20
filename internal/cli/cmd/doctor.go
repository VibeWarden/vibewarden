package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	sshadapter "github.com/vibewarden/vibewarden/internal/adapters/ssh"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// NewDoctorCmd creates the "vibew doctor" subcommand.
//
// The command runs a series of independent diagnostics and reports problems.
// It exits with status 1 when any check fails so it can be used in scripts.
func NewDoctorCmd() *cobra.Command {
	var (
		configPath string
		jsonOutput bool
		target     string
		sshKey     string
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose common configuration and environment issues",
		Long: `Run a series of independent diagnostics and report any issues found.

Checks are organised into three layers:

  Config & Docker (always runs):
    - vibewarden.yaml is present and parses without errors
    - Docker daemon is reachable (docker info)
    - Docker Compose v2+ is available (docker compose version)
    - Required ports are available (proxy port)
    - Generated files are present (.vibewarden/generated/docker-compose.yml)
    - If the stack is running: containers are healthy (docker compose ps)
    - ACME email configured when using ZeroSSL
    - Expected app image exists locally (image tag consistency)

  Local Runtime (always runs):
    - Upstream application is reachable (HTTP GET)
    - TLS certificate is valid (if self-signed)

  Production (requires --target):
    - SSH connectivity to the target host
    - Remote container health
    - Domain DNS resolves to target IP
    - Remote TLS certificate expiry
    - Architecture compatibility (local build arch vs remote server arch)

Each check runs independently — a failure does not stop subsequent checks.
Exit code is 1 when any check fails.

Examples:
  vibew doctor
  vibew doctor --config ./my-vibewarden.yaml
  vibew doctor --json
  vibew doctor --target ssh://ubuntu@203.0.113.10
  vibew doctor --target ssh://ubuntu@myserver.example.com --ssh-key ~/.ssh/id_ed25519`,
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
			svc := opsapp.NewDoctorService(compose, portChecker, healthChecker).
				WithImageChecker(opsadapter.NewImageCheckerAdapter())

			// When --target is provided, create an SSH executor for production checks.
			if target != "" {
				t, parseErr := sshadapter.ParseTarget(target)
				if parseErr != nil {
					return fmt.Errorf("invalid --target: %w", parseErr)
				}
				var executor *sshadapter.Executor
				if sshKey != "" {
					executor = sshadapter.NewExecutorWithKey(t, sshKey)
				} else {
					executor = sshadapter.NewExecutor(t)
				}
				svc = svc.WithRemoteExecutor(executor)
			}

			label := configPath
			if label == "" {
				label = "vibewarden.yaml"
			}

			opts := opsapp.DoctorOptions{
				ConfigPath: label,
				WorkDir:    workDir,
				JSON:       jsonOutput,
				Target:     target,
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
	cmd.Flags().StringVar(&target, "target", "", "SSH target for production checks (e.g. ssh://user@host)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to SSH private key (default: use SSH agent)")

	return cmd
}
