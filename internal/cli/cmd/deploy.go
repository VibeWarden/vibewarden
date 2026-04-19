package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
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
		force         bool
		env           string
		dryRun        bool
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

Use --dry-run to generate the deploy bundle and inspect its contents without
transferring anything to the remote host. No SSH, rsync, or Docker commands
are executed.

Examples:
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy --config vibewarden.prod.yaml --target ssh://deploy@myserver.example.com:2222
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10 --secrets-from .env.prod
  vibew deploy --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10 --rotate-secrets --secrets-from .env.prod
  vibew deploy --dry-run --target ssh://ubuntu@203.0.113.10`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireScaffolding(); err != nil {
				return err
			}

			if !dryRun && target == "" {
				return fmt.Errorf("--target is required (e.g. ssh://user@host)")
			}

			// Determine the deployment environment name.
			envName := env
			if envName == "" {
				envName = "production"
			}

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// Resolve the config path to an absolute path. When --config is
			// not provided, default to vibewarden.yaml in the current directory
			// so that filepath.Dir returns the project directory (not ".").
			resolvedConfig := configPath
			if resolvedConfig == "" {
				resolvedConfig = "vibewarden.yaml"
			}
			absConfig, err := filepath.Abs(resolvedConfig)
			if err != nil {
				absConfig = resolvedConfig
			}

			// Determine the production override file path after resolving the
			// base config to an absolute path.
			prodConfigPath := prodConfigPathForEnv(absConfig, envName)

			projectName := cfg.Name
			if projectName == "" {
				projectName = deployapp.ProjectNameFromConfig(absConfig)
			}

			renderer := templateadapter.NewRenderer(configtemplates.FS)
			generator := generateapp.NewServiceWithCredentials(
				renderer,
				credentialsadapter.NewGenerator(),
				credentialsadapter.NewStore(),
			).WithConfigSourcePath(configPath)

			// --dry-run: generate bundle, list contents, and exit.
			if dryRun {
				svc := deployapp.NewService(nil, generator)
				return runDeployDryRun(cmd, svc, cfg, absConfig, prodConfigPath, projectName, envName)
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
			svc := deployapp.NewService(executor, generator).
				WithImageExporter(opsadapter.NewImageExportAdapter())

			remoteDir := "~/vibewarden/" + projectName + "/"

			opts := deployapp.RunOptions{
				ConfigPath:     absConfig,
				ProdConfigPath: prodConfigPath,
				Env:            envName,
				Force:          force,
				Out:            cmd.OutOrStdout(),
			}

			// Check if the production overlay explicitly disables secrets.
			// The typed Config overlay can't distinguish "not set" from "set
			// to false" for booleans, so we read the raw YAML map.
			secretsEnabled := cfg.Secrets.Enabled
			if prodConfigPath != "" {
				if data, readErr := os.ReadFile(prodConfigPath); readErr == nil {
					var m map[string]any
					if yaml.Unmarshal(data, &m) == nil {
						if secrets, ok := m["secrets"].(map[string]any); ok {
							if enabled, ok := secrets["enabled"].(bool); ok {
								secretsEnabled = enabled
							}
						}
					}
				}
			}

			// Bootstrap OpenBao when the secrets plugin is enabled.
			if secretsEnabled {
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

			// Detect deploy mode: fresh install or add-site.
			hasDomain := cfg.TLS.Domain != ""

			mode, err := deployapp.Detect(cmd.Context(), executor)
			if err != nil {
				return fmt.Errorf("detecting deploy mode: %w", err)
			}

			switch mode {
			case deployapp.ModeFreshInstall:
				if hasDomain {
					// Multi-app bootstrap: create sidecar + first site.
					return svc.BootstrapSidecar(cmd.Context(), cfg, opts)
				}
				// Legacy single-app deploy (backward compatible).
				return svc.Deploy(cmd.Context(), cfg, opts)

			case deployapp.ModeAddSite:
				if hasDomain {
					// Add a new site to existing sidecar.
					return svc.DeployMultiApp(cmd.Context(), cfg, opts)
				}
				return fmt.Errorf("cannot add a site without a TLS domain; set tls.domain in %s", absConfig)

			default:
				return fmt.Errorf("unexpected deploy mode: %v", mode)
			}
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")
	cmd.Flags().StringVar(&secretsFrom, "secrets-from", "", "path to a .env-format file whose KEY=VALUE pairs are seeded into OpenBao")
	cmd.Flags().BoolVar(&rotateSecrets, "rotate-secrets", false, "re-seed secrets from --secrets-from on subsequent deploys")
	cmd.Flags().StringVar(&unsealKey, "unseal-key", "", "OpenBao unseal key (required when redeploying a sealed instance); overrides stored key")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite remote files even if they have been modified since last deploy")
	cmd.Flags().StringVar(&env, "env", "production", `deployment environment name; reads vibewarden.<env>.yaml as production override`)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "generate the deploy bundle and list its contents without deploying")

	// --target is validated manually in RunE to allow --dry-run without a target.
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
		app        string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Docker Compose service status on the remote",
		Long: `Show the Docker Compose service status on the remote server.

The --config flag is used to derive the project name, which determines the
remote directory (~/vibewarden/<project-name>/). It must match the value used
when the project was deployed. When omitted the current directory name is used.

In multi-app mode, use --app to target a specific site. Without --app, all
sites and the sidecar status are shown.

Examples:
  vibew deploy status --target ssh://ubuntu@203.0.113.10
  vibew deploy status --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy status --target ssh://ubuntu@203.0.113.10 --app blog`,
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
			svc := deployapp.NewService(executor, nil)

			// Check if remote is multi-app.
			isMulti, err := deployapp.IsMultiApp(cmd.Context(), executor)
			if err != nil {
				return fmt.Errorf("detecting multi-app mode: %w", err)
			}

			if isMulti || app != "" {
				return svc.StatusMultiApp(cmd.Context(), app, cmd.OutOrStdout())
			}

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
	cmd.Flags().StringVar(&app, "app", "", "target a specific site in multi-app mode (e.g. --app blog)")

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
		follow     bool
		app        string
	)

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Fetch Docker Compose logs from the remote",
		Long: `Fetch Docker Compose logs from the remote server.

The --config flag is used to derive the project name, which determines the
remote directory (~/vibewarden/<project-name>/). It must match the value used
when the project was deployed. When omitted the current directory name is used.

In multi-app mode, use --app to target a specific site's logs. Without --app,
the sidecar logs are shown.

Examples:
  vibew deploy logs --target ssh://ubuntu@203.0.113.10
  vibew deploy logs --config vibewarden.prod.yaml --target ssh://ubuntu@203.0.113.10
  vibew deploy logs --target ssh://ubuntu@203.0.113.10 --lines 100
  vibew deploy logs --target ssh://ubuntu@203.0.113.10 --follow
  vibew deploy logs --target ssh://ubuntu@203.0.113.10 --app blog --follow`,
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
			svc := deployapp.NewService(executor, nil)

			// Check if remote is multi-app.
			isMulti, err := deployapp.IsMultiApp(cmd.Context(), executor)
			if err != nil {
				return fmt.Errorf("detecting multi-app mode: %w", err)
			}

			if isMulti || app != "" {
				return svc.LogsMultiApp(cmd.Context(), app, deployapp.LogsOptions{
					Lines:  lines,
					Follow: follow,
					Out:    cmd.OutOrStdout(),
				})
			}

			absConfig, err := filepath.Abs(configPath)
			if err != nil {
				absConfig = configPath
			}

			// Load config to get the project name (cfg.Name).
			logsCfg, loadErr := config.Load(absConfig)
			var projectName string
			if loadErr == nil && logsCfg.Name != "" {
				projectName = logsCfg.Name
			}

			return svc.Logs(cmd.Context(), deployapp.LogsOptions{
				ConfigPath:  absConfig,
				ProjectName: projectName,
				Lines:       lines,
				Follow:      follow,
				Out:         cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to vibewarden.yaml — used to derive the remote project directory (default: ./vibewarden.yaml)")
	cmd.Flags().StringVar(&target, "target", "", "remote target in ssh://user@host[:port] format (required)")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "path to the SSH private key file (default: use SSH agent / ~/.ssh/config)")
	cmd.Flags().IntVar(&lines, "lines", 50, "number of log lines to fetch (0 = all)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream log output continuously until cancelled (Ctrl-C)")
	cmd.Flags().StringVar(&app, "app", "", "target a specific site in multi-app mode (e.g. --app blog)")

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

// runDeployDryRun generates the deploy bundle and lists its contents without
// performing any remote operations. It prints the bundle file listing and
// key configuration values, then returns nil.
func runDeployDryRun(
	cmd *cobra.Command,
	svc *deployapp.Service,
	cfg *config.Config,
	absConfig, prodConfigPath, projectName, envName string,
) error {
	out := cmd.OutOrStdout()

	bundleDir, err := os.MkdirTemp("", "vibewarden-dry-run-*")
	if err != nil {
		return fmt.Errorf("creating temp bundle directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(bundleDir) }()

	fmt.Fprintln(out, "Dry run: generating deploy bundle...")
	if err := svc.Bundle(cmd.Context(), deployapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     absConfig,
		ProdConfigPath: prodConfigPath,
		ProjectName:    projectName,
		OutputDir:      bundleDir,
		Env:            envName,
	}); err != nil {
		return fmt.Errorf("creating deploy bundle: %w", err)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Project:     %s\n", projectName)
	fmt.Fprintf(out, "Environment: %s\n", envName)
	fmt.Fprintf(out, "Remote dir:  ~/vibewarden/%s/\n", projectName)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Bundle contents:")

	var files []string
	err = filepath.Walk(bundleDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(bundleDir, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing bundle contents: %w", err)
	}

	sort.Strings(files)
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", f)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Dry run complete. No files were transferred.")
	return nil
}

// prodConfigPathForEnv returns the path to the environment-specific production
// override file (e.g. vibewarden.production.yaml) based on the base config
// path. When the computed file does not exist, an empty string is returned
// (no override).
func prodConfigPathForEnv(configPath, envName string) string {
	dir := filepath.Dir(configPath)
	prodFile := filepath.Join(dir, "vibewarden."+envName+".yaml")
	if _, err := os.Stat(prodFile); err == nil {
		return prodFile
	}
	return ""
}
