package bundle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/templates"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// BundleOptions holds parameters for producing the deploy bundle.
//
// The name is retained across the deploy → bundle package rename (ADR-086)
// so downstream callers access it as `bundleapp.BundleOptions`, which does
// not stutter at the call site.
type BundleOptions struct { //nolint:revive // kept stable across package rename; see ADR-086
	// Config is the user's loaded configuration (merged result of base +
	// production override, loaded via config.Load). It is used for template
	// rendering and generator input where a typed Config is needed.
	Config *config.Config

	// ConfigPath is the path to the base vibewarden.yaml on disk. The raw YAML
	// is read from this file for map-based merging and marshalling to preserve
	// original field names.
	ConfigPath string

	// ProdConfigPath is the optional path to the production override file
	// (e.g. vibewarden.production.yaml). When set, its values are deep-merged
	// on top of the base config before writing to the bundle.
	ProdConfigPath string

	// ProjectName is the DNS-safe project name used in directory and container
	// naming.
	ProjectName string

	// MultiSite indicates whether the deploy targets a multi-site layout.
	MultiSite bool

	// OutputDir is the directory where bundle files are written. Defaults to
	// ".vibewarden/deploy/<env>" when empty.
	OutputDir string

	// Env is the deployment environment name (e.g. "production", "staging").
	// Defaults to "production" when empty. The output directory includes the
	// environment name: .vibewarden/deploy/<env>/.
	Env string

	// Overwrite, when true, replaces an existing .env in the output
	// directory. When false (the default), an existing .env is preserved so
	// user edits survive re-running vibew bundle. This flag is honoured only
	// by the extras pipeline (bundle_extras.go) — it has no effect on the
	// deterministic files (docker-compose.yml, vibewarden.yaml, .credentials).
	Overwrite bool

	// SkipImage, when true, omits image.tar from the bundle. This is the
	// recommended mode for users who push their image via a container
	// registry rather than docker save / docker load.
	SkipImage bool

	// ImageTag is the Docker image reference passed to docker save when
	// producing image.tar. When empty, the bundle service defers to
	// cfg.ComposeProjectName()+"-app:latest". Exported so the CLI layer can
	// override the default without the service inventing names.
	ImageTag string

	// TargetPlatform is the expected deployment platform (e.g. "linux/amd64").
	// Used by the image health check. When empty, "linux/amd64" is assumed.
	TargetPlatform string

	// AllowStale, when true, suppresses the STALE warning in the image health
	// block. The bundle always proceeds regardless of freshness.
	AllowStale bool

	// ProjectRoot is the directory walked by the staleness walker. When empty,
	// the directory containing ConfigPath is used.
	ProjectRoot string

	// Out is the writer for the image health block output. When nil, os.Stdout
	// is implied by the CLI layer — the service itself does not write to stdout
	// directly so callers control where the block goes.
	Out interface {
		Write(p []byte) (n int, err error)
	}
}

// defaultBundleDir is the default output directory for deploy bundles.
const defaultBundleDir = ".vibewarden/deploy"

// DefaultEnv is the default deployment environment name used when BundleOptions.Env
// is empty. The output directory becomes .vibewarden/deploy/production/.
const DefaultEnv = "production"

// Bundle produces a complete deploy bundle under opts.OutputDir (or
// .vibewarden/deploy/<env>/ by default).
//
// For single-site mode it generates docker-compose.yml, vibewarden.yaml,
// credentials, and supporting files into the output directory.
//
// For multi-site mode it generates the .sidecar/ and/or
// sites/<project>/ subdirectories.
//
// All values are fully resolved -- no sed or runtime patching needed.
func (s *Service) Bundle(ctx context.Context, opts BundleOptions) error {
	outDir := opts.OutputDir
	if outDir == "" {
		env := opts.Env
		if env == "" {
			env = DefaultEnv
		}
		outDir = filepath.Join(defaultBundleDir, env)
	}

	if opts.MultiSite {
		return s.bundleMultiSiteSite(ctx, opts.Config, opts.ConfigPath, opts.ProdConfigPath, opts.ProjectName, outDir)
	}

	// Step 0: Image health check — runs before any files are written so that
	// a missing/stale/mismatched image is caught before producing a partial
	// bundle artifact. When no inspector is wired (existing tests that predate
	// ADR-089), the health block is skipped with a visible notice.
	if s.imageInspector != nil {
		if err := s.runImageHealthCheck(ctx, opts); err != nil {
			return err
		}
	}

	// Snapshot the pre-existing .env (if any) BEFORE the generator runs.
	// The generator unconditionally writes a fresh .env with randomised
	// credentials every invocation; without this snapshot, re-running
	// `vibew bundle` would destroy user edits even though the idempotency
	// contract says the .env is preserved across runs.
	priorDotEnv, priorExisted := s.snapshotPriorDotEnv(outDir)

	// Defer-safe restore: if the generator clobbers .env and then something
	// downstream fails (bundleSingleSite OR writeBundleExtras), the user's
	// snapshot must come back. Without this defer, a mid-run failure leaves
	// the fresh-random-credentials .env on disk and destroys user edits —
	// silently breaking the "preserved across runs" contract. Opts.Overwrite
	// is the only path where the user explicitly asked to drop the snapshot.
	restored := false
	defer func() {
		if restored || !priorExisted || opts.Overwrite || s.bundleFS == nil {
			return
		}
		// Best-effort restore on the error path. We deliberately ignore any
		// restore error here because the caller already has an error to
		// surface, and writing into a directory the generator just failed
		// inside may itself fail — we do not want to mask the real cause.
		path := filepath.Join(outDir, fileDotEnv)
		_ = s.bundleFS.WriteFile(path, priorDotEnv, 0o600)
	}()

	if err := s.bundleSingleSite(ctx, opts.Config, opts.ConfigPath, opts.ProdConfigPath, opts.ProjectName, outDir); err != nil {
		return err
	}
	// Bundle extras (sample.env, .env, deploy.sh, README.md, image.tar) are
	// additive. They run only after the base generator has succeeded so a
	// failing compose render does not leave half a bundle on disk with a
	// fresh .env. See bundle_extras.go.
	if err := s.writeBundleExtras(ctx, opts, outDir, priorDotEnv, priorExisted); err != nil {
		return err
	}
	// writeBundleExtras already handled the snapshot per its own idempotency
	// rules. Signal the deferred guard to skip the error-path restore so it
	// does not clobber whatever the extras pipeline decided to write.
	restored = true
	return nil
}

// ErrImageMissing is a sentinel that the CLI layer maps to exit code 2.
// It is returned (wrapping ports.ErrImageNotFound) when the target image is
// absent from the local Docker daemon and --build was not requested.
var ErrImageMissing = errors.New("image missing from local Docker")

// runImageHealthCheck executes the image inspection + staleness walk, writes
// the rendered health block to opts.Out (or discards it when nil), and returns
// an error when the image is absent or Docker is unreachable.
func (s *Service) runImageHealthCheck(ctx context.Context, opts BundleOptions) error {
	projectRoot := opts.ProjectRoot
	if projectRoot == "" && opts.ConfigPath != "" {
		projectRoot = filepath.Dir(opts.ConfigPath)
	}

	health, err := CheckImageHealth(ctx, CheckImageHealthOptions{
		ImageTag:       opts.ImageTag,
		ProjectRoot:    projectRoot,
		TargetPlatform: opts.TargetPlatform,
		AllowStale:     opts.AllowStale,
		Inspector:      s.imageInspector,
		Walker:         s.stalenessWalker,
	})
	if err != nil {
		if errors.Is(err, ports.ErrDockerUnavailable) {
			return fmt.Errorf("docker daemon unreachable: %w", ports.ErrDockerUnavailable)
		}
		if errors.Is(err, ports.ErrImageNotFound) {
			// Intentionally multi-line: this is the user-facing hard-failure
			// message mandated by ADR-089 §F. The newlines and period are
			// part of the UX copy, not a Go error string convention.
			//nolint:revive,staticcheck // multi-line actionable message, not a wrapped log string
			msg := fmt.Sprintf("image %s not found in local Docker\n\nFix one of:\n  vibew bundle --build\n  vibew build --platform linux/amd64\n  docker buildx build --platform linux/amd64 --load -t %s .",
				opts.ImageTag, opts.ImageTag)
			return fmt.Errorf("%w: %s", ErrImageMissing, msg)
		}
		return err
	}

	// Render the health block to opts.Out when provided.
	if opts.Out != nil {
		RenderImageHealth(opts.Out, health)
		fmt.Fprintln(opts.Out)
	}

	return nil
}

// snapshotPriorDotEnv reads the existing .env in outDir so the extras
// pipeline can restore it after the generator clobbers the file with fresh
// random credentials. Returns (nil, false) when the file is absent or when
// no BundleFS is wired (in which case the extras pipeline is a no-op).
func (s *Service) snapshotPriorDotEnv(outDir string) ([]byte, bool) {
	if s.bundleFS == nil {
		return nil, false
	}
	path := filepath.Join(outDir, fileDotEnv)
	exists, err := s.bundleFS.Exists(path)
	if err != nil || !exists {
		return nil, false
	}
	data, err := s.bundleFS.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// BundleSidecar produces the sidecar compose and global.yaml under
// <outputDir>/.sidecar/. This is called during multi-site bootstrap.
func (s *Service) BundleSidecar(_ context.Context, cfg *config.Config, outputDir string) error {
	if outputDir == "" {
		outputDir = defaultBundleDir
	}
	return bundleMultiSiteSidecar(cfg, outputDir)
}

// bundleSingleSite generates the complete single-site deploy bundle.
func (s *Service) bundleSingleSite(ctx context.Context, cfg *config.Config, configPath, prodConfigPath, projectName string, outputDir string) error {
	// Load and overlay the production override into the Config struct so that
	// the compose template uses production values (port 443, letsencrypt, etc.)
	// for port bindings, TLS config, and other template-driven settings.
	//
	// When configPath is empty or the file does not exist on disk (unit tests
	// that drive the service with an in-memory cfg), fall back to the
	// caller-supplied cfg so the template still renders. Any other stat error
	// (permission denied, I/O failure) surfaces so we never silently drop a
	// user-supplied config — that was the class of bug behind #1053.
	mergedCfg := cfg
	if configPath != "" {
		_, statErr := os.Stat(configPath)
		switch {
		case statErr == nil:
			loaded, err := LoadMergedConfig(configPath, prodConfigPath)
			if err != nil {
				return err
			}
			mergedCfg = loaded
		case errors.Is(statErr, fs.ErrNotExist):
			// Intentional fall-through: in-memory cfg is the source of truth.
		default:
			return fmt.Errorf("stat config %s: %w", configPath, statErr)
		}
	}

	// Resolve upstream.host on the merged Config for template rendering.
	resolved := ResolveProdConfig(mergedCfg, projectName, false)

	// Set deploy mode so the template uses the original App.Build value as
	// the build context (e.g. ".") instead of the resolved ProjectRoot.
	resolved.DeployMode = true

	// When app.build is set, the image is built locally by `vibew build` and
	// transferred via docker save/load. The deploy compose must use `image:`
	// (not `build:`) because no source code is present on the server.
	if resolved.App.Build != "" {
		resolved.App.Image = mergedCfg.ComposeProjectName() + "-app:latest"
		resolved.App.Build = ""
	}

	// Use the generator to produce docker-compose.yml, kratos/, credentials,
	// etc. The generator writes directly into outputDir. This must run BEFORE
	// the merged config write because the generator copies the original
	// vibewarden.yaml into outputDir — the merged write overwrites it.
	if err := s.generator.Generate(ctx, resolved.ToGeneratorInput(), outputDir); err != nil {
		return fmt.Errorf("generating config files: %w", err)
	}

	// Build the merged YAML map for writing vibewarden.yaml.
	// This preserves original field names (rate_limit, security_headers, etc.).
	// Written AFTER generator.Generate() to overwrite the unmerged copy.
	configData, err := buildMergedConfigYAML(configPath, prodConfigPath, projectName, false, cfg)
	if err != nil {
		return fmt.Errorf("building merged config YAML: %w", err)
	}
	if err := writeFile(filepath.Join(outputDir, "vibewarden.yaml"), configData); err != nil {
		return fmt.Errorf("writing vibewarden.yaml to bundle: %w", err)
	}

	return nil
}

// bundleMultiSiteSite generates the per-site deploy bundle files under
// <outputDir>/sites/<project>/.
func (s *Service) bundleMultiSiteSite(_ context.Context, cfg *config.Config, configPath, prodConfigPath, projectName string, outputDir string) error {
	siteDir := filepath.Join(outputDir, "sites", projectName)

	// Build the merged YAML map for writing vibewarden.yaml.
	configData, err := buildMergedConfigYAML(configPath, prodConfigPath, projectName, true, cfg)
	if err != nil {
		return fmt.Errorf("building merged config YAML: %w", err)
	}
	if err := writeFile(filepath.Join(siteDir, "vibewarden.yaml"), configData); err != nil {
		return fmt.Errorf("writing vibewarden.yaml to bundle: %w", err)
	}

	// When app.build is set, the image is built locally and transferred via
	// docker save/load. The per-app compose must use image: instead of build:.
	composeCfg := cfg
	if cfg.App.Build != "" {
		cfgCopy := *cfg
		cfgCopy.App.Image = cfg.ComposeProjectName() + "-app:latest"
		cfgCopy.App.Build = ""
		composeCfg = &cfgCopy
	}

	// Render and write the per-app compose file.
	appCompose, err := renderAppCompose(composeCfg, projectName)
	if err != nil {
		return fmt.Errorf("rendering app compose: %w", err)
	}
	if err := writeFile(filepath.Join(siteDir, "docker-compose.yml"), []byte(appCompose)); err != nil {
		return fmt.Errorf("writing app docker-compose.yml to bundle: %w", err)
	}

	return nil
}

// bundleMultiSiteSidecar generates the sidecar compose and global.yaml under
// <outputDir>/.sidecar/.
func bundleMultiSiteSidecar(cfg *config.Config, outputDir string) error {
	sidecarBundleDir := filepath.Join(outputDir, ".sidecar")

	listenPort := cfg.Server.Port
	if listenPort == 0 {
		listenPort = defaultHealthPort
	}

	// Write global.yaml.
	globalYAML := renderGlobalYAML(listenPort)
	if err := writeFile(filepath.Join(sidecarBundleDir, "global.yaml"), []byte(globalYAML)); err != nil {
		return fmt.Errorf("writing global.yaml to bundle: %w", err)
	}

	// Render and write the sidecar compose file.
	sidecarCompose, err := renderSidecarCompose(listenPort)
	if err != nil {
		return fmt.Errorf("rendering sidecar compose: %w", err)
	}
	if err := writeFile(filepath.Join(sidecarBundleDir, "docker-compose.yml"), []byte(sidecarCompose)); err != nil {
		return fmt.Errorf("writing sidecar docker-compose.yml to bundle: %w", err)
	}

	return nil
}

// renderGlobalYAML produces the global.yaml content for the sidecar.
func renderGlobalYAML(listenPort int) string {
	return fmt.Sprintf(`# global.yaml — VibeWarden sidecar global configuration
# Generated by vibew bundle — do not edit manually.
listen_port: %d
log_level: info
`, listenPort)
}

// renderSidecarCompose renders the sidecar docker-compose.yml template.
func renderSidecarCompose(listenPort int) (string, error) {
	tmplContent, err := templates.FS.ReadFile("sidecar-compose.yml.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading sidecar compose template: %w", err)
	}

	tmpl, err := template.New("sidecar-compose").Funcs(templateadapter.SharedFuncMap()).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("parsing sidecar compose template: %w", err)
	}

	data := SidecarComposeData{ListenPort: listenPort}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing sidecar compose template: %w", err)
	}

	return buf.String(), nil
}

// renderAppCompose renders the per-app docker-compose.yml template.
func renderAppCompose(cfg *config.Config, projectName string) (string, error) {
	tmplContent, err := templates.FS.ReadFile("app-compose.yml.tmpl")
	if err != nil {
		return "", fmt.Errorf("reading app compose template: %w", err)
	}

	tmpl, err := template.New("app-compose").Funcs(templateadapter.SharedFuncMap()).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("parsing app compose template: %w", err)
	}

	healthcheck := "none"
	if cfg.App.Healthcheck != "none" && cfg.App.Healthcheck != "" {
		healthcheck = cfg.App.Healthcheck
	}

	data := AppComposeData{
		ProjectName:    projectName,
		AppImage:       cfg.App.Image,
		AppBuild:       cfg.App.Build,
		AppHealthcheck: healthcheck,
		UpstreamPort:   cfg.Upstream.Port,
		AppLanguage:    cfg.App.Language,
		AppEnvironment: cfg.App.Environment,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing app compose template: %w", err)
	}

	return buf.String(), nil
}

// buildMergedConfigYAML reads the base config YAML, optionally deep-merges a
// production override YAML, resolves upstream.host for Docker networking, and
// returns the result as marshalled YAML bytes. This approach avoids marshalling
// the Config struct (which only has mapstructure tags, not yaml tags) so that
// multi-word field names like rate_limit and security_headers are preserved.
//
// When configPath is empty or the file does not exist, cfg is used as a
// fallback: it is marshalled to YAML via yaml.Marshal and used as the base.
// This preserves backwards compatibility with callers that supply only a Config.
func buildMergedConfigYAML(configPath, prodConfigPath, projectName string, multiSite bool, cfg *config.Config) ([]byte, error) {
	var baseYAML []byte
	var err error

	if configPath != "" {
		baseYAML, err = os.ReadFile(configPath) //nolint:gosec // configPath is the vibewarden.yaml resolved from project root
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading base config %s: %w", configPath, err)
		}
	}

	// Fallback: marshal the Config struct when no file is available.
	if len(baseYAML) == 0 && cfg != nil {
		baseYAML, err = yaml.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("marshalling config fallback: %w", err)
		}
	}

	var overrideYAML []byte
	if prodConfigPath != "" {
		overrideYAML, err = os.ReadFile(prodConfigPath) //nolint:gosec // prodConfigPath is the production override resolved from project root
		if err != nil {
			return nil, fmt.Errorf("reading production config %s: %w", prodConfigPath, err)
		}
	}

	merged, err := MergeConfigYAML(baseYAML, overrideYAML)
	if err != nil {
		return nil, fmt.Errorf("merging config YAML: %w", err)
	}

	ResolveProdUpstream(merged, projectName, multiSite)

	data, err := MarshalYAMLMap(merged)
	if err != nil {
		return nil, fmt.Errorf("marshalling merged config: %w", err)
	}

	return data, nil
}

// writeFile writes data to the named file, creating parent directories as
// needed. It overwrites any existing file.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
