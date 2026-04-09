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
// vibew deploy --config vibewarden.prod.yaml --target ssh://user@host --secrets-from .env.prod
// vibew deploy status --target ssh://user@host
// vibew deploy logs   --target ssh://user@host [--lines 50]
func NewDeployCmd() *cobra.Command {
	var (
		configPath    string
		target        string
		sshKey        string
		secretsFrom   string
		rotateSecrets bool
		unsealKey     string
	)

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy VibeWarden to a remote server over SSH",
		Long: `Deploy the VibeWarden stack to a remote server over SSH.

The command generates runtime configuration, transfers files via rsync, and
starts Docker Compose on the remote host.

When the secrets plugin is enabled (secrets.enabled: true in vibewarden.yaml),
the first deploy also bootstraps OpenBao: initialises, unseals, enables KV v2
and AppRole, creates the vibewarden policy and role, and seeds secrets from
--secrets-from when provided.

On subsequent deploys the unseal key stored in ~/vibewarden/<project>/.openbao-credentials
is used automatically unless you supply --unseal-key explicitly.

Target URL format:
  ssh://user@host
  ssh://user@host:port

The system ssh and rsync binaries are used so your SSH agent and
~/.ssh/config (IdentityFile, ProxyJump, etc.) are honoured automatically.

Remote directory: ~/vibewarden/<project-name>/

Examples:
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy --config vibewarden.prod.yaml --target ssh://deploy@myserver.example.com:2222
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10 --secrets-from .env.prod
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10 --rotate-secrets --secrets-from .env.prod`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireScaffolding(); err != nil {
				return err
			}

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

			var executor *sshadapter.Executor
			if sshKey != "" {
				executor = sshadapter.NewExecutorWithKey(t, sshKey)
			} else {
				executor = sshadapter.NewExecutor(t)
			}
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

			projectName := deployapp.ProjectNameFromConfig(absConfig)
			remoteDir := "~/vibewarden/" + projectName + "/"

			opts := deployapp.RunOptions{
				ConfigPath: absConfig,
				Out:        cmd.OutOrStdout(),
			}

			// Bootstrap OpenBao when the secrets plugin is enabled.
			if cfg.Secrets.Enabled {
				bootstrapper := deployapp.NewOpenBaoBootstrapper(executor)
				result, err := bootstrapper.Bootstrap(cmd.Context(), deployapp.BootstrapOptions{
					SecretsFile:   secretsFrom,
					RotateSecrets: rotateSecrets,
					UnsealKey:     unsealKey,
					RemoteDir:     remoteDir,
					Out:           cmd.OutOrStdout(),
				})
				if err != nil {
					return fmt.Errorf("openbao bootstrap: %w", err)
				}
				if result.UnsealKey != "" {
					fmt.Fprintln(cmd.OutOrStdout())
					fmt.Fprintln(cmd.OutOrStdout(), "=======================================================")
					fmt.Fprintln(cmd.OutOrStdout(), "  IMPORTANT: Save your OpenBao unseal key now!")
					fmt.Fprintln(cmd.OutOrStdout(), "  You will need it to unseal OpenBao after a restart.")
					fmt.Fprintln(cmd.OutOrStdout(), "  This key will NOT be shown again.")
					fmt.Fprintln(cmd.OutOrStdout(), "=======================================================")
					fmt.Fprintf(cmd.OutOrStdout(), "  Unseal Key : %s\n", result.UnsealKey)
					fmt.Fprintf(cmd.OutOrStdout(), "  Root Token : %s\n", result.RootToken)
					fmt.Fprintf(cmd.OutOrStdout(), "  Role ID    : %s\n", result.RoleID)
					fmt.Fprintf(cmd.OutOrStdout(), "  Secret ID  : %s\n", result.SecretID)
					fmt.Fprintln(cmd.OutOrStdout(), "=======================================================")
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}

			return svc.Deploy(cmd.Context(), cfg, opts)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")
	cmd.Flags().StringVar(&secretsFrom, "secrets-from", "", "path to a .env-format file whose KEY=VALUE pairs are seeded into OpenBao")
	cmd.Flags().BoolVar(&rotateSecrets, "rotate-secrets", false, "re-seed secrets from --secrets-from on subsequent deploys")
	cmd.Flags().StringVar(&unsealKey, "unseal-key", "", "OpenBao unseal key (required when redeploying a sealed instance); overrides stored key")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("secrets-from", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"env"}, cobra.ShellCompDirectiveFilterFileExt
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
	var (
		configPath string
		target     string
		sshKey     string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Docker Compose service status on the remote",
		Long: `Show the Docker Compose service status on the remote server.

The --config flag is used to derive the project name, which determines the
remote directory (~/vibewarden/<project-name>/). It must match the value used
when the project was deployed. When omitted the current directory name is used.

Examples:
  vibew deploy status --target ssh://ubuntu@203.0.113.10
  vibew deploy status --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			var executor *sshadapter.Executor
			if sshKey != "" {
				executor = sshadapter.NewExecutorWithKey(t, sshKey)
			} else {
				executor = sshadapter.NewExecutor(t)
			}
			svc := deployapp.NewService(executor, nil, nil)

			absConfig, err := filepath.Abs(configPath)
			if err != nil {
				absConfig = configPath
			}

			return svc.Status(cmd.Context(), deployapp.StatusOptions{
				ConfigPath: absConfig,
				Out:        cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml — used to derive the remote project directory (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}

// newDeployLogsCmd creates the "vibew deploy logs" subcommand.
func newDeployLogsCmd() *cobra.Command {
	var (
		configPath string
		target     string
		sshKey     string
		lines      int
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch Docker Compose logs from the remote",
		Long: `Fetch Docker Compose logs from the remote server.

The --config flag is used to derive the project name, which determines the
remote directory (~/vibewarden/<project-name>/). It must match the value used
when the project was deployed. When omitted the current directory name is used.

Examples:
  vibew deploy logs --target ssh://ubuntu@203.0.113.10
  vibew deploy logs --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy logs --target ssh://ubuntu@203.0.113.10 --lines 100`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			t, err := sshadapter.ParseTarget(target)
			if err != nil {
				return fmt.Errorf("invalid --target: %w", err)
			}

			var executor *sshadapter.Executor
			if sshKey != "" {
				executor = sshadapter.NewExecutorWithKey(t, sshKey)
			} else {
				executor = sshadapter.NewExecutor(t)
			}
			svc := deployapp.NewService(executor, nil, nil)

			absConfig, err := filepath.Abs(configPath)
			if err != nil {
				absConfig = configPath
			}

			return svc.Logs(cmd.Context(), deployapp.LogsOptions{
				ConfigPath: absConfig,
				Lines:      lines,
				Out:        cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml — used to derive the remote project directory (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")
	cmd.Flags().IntVar(&lines, "lines", 50, "number of log lines to fetch (0 = all)")

	if err := cmd.MarkFlagRequired("target"); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag required registration failed:", err)
	}
	if err := cmd.RegisterFlagCompletionFunc("config", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"yaml", "yml"}, cobra.ShellCompDirectiveFilterFileExt
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warning: flag completion registration failed:", err)
	}

	return cmd
}
