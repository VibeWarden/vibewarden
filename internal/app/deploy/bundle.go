package deploy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// BundleOptions holds parameters for producing the deploy bundle.
type BundleOptions struct {
	// Config is the user's loaded configuration.
	Config *config.Config

	// ConfigPath is the path to the original config file on disk.
	ConfigPath string

	// ProjectName is the DNS-safe project name used in directory and container
	// naming.
	ProjectName string

	// MultiSite indicates whether the deploy targets a multi-site layout.
	MultiSite bool

	// OutputDir is the directory where bundle files are written. Defaults to
	// ".vibewarden/deploy" when empty.
	OutputDir string
}

// defaultBundleDir is the default output directory for deploy bundles.
const defaultBundleDir = ".vibewarden/deploy"

// Bundle produces a complete deploy bundle under opts.OutputDir (or
// .vibewarden/deploy/ by default).
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
		outDir = defaultBundleDir
	}

	if opts.MultiSite {
		return s.bundleMultiSiteSite(ctx, opts.Config, opts.ProjectName, outDir)
	}
	return s.bundleSingleSite(ctx, opts.Config, opts.ProjectName, outDir)
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
func (s *Service) bundleSingleSite(ctx context.Context, cfg *config.Config, projectName string, outputDir string) error {
	// Resolve upstream.host for production Docker networking.
	resolved := ResolveProdConfig(cfg, projectName, false)

	// Write resolved vibewarden.yaml into the bundle.
	configData, err := MarshalConfig(resolved)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
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
func (s *Service) bundleMultiSiteSite(_ context.Context, cfg *config.Config, projectName string, outputDir string) error {
	siteDir := filepath.Join(outputDir, "sites", projectName)

	// Resolve upstream.host for production Docker networking.
	resolved := ResolveProdConfig(cfg, projectName, true)

	// Write resolved vibewarden.yaml.
	configData, err := MarshalConfig(resolved)
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
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
