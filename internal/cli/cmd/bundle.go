package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

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
  vibew bundle --overwrite`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code, err := runBundle(cmd, outputDir, imageTag, targetPlatform, overwrite, skipImage, build, allowStale)
			if err != nil {
				// Set the process exit code for semantic exit codes (2, 3) while
				// still surfacing the error message via cobra.
				if code == 2 || code == 3 {
					// cobra prints the error; we need to signal exit code.
					// Use os.Exit after printing to avoid cobra's default exit-1 swallowing
					// our carefully chosen code. We print the error ourselves first.
					fmt.Fprintln(cmd.ErrOrStderr(), "ERROR:", err)
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
	cmd.Flags().StringVar(&targetPlatform, "target-platform", "linux/amd64", "expected deployment platform, e.g. linux/arm64 (use when: your VPS differs from your laptop arch)")

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
func runBundle(cmd *cobra.Command, outputDir, imageTag, targetPlatform string, overwrite, skipImage, build, allowStale bool) (int, error) {
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
	if targetPlatform == "" {
		targetPlatform = "linux/amd64"
	}

	// --build: run `vibew build --platform <target>` before inspecting or
	// packaging. Failure aborts the bundle (no partial output written yet).
	if build {
		builder := opsadapter.NewBuildAdapter()
		buildSvc := opsapp.NewBuildService(builder).
			WithShellProber(opsadapter.NewShellProberAdapter())
		buildOpts := opsapp.BuildOptions{
			Platform:   targetPlatform,
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
		TargetPlatform: targetPlatform,
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

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Next: see %s/README.md for the deploy contract.\n", absOut)
	return 0, nil
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
