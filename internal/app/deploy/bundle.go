package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// BundleOptions holds parameters for producing the deploy bundle.
type BundleOptions struct {
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
	return s.bundleSingleSite(ctx, opts.Config, opts.ConfigPath, opts.ProdConfigPath, opts.ProjectName, outDir)
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
	// Resolve upstream.host on the typed Config for template rendering.
	resolved := ResolveProdConfig(cfg, projectName, false)

	// Build the merged YAML map for writing vibewarden.yaml.
	// This preserves original field names (rate_limit, security_headers, etc.).
	configData, err := buildMergedConfigYAML(configPath, prodConfigPath, projectName, false, cfg)
	if err != nil {
		return fmt.Errorf("building merged config YAML: %w", err)
	}
	if err := writeFile(filepath.Join(outputDir, "vibewarden.yaml"), configData); err != nil {
		return fmt.Errorf("writing vibewarden.yaml to bundle: %w", err)
	}

	// Use the generator to produce docker-compose.yml, kratos/, credentials,
	// etc. The generator writes directly into outputDir.
	if err := s.generator.Generate(ctx, resolved.ToGeneratorInput(), outputDir); err != nil {
		return fmt.Errorf("generating config files: %w", err)
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

	// Render and write the per-app compose file.
	appCompose, err := renderAppCompose(cfg, projectName)
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
# Generated by vibew deploy — do not edit manually.
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
