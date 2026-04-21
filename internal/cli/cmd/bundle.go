package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vibewarden/vibewarden/internal/adapters/bundlefs"
	credentialsadapter "github.com/vibewarden/vibewarden/internal/adapters/credentials"
	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
	generateapp "github.com/vibewarden/vibewarden/internal/app/generate"
	"github.com/vibewarden/vibewarden/internal/config"
	configtemplates "github.com/vibewarden/vibewarden/internal/config/templates"
)

// defaultBundleOutputDir is the default target directory for `vibew bundle`.
// It is a peer of .vibewarden/deploy/<env>/ — both roots live under the
// project's .vibewarden/ so neither pollutes the user's source tree.
const defaultBundleOutputDir = ".vibewarden/bundle"

// multiSiteErrorMessage is the user-facing error returned when vibew bundle
// is run against a multi-site project. See ADR-085 §7.
const multiSiteErrorMessage = "multi-site bundle is not yet supported; use `vibew deploy` until this lands (tracking: see ADR-085)"

// NewBundleCmd creates the "vibew bundle" command.
//
// vibew bundle produces a self-contained Docker Compose deployment artifact
// the user can scp to a VPS and start with `docker compose up -d`. The
// command writes files under --output (default .vibewarden/bundle/) and
// never opens an SSH connection, never calls docker on a remote host, and
// never touches files outside the output directory.
//
// Flags:
//   - --output <dir>    output directory (default: .vibewarden/bundle)
//   - --overwrite       overwrite an existing .env inside --output
//   - --image <tag>     docker image tag to package (default: <project>-app:latest)
//   - --skip-image      do not package image.tar (for registry-pull users)
//
// Configuration is loaded via config.LoadStrict so unknown keys in
// vibewarden.yaml or vibewarden.production.yaml abort the command before
// any files are written. This is the #1053 contract.
func NewBundleCmd() *cobra.Command {
	var (
		outputDir string
		overwrite bool
		imageTag  string
		skipImage bool
	)

	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Produce a self-contained Docker Compose deploy bundle",
		Long: `Produce a self-contained Docker Compose deploy bundle under --output.

` + "`vibew bundle`" + ` writes everything a user needs to deploy on a VPS: a
merged docker-compose.yml (with image: pinned, never build:), the merged
vibewarden.yaml, a sample.env scaffold, a preserved-across-runs .env, a
reference deploy.sh script, an optional image.tar produced via docker save,
and a README.md describing the three-step manual deploy.

No SSH connection is opened, no remote docker call is made, and nothing
outside --output is modified. The command is purely local.

Output layout:
  .vibewarden/bundle/
    docker-compose.yml    # image: pinned, never build:
    vibewarden.yaml       # merged base + prod override, strict-validated
    sample.env            # regenerated every run
    .env                  # first-run only; --overwrite to replace
    deploy.sh             # mode 0o750, 10-line reference script
    image.tar             # omitted with --skip-image
    README.md             # 3-paragraph manual-deploy guide
    kratos/, .credentials # anything the generator produces

Examples:
  vibew bundle
  vibew bundle --output build/deploy
  vibew bundle --skip-image
  vibew bundle --image ghcr.io/acme/myapp:v1.2.3
  vibew bundle --overwrite`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBundle(cmd, outputDir, imageTag, overwrite, skipImage)
		},
	}

	cmd.Flags().StringVar(&outputDir, "output", defaultBundleOutputDir, "output directory")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "overwrite an existing .env inside the output directory")
	cmd.Flags().StringVar(&imageTag, "image", "", "docker image tag to package (default: <project>-app:latest)")
	cmd.Flags().BoolVar(&skipImage, "skip-image", false, "do not package image.tar (for users pulling from a registry)")

	return cmd
}

// runBundle executes the "vibew bundle" use case. It is extracted from RunE
// so tests can drive it directly with a fake cobra.Command.
func runBundle(cmd *cobra.Command, outputDir, imageTag string, overwrite, skipImage bool) error {
	if err := requireScaffolding(); err != nil {
		return err
	}

	if outputDir == "" {
		outputDir = defaultBundleOutputDir
	}

	cfg, err := loadAndResolve(cmd.Context(), "")
	if err != nil {
		return err
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
		return fmt.Errorf("validating config: %w", err)
	}

	// Multi-site projects are deferred to a follow-up — see ADR-085.
	// Local detection: presence of a sites/ directory next to the config.
	if isMultiSiteProject(absConfig) {
		return fmt.Errorf("%s", multiSiteErrorMessage)
	}

	projectName := deriveProjectName(cfg, absConfig)
	if imageTag == "" {
		imageTag = cfg.ComposeProjectName() + "-app:latest"
	}

	// Ensure the output directory exists before we start writing into it.
	bfs := bundlefs.New()
	if err := bfs.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	renderer := templateadapter.NewRenderer(configtemplates.FS)
	generator := generateapp.NewServiceWithCredentials(
		renderer,
		credentialsadapter.NewGenerator(),
		credentialsadapter.NewStore(),
	).WithConfigSourcePath(absConfig)

	svc := deployapp.NewService(nil, generator).WithBundleFS(bfs)
	if !skipImage {
		svc = svc.WithImageSaver(opsadapter.NewImageExportAdapter())
	}

	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		absOut = outputDir
	}

	if err := svc.Bundle(cmd.Context(), deployapp.BundleOptions{
		Config:         cfg,
		ConfigPath:     absConfig,
		ProdConfigPath: prodConfigPath,
		ProjectName:    projectName,
		MultiSite:      false,
		OutputDir:      absOut,
		Env:            deployapp.DefaultEnv,
		Overwrite:      overwrite,
		SkipImage:      skipImage,
		ImageTag:       imageTag,
	}); err != nil {
		return fmt.Errorf("creating bundle: %w", err)
	}

	// Print a listing so users (and AI agents) can see exactly what was
	// written.
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Bundle written to %s\n", absOut)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Contents:")
	files, listErr := bundleListing(absOut)
	if listErr != nil {
		return fmt.Errorf("listing bundle contents: %w", listErr)
	}
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", f)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "Next: ./deploy.sh <user@host>  (or scp -r %s/* user@host:~/ && ssh user@host 'docker compose up -d')\n", absOut)
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

// deriveProjectName mirrors the chain used by `vibew deploy`:
//  1. cfg.Name
//  2. cfg.App.Image (strip ":tag" and any registry prefix)
//  3. ProjectNameFromConfig fallback
func deriveProjectName(cfg *config.Config, absConfig string) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	if cfg.App.Image != "" {
		image := cfg.App.Image
		if idx := strings.LastIndex(image, ":"); idx > 0 {
			image = image[:idx]
		}
		if idx := strings.LastIndex(image, "/"); idx >= 0 {
			image = image[idx+1:]
		}
		if image != "" {
			return image
		}
	}
	return deployapp.ProjectNameFromConfig(absConfig)
}

// isMultiSiteProject reports whether configPath sits in a project whose
// local layout implies multi-site bundling. The signal is a non-empty
// sites/ subdirectory next to the base config file. This matches the
// heuristic used by internal/config/sites.LoadSites.
func isMultiSiteProject(configPath string) bool {
	sitesDir := filepath.Join(filepath.Dir(configPath), "sites")
	entries, err := os.ReadDir(sitesDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
	}
	return false
}
