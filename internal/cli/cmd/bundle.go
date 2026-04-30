package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/adapters/bundlefs"
	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	bundleapp "github.com/vibewarden/vibewarden/internal/app/bundle"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	multisiteapp "github.com/vibewarden/vibewarden/internal/app/multisite"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// defaultBundleOutputDir is the default target directory for `vibew bundle`.
// It is a peer of .vibewarden/deploy/<env>/ — both roots live under the
// project's .vibewarden/ so neither pollutes the user's source tree.
const defaultBundleOutputDir = ".vibewarden/bundle"

// multiSiteErrorMessage is the user-facing error returned when vibew bundle
// is run against a multi-site project. Multi-site bundle is post-v1; the
// N-apps-on-one-VM architecture is tracked at #1169.
const multiSiteErrorMessage = "multi-site bundle is post-v1; see #1169 for the N-apps-on-one-VM architecture work."

// NewBundleCmd creates the "vibew bundle" command.
//
// vibew bundle produces a self-contained Docker Compose deployment artifact
// the user can scp to a VPS and start with `docker compose up -d`. The
// command writes files under --output (default .vibewarden/bundle/) and
// never opens an SSH connection, never calls docker on a remote host, and
// never touches files outside the output directory.
//
// New flags (ADR-089):
//   - --build              build the image first using `vibew build --platform <target>`
//   - --allow-stale        suppress the stale warning; bundle proceeds regardless
//   - --target-platform    target deployment platform (default: linux/amd64)
//
// New flags (#1245):
//   - --print-deploy        substitute --host/--user/--path into the stdout "Next: deploy" block
//   - --host                SSH host for the stdout deploy block (requires --print-deploy)
//   - --user                SSH user for the stdout deploy block (requires --print-deploy)
//   - --path                remote deploy path for the stdout deploy block (requires --print-deploy)
//
// Exit codes:
//   - 0: success (including warnings)
//   - 1: generic / config failure
//   - 2: image missing from local Docker (use --build or run vibew build)
//   - 3: docker daemon unreachable
//
// Configuration is loaded via config.LoadStrict so unknown keys in
// vibewarden.yaml or vibewarden.production.yaml abort the command before
// any files are written. This is the #1053 contract.
func NewBundleCmd() *cobra.Command {
	var (
		outputDir      string
		overwrite      bool
		imageTag       string
		skipImage      bool
		build          bool
		allowStale     bool
		targetPlatform string
		printDeploy    bool
		deployHost     string
		deployUser     string
		deployPath     string
	)

	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Produce a self-contained Docker Compose deploy bundle",
		Long: `Produce a self-contained Docker Compose deploy bundle under --output.

` + "`vibew bundle`" + ` writes everything a user needs to deploy on a VPS: a
merged docker-compose.yml (with image: pinned, never build:), the merged
vibewarden.yaml, a sample.env scaffold, a preserved-across-runs .env, an
optional image.tar produced via docker save, and a README.md describing
the deploy contract (what the bundle is, where to put it, the two
non-obvious traps).

Before writing any files, bundle inspects the target image and prints a
health block showing the tag, digest, architecture, freshness, and any
warnings. The bundle aborts if the image is missing — use --build to build
it automatically, or run ` + "`vibew build`" + ` first.

No SSH connection is opened, no remote docker call is made, and nothing
outside --output is modified. The command is purely local.

Exit codes:
  0  success (warnings do not change the exit code)
  1  generic failure (config invalid, I/O error, etc.)
  2  image missing — build it with --build or vibew build
  3  docker daemon unreachable

Output layout:
  .vibewarden/bundle/
    docker-compose.yml    # image: pinned, never build:
    vibewarden.yaml       # merged base + prod override, strict-validated
    sample.env            # regenerated every run
    .env                  # first-run only; --overwrite to replace
    image.tar             # omitted with --skip-image
    README.md             # deploy contract — see file
    kratos/, .credentials # anything the generator produces

Examples:
  vibew bundle
  vibew bundle --build
  vibew bundle --build --target-platform linux/arm64
  vibew bundle --allow-stale
  vibew bundle --output build/deploy
  vibew bundle --skip-image
  vibew bundle --image ghcr.io/acme/myapp:v1.2.3
  vibew bundle --overwrite
  vibew bundle --print-deploy --host 1.2.3.4 --user root --path /opt/myapp   # ad-hoc deploy block (no config mutation)

Deploy block precedence ("Next: deploy" stdout):
  1. --print-deploy --host <h> --user <u> --path <p>  (ad-hoc; affects stdout only)
  2. deploy.host in vibewarden.production.yaml         (persistent; affects stdout AND bundle README)
  3. bracketed placeholder + hint paragraph            (default; nothing configured)

The bundle README always reflects #2 or #3 — --print-deploy is stdout-only by design,
since the README is a versioned artifact that ships with the bundle.

--print-deploy overrides any deploy.host in vibewarden.production.yaml for the
printed "Next: deploy" block. All three sub-flags (--host, --user, --path) must be
supplied together. Paths with spaces in --path are not supported.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, err := runBundle(cmd, outputDir, imageTag, targetPlatform, overwrite, skipImage, build, allowStale, printDeploy, deployHost, deployUser, deployPath)
			if err != nil {
				// Set the process exit code for semantic exit codes (2, 3) while
				// still surfacing the error message via cobra.
				if code == 2 || code == 3 {
					// cobra prints the error; we need to signal exit code.
					// Use os.Exit after printing to avoid cobra's default exit-1 swallowing
					// our carefully chosen code. For exit code 3 (ErrDockerUnavailable),
					// render the actionable operator hint; otherwise print the raw error.
					if code == 3 {
						renderDockerUnavailable(cmd.ErrOrStderr(), err)
					} else {
						fmt.Fprintln(cmd.ErrOrStderr(), "ERROR:", err)
					}
					os.Exit(code) //nolint:gocritic // intentional: semantic exit code required by ADR-089
				}
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", defaultBundleOutputDir, "output directory")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite an existing .env inside the output directory")
	cmd.Flags().StringVar(&imageTag, "image", "", "docker image tag to package (default: <project>-app:latest)")
	cmd.Flags().BoolVar(&skipImage, "skip-image", false, "do not package image.tar (for users pulling from a registry)")
	cmd.Flags().BoolVar(&build, "build", false, "build the app image before bundling (use when: image is missing or stale)")
	cmd.Flags().BoolVar(&allowStale, "allow-stale", false, "suppress the stale-image warning (bundle proceeds regardless of freshness)")
	cmd.Flags().StringVar(&targetPlatform, "target-platform", "",
		"expected deployment platform, e.g. linux/arm64 (default: deploy.target_platform from vibewarden.production.yaml, or linux/amd64)")
	cmd.Flags().BoolVar(&printDeploy, "print-deploy", false,
		"substitute --host/--user/--path into the printed 'Next: deploy' block (overrides deploy.host from production.yaml)")
	cmd.Flags().StringVar(&deployHost, "host", "", "SSH host to substitute into the printed deploy block (requires --print-deploy)")
	cmd.Flags().StringVar(&deployUser, "user", "", "SSH user to substitute into the printed deploy block (requires --print-deploy)")
	cmd.Flags().StringVar(&deployPath, "path", "", "remote deploy path to substitute into the printed deploy block, e.g. /opt/myapp (requires --print-deploy; paths with spaces are not supported)")

	return cmd
}

// runBundle executes the "vibew bundle" use case. It is extracted from RunE
// so tests can drive it directly with a fake cobra.Command.
//
// Returns (exitCode, error). Exit codes:
//
//	0: success
//	1: generic failure
//	2: image missing (ErrImageMissing)
//	3: docker daemon unreachable (ErrDockerUnavailable)
func runBundle(cmd *cobra.Command, outputDir, imageTag, targetPlatform string, overwrite, skipImage, build, allowStale bool, printDeploy bool, deployHost, deployUser, deployPath string) (int, error) {
	// Validate --print-deploy flag combination FIRST — pure function, no I/O.
	// A user who types --host/--user/--path without --print-deploy anywhere
	// (including outside a vibewarden directory) sees the relevant flag error,
	// not the scaffolding error.
	if err := validatePrintDeployFlags(printDeploy, deployHost, deployUser, deployPath); err != nil {
		return 1, err
	}

	if err := requireScaffolding(); err != nil {
		return 1, err
	}

	if outputDir == "" {
		outputDir = defaultBundleOutputDir
	}

	cfg, err := loadAndResolve(cmd.Context(), "")
	if err != nil {
		return 1, err
	}

	// Resolve the config path to an absolute path so the merge and the
	// sites-dir check both see the same project root.
	absConfig, err := filepath.Abs(defaultConfigName)
	if err != nil {
		absConfig = defaultConfigName
	}
	prodConfigPath := prodConfigPathForEnv(absConfig, "production")

	// Strict schema check — unknown keys in either file abort before we
	// write anything. This is the #1053 regression guard for bundle.
	if _, err := config.LoadStrict(absConfig, prodConfigPath); err != nil {
		var unknown *config.UnknownKeyError
		if errors.As(err, &unknown) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Configuration invalid: %s\n", unknown.Error())
		}
		return 1, fmt.Errorf("validating config: %w", err)
	}

	// Multi-site bundle is post-v1 (#1169). Refuse early with a clear message
	// so users discover the limitation before any files are written.
	if multisiteapp.IsProject(absConfig) {
		return 1, fmt.Errorf("%s", multiSiteErrorMessage)
	}

	projectName := deriveProjectName(cfg, absConfig)
	if projectName == "" {
		return 1, fmt.Errorf("cannot derive project name from configuration path %q", absConfig)
	}
	if imageTag == "" {
		imageTag = projectName + "-app:latest"
	}

	// Resolve platform precedence: CLI flag (if non-empty) → yaml
	// deploy.target_platform → viper default "linux/amd64".
	// cfg.Deploy.TargetPlatform is populated by the viper default when the
	// yaml field is absent entirely; however, when the yaml field is present
	// but set to an empty string (""), viper returns "" (explicit empty
	// overrides the default). In that case CheckImageHealth's own empty-check
	// falls back to defaultTargetPlatform ("linux/amd64").
	resolvedPlatform := targetPlatform
	if resolvedPlatform == "" {
		resolvedPlatform = cfg.Deploy.TargetPlatform
	}

	// --build: run `vibew build --platform <target>` before inspecting or
	// packaging. Failure aborts the bundle (no partial output written yet).
	if build {
		builder := opsadapter.NewBuildAdapter()
		buildSvc := opsapp.NewBuildService(builder).
			WithShellProber(opsadapter.NewShellProberAdapter())
		buildOpts := opsapp.BuildOptions{
			Platform:   resolvedPlatform,
			ConfigPath: absConfig,
			ImageTag:   imageTag,
		}
		if err := buildSvc.Run(cmd.Context(), cfg, buildOpts, cmd.OutOrStdout()); err != nil {
			return 1, fmt.Errorf("building image: %w", err)
		}
	}

	// Ensure the output directory exists before we start writing into it.
	bfs := bundlefs.New()
	if err := bfs.MkdirAll(outputDir, 0o750); err != nil {
		return 1, fmt.Errorf("creating output directory: %w", err)
	}

	renderer := templateadapter.NewRenderer(configtemplates.FS)
	generator := generateapp.NewServiceWithCredentials(
		renderer,
		credentialsadapter.NewGenerator(),
		credentialsadapter.NewStore(),
	).WithConfigSourcePath(absConfig)

	svc := bundleapp.NewService(nil, generator).WithBundleFS(bfs)
	if !skipImage {
		// Wire image inspection and saving only when we will actually package the
		// image. When --skip-image is set the user is pulling from a registry and
		// inspecting a local (possibly absent) image would abort unnecessarily.
		svc = svc.
			WithImageInspector(opsadapter.NewImageInspectAdapter()).
			WithStalenessWalker(bundleapp.NewFileSystemStalenessWalker(filepath.Dir(absConfig))).
			WithImageSaver(opsadapter.NewImageExportAdapter())
	}

	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		absOut = outputDir
	}

	// Production overrides (notably tls.domain for letsencrypt deployments)
	// land in vibewarden.production.yaml, NOT in the base cfg. The bundle's
	// rendered docker-compose / vibewarden.yaml output is correctly merged
	// internally by the service, but the README's fenced deploy block + the
	// "Next: deploy" stdout block both need the resolved domain to print a
	// copy-pasteable healthcheck URL. Read it from production.yaml directly.
	if cfg.TLS.Domain == "" {
		if prodDomain := readProdTLSDomain(prodConfigPath); prodDomain != "" {
			cfg.TLS.Domain = prodDomain
		}
	}

	// Mirror the same pattern for deploy.host. When set in production.yaml,
	// vibew bundle substitutes it verbatim into the stdout block and the
	// bundle README; otherwise, the bracketed placeholder is used with a hint
	// paragraph. No auto-resolve from ~/.ssh/config; explicit is better.
	if cfg.Deploy.Host == "" {
		if prodHost := readProdDeployHost(prodConfigPath); prodHost != "" {
			cfg.Deploy.Host = prodHost
		}
	}
	bundleErr := svc.Bundle(cmd.Context(), bundleapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     absConfig,
		ProdConfigPath: prodConfigPath,
		ProjectName:    projectName,
		MultiSite:      false,
		OutputDir:      absOut,
		Env:            bundleapp.DefaultEnv,
		Overwrite:      overwrite,
		SkipImage:      skipImage,
		ImageTag:       imageTag,
		TargetPlatform: resolvedPlatform,
		AllowStale:     allowStale,
		Out:            cmd.OutOrStdout(),
	})
	if bundleErr != nil {
		// Map sentinel errors to their designated exit codes (ADR-089 §Exit codes).
		if errors.Is(bundleErr, bundleapp.ErrImageMissing) {
			return 2, bundleErr
		}
		if errors.Is(bundleErr, ports.ErrDockerUnavailable) {
			return 3, bundleErr
		}
		if errors.Is(bundleErr, bundleapp.ErrPlatformMismatch) {
			return 1, bundleErr
		}
		return 1, fmt.Errorf("creating bundle: %w", bundleErr)
	}

	// Write content-hash digest after a successful bundle so the next run can
	// use digest comparison instead of mtime (ADR-089 §Refinement, issue #1146).
	svc.WriteInputDigest(filepath.Dir(absConfig))

	// Print a listing so users (and AI agents) can see exactly what was
	// written.
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Bundle written to %s\n", absOut)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Contents:")
	files, listErr := bundleListing(absOut)
	if listErr != nil {
		return 1, fmt.Errorf("listing bundle contents: %w", listErr)
	}
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", f)
	}

	// Post-bundle sensitive-file awareness block (ADR-094). Scans the output
	// directory after writing and prints a stable block when any credential or
	// key files are detected. Empty result → block is omitted entirely.
	sensitive, scanErr := detectSensitiveFiles(absOut)
	if scanErr != nil {
		return 1, fmt.Errorf("scanning bundle for sensitive files: %w", scanErr)
	}
	if len(sensitive) > 0 {
		fmt.Fprintln(out, "")
		renderSensitiveBlock(sensitive, out)
	}

	// Resolve substitution values for the "Next: deploy" block.
	appName := projectName
	if appName == "" {
		appName = "<your-app>"
	}
	domain := "<your-domain>"
	if cfg.TLS.Domain != "" {
		domain = cfg.TLS.Domain
	}

	// Resolve the SSH target and remote path for the "Next: deploy" block.
	// Three-way precedence (#1245):
	//   1. --print-deploy flags (ad-hoc; stdout only — README unaffected)
	//   2. deploy.host from config/production.yaml (persistent)
	//   3. bracketed placeholder + hint paragraph (default)
	const sshPlaceholder = "<your-ssh-user>@<your-ssh-host>"
	sshTarget := sshPlaceholder
	remotePath := "/opt/" + appName // default implicit value — explicit for substitution
	suppressHint := false

	switch {
	case printDeploy:
		sshTarget = deployUser + "@" + deployHost
		remotePath = deployPath
		suppressHint = true
	case cfg.Deploy.Host != "":
		sshTarget = cfg.Deploy.Host
		suppressHint = true
	}

	// Build the docker command for the "Next" block — omit docker load when
	// --skip-image was set so the printed sequence stays valid.
	dockerCmd := "docker load -i image.tar && docker compose up -d"
	if skipImage {
		dockerCmd = "docker compose up -d"
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Next: deploy")
	fmt.Fprintf(out, "    ssh %s 'mkdir -p %s'\n", sshTarget, remotePath)
	fmt.Fprintf(out, "    tar -czf - -C %q . | ssh %s 'tar -xzf - -C %s/'\n", absOut, sshTarget, remotePath)
	fmt.Fprintf(out, "    ssh %s \"cd %s && %s\"\n", sshTarget, remotePath, dockerCmd)
	fmt.Fprintf(out, "    curl -fsSL https://%s/_vibewarden/health\n", domain)
	fmt.Fprintln(out, "")
	// Hint paragraph — emitted only when neither --print-deploy nor deploy.host
	// resolved a real target. When a target is known, the block is self-explanatory.
	if !suppressHint {
		fmt.Fprintf(out, "Replace `%s` with your actual SSH target.\n", sshPlaceholder)
		fmt.Fprintln(out, "  - Check `~/.ssh/config` for an existing alias.")
		fmt.Fprintln(out, "  - Or set `deploy.host: user@host` in `vibewarden.production.yaml`")
		fmt.Fprintln(out, "    (vibew will substitute it into the bundle stdout next time).")
		fmt.Fprintln(out, "")
	}
	fmt.Fprintf(out, "See %s/README.md for context and read-only inspection commands.\n", absOut)
	return 0, nil
}

// validatePrintDeployFlags enforces the all-or-nothing relationship between
// --print-deploy and its three sub-flags. When --print-deploy is set, all
// three of --host, --user, and --path must be non-empty. When --print-deploy
// is unset, none of the three may be set.
func validatePrintDeployFlags(printDeploy bool, host, user, path string) error {
	if !printDeploy {
		if host != "" || user != "" || path != "" {
			return fmt.Errorf("--host/--user/--path require --print-deploy")
		}
		return nil
	}
	var missing []string
	if host == "" {
		missing = append(missing, "--host")
	}
	if user == "" {
		missing = append(missing, "--user")
	}
	if path == "" {
		missing = append(missing, "--path")
	}
	if len(missing) > 0 {
		return fmt.Errorf("--print-deploy requires --host, --user, --path (missing: %s)", strings.Join(missing, ", "))
	}
	return nil
}

// bundleListing walks dir and returns a sorted slice of relative file paths
// (directories are skipped). It is used by runBundle to print a stable
// summary of what was written.
func bundleListing(dir string) ([]string, error) {
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking bundle dir: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// deriveProjectName delegates to the shared bundleapp.DeriveProjectName so
// that vibew bundle and vibew validate use identical name-resolution logic.
// See bundleapp.DeriveProjectName for the full derivation chain (ADR-085 §7).
func deriveProjectName(cfg *config.Config, absConfig string) string {
	return bundleapp.DeriveProjectName(cfg, absConfig)
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

// readProdTLSDomain reads tls.domain from a production override YAML without
// merging the rest of the file. config.Load+LoadStrict together do not merge
// production values into *Config (LoadStrict only schema-checks), so this
// targeted read is the smallest path to making the bundle README and stdout
// substitute the real domain when it lives in vibewarden.production.yaml.
// Returns "" when the file does not exist, is unreadable, or has no domain.
func readProdTLSDomain(prodConfigPath string) string {
	if prodConfigPath == "" {
		return ""
	}
	data, err := os.ReadFile(prodConfigPath) //nolint:gosec // path is the resolved production config path
	if err != nil {
		return ""
	}
	var tree struct {
		TLS struct {
			Domain string `yaml:"domain"`
		} `yaml:"tls"`
	}
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return ""
	}
	return tree.TLS.Domain
}

// readProdDeployHost reads deploy.host from a production override YAML without
// merging the rest of the file. This mirrors readProdTLSDomain — both helpers
// are intentionally near-identical rather than generalised into a single
// config-driven function (YAGNI for two fields; if a third lands the refactor
// is one commit per the architect's decision in #1244).
// Returns "" when the file does not exist, is unreadable, or has no host.
func readProdDeployHost(prodConfigPath string) string {
	if prodConfigPath == "" {
		return ""
	}
	data, err := os.ReadFile(prodConfigPath) //nolint:gosec // path is the resolved production config path
	if err != nil {
		return ""
	}
	var tree struct {
		Deploy struct {
			Host string `yaml:"host"`
		} `yaml:"deploy"`
	}
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return ""
	}
	return tree.Deploy.Host
}
