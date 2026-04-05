package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	sshadapter "github.com/vibewarden/vibewarden/internal/adapters/ssh"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// NewDeployCmd creates the "vibew deploy" command and its subcommands.
//
// vibew deploy --config vibewarden.prod.yaml --target ssh://user@host
// vibew deploy status --target ssh://user@host
// vibew deploy logs   --target ssh://user@host [--lines 50]
func NewDeployCmd() *cobra.Command {
	var (
		configPath string
		target     string
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy VibeWarden to a remote server over SSH",
		Long: `Deploy the VibeWarden stack to a remote server over SSH.

The command generates runtime configuration, transfers files via rsync, and
starts Docker Compose on the remote host.

Target URL format:
  ssh://user@host
  ssh://user@host:port

The system ssh and rsync binaries are used so your SSH agent and
~/.ssh/config (IdentityFile, ProxyJump, etc.) are honoured automatically.

Remote directory: ~/vibewarden/<project-name>/

Examples:
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy --config vibewarden.prod.yaml --target ssh://deploy@myserver.example.com:2222`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			executor := sshadapter.NewExecutor(t)
			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			)
			svc := deployapp.NewService(executor, generator, nil)

			absConfig, err := filepath.Abs(configPath)
			if err != nil {
				absConfig = configPath
			}

			return svc.Deploy(cmd.Context(), cfg, deployapp.RunOptions{
				ConfigPath: absConfig,
				Out:        cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	// Add subcommands.
	cmd.AddCommand(newDeployStatusCmd())
	cmd.AddCommand(newDeployLogsCmd())

	return cmd
}

// newDeployStatusCmd creates the "vibew deploy status" subcommand.
func newDeployStatusCmd() *cobra.Command {
	var target string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Docker Compose service status on the remote",
		Long: `Show the Docker Compose service status on the remote server.

Examples:
  vibew deploy status --target ssh://ubuntu@203.0.113.10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			executor := sshadapter.NewExecutor(t)
			svc := deployapp.NewService(executor, nil, nil)

			return svc.Status(cmd.Context(), deployapp.StatusOptions{
				Out: cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}

	return cmd
}

// newDeployLogsCmd creates the "vibew deploy logs" subcommand.
func newDeployLogsCmd() *cobra.Command {
	var (
		target string
		lines  int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch Docker Compose logs from the remote",
		Long: `Fetch Docker Compose logs from the remote server.

Examples:
  vibew deploy logs --target ssh://ubuntu@203.0.113.10
  vibew deploy logs --target ssh://ubuntu@203.0.113.10 --lines 100`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			executor := sshadapter.NewExecutor(t)
			svc := deployapp.NewService(executor, nil, nil)

			return svc.Logs(cmd.Context(), deployapp.LogsOptions{
				Lines: lines,
				Out:   cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().IntVar(&lines, "lines", 50, "number of log lines to fetch (0 = all)")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}

	return cmd
}
