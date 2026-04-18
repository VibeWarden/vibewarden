package deploy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"text/template"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/config/templates"
)

const (
	// sidecarDir is the directory on the remote host where the sidecar's own
	// compose file and global.yaml live.
	sidecarDir = remoteBaseDir + "/.sidecar/"

	// sitesDir is the parent directory on the remote host for all per-app
	// site directories.
	sitesDir = remoteBaseDir + "/sites/"

	// multiappNetwork is the shared Docker network name that connects the
	// sidecar container to all per-app containers.
	multiappNetwork = "vibewarden-multiapp"
)

// SidecarComposeData holds the template data for the sidecar compose file.
type SidecarComposeData struct {
	// ListenPort is the port the sidecar binds to for HTTPS traffic.
	ListenPort int
}

// AppComposeData holds the template data for a per-app compose file.
type AppComposeData struct {
	// ProjectName is the DNS-safe name used as the Docker service prefix.
	ProjectName string

	// AppImage is the Docker image reference when using image mode.
	// Empty when using build mode.
	AppImage string

	// AppBuild is the build context path when building the image locally.
	// Empty when using image mode.
	AppBuild string

	// AppHealthcheck is the health check command for the app container.
	// Set to "none" to disable the health check.
	AppHealthcheck string

	// UpstreamPort is the port the app listens on inside the container.
	UpstreamPort int

	// AppLanguage is the app's language/runtime for health check probe selection.
	AppLanguage string
}

// BootstrapSidecar creates the full multi-app directory layout on the remote
// host and starts the shared sidecar container. This is called when Detect
// returns DeployModeFreshInstall.
//
// The created layout:
//
//	~/vibewarden/
//	  .sidecar/
//	    global.yaml
//	    docker-compose.yml   (sidecar compose)
//	  sites/
//	    <project>/
//	      vibewarden.yaml
//	      docker-compose.yml (per-app compose)
func (s *Service) BootstrapSidecar(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}

	// Step 1: create the directory layout.
	fmt.Fprintln(out, "Creating multi-app directory layout...")
	siteDir := sitesDir + projectName + "/"
	mkdirCmd := fmt.Sprintf("mkdir -p %s %s", sidecarDir, siteDir)
	if _, err := s.executor.Run(ctx, mkdirCmd); err != nil {
		return fmt.Errorf("creating directory layout: %w", err)
	}

	// Step 2: create the shared Docker network.
	fmt.Fprintln(out, "Creating shared Docker network...")
	netCmd := fmt.Sprintf("docker network create %s 2>/dev/null || true", multiappNetwork)
	if _, err := s.executor.Run(ctx, netCmd); err != nil {
		return fmt.Errorf("creating docker network: %w", err)
	}

	// Step 3: write global.yaml to the remote.
	fmt.Fprintln(out, "Writing global.yaml...")
	listenPort := cfg.Server.Port
	if listenPort == 0 {
		listenPort = defaultHealthPort
	}
	globalYAML := renderGlobalYAML(listenPort)
	if err := s.writeRemoteFile(ctx, sidecarDir+"global.yaml", globalYAML); err != nil {
		return fmt.Errorf("writing global.yaml: %w", err)
	}

	// Step 4: render and write the sidecar compose file.
	fmt.Fprintln(out, "Writing sidecar docker-compose.yml...")
	sidecarCompose, err := renderSidecarCompose(listenPort)
	if err != nil {
		return fmt.Errorf("rendering sidecar compose: %w", err)
	}
	if err := s.writeRemoteFile(ctx, sidecarDir+"docker-compose.yml", sidecarCompose); err != nil {
		return fmt.Errorf("writing sidecar docker-compose.yml: %w", err)
	}

	// Step 5: deploy the first site.
	fmt.Fprintf(out, "Deploying site %q...\n", projectName)
	if err := s.deploySite(ctx, cfg, projectName, opts); err != nil {
		return fmt.Errorf("deploying first site: %w", err)
	}

	// Step 6: start the sidecar.
	fmt.Fprintln(out, "Starting sidecar...")
	if err := s.startSidecar(ctx); err != nil {
		return fmt.Errorf("starting sidecar: %w", err)
	}

	// Step 7: health check.
	port := cfg.Server.Port
	if port == 0 {
		port = defaultHealthPort
	}
	healthURL := fmt.Sprintf("http://localhost:%d/_vibewarden/health", port)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s (via SSH)...\n", healthURL)
	s.waitHealthy(ctx, port, out)

	fmt.Fprintln(out, "Bootstrap complete.")
	return nil
}

// DeployMultiApp adds a new site to an existing VibeWarden sidecar
// installation. This is called when Detect returns DeployModeAddSite.
//
// It writes the per-app configuration and compose file to
// ~/vibewarden/sites/<project>/ and then restarts the sidecar to pick up
// the new site configuration.
func (s *Service) DeployMultiApp(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}

	// Step 1: deploy the site files.
	fmt.Fprintf(out, "Deploying site %q to existing sidecar...\n", projectName)
	if err := s.deploySite(ctx, cfg, projectName, opts); err != nil {
		return fmt.Errorf("deploying site: %w", err)
	}

	// Step 2: restart the sidecar to pick up the new site.
	fmt.Fprintln(out, "Restarting sidecar to load new site...")
	restartCmd := fmt.Sprintf("cd %s && docker compose restart vibewarden", sidecarDir)
	if _, err := s.executor.Run(ctx, restartCmd); err != nil {
		return fmt.Errorf("restarting sidecar: %w", err)
	}

	// Step 3: health check.
	port := cfg.Server.Port
	if port == 0 {
		port = defaultHealthPort
	}
	healthURL := fmt.Sprintf("http://localhost:%d/_vibewarden/health", port)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s (via SSH)...\n", healthURL)
	s.waitHealthy(ctx, port, out)

	fmt.Fprintln(out, "Site deployed.")
	return nil
}

// deploySite writes the per-app vibewarden.yaml and docker-compose.yml to
// the site directory on the remote host.
func (s *Service) deploySite(ctx context.Context, cfg *config.Config, projectName string, opts RunOptions) error {
	siteDir := sitesDir + projectName + "/"

	// Ensure the site directory exists.
	if _, err := s.executor.Run(ctx, "mkdir -p "+siteDir); err != nil {
		return fmt.Errorf("creating site directory: %w", err)
	}

	// Transfer the config file as vibewarden.yaml.
	if err := s.executor.TransferFile(ctx, opts.ConfigPath, siteDir+"vibewarden.yaml"); err != nil {
		return fmt.Errorf("transferring vibewarden.yaml: %w", err)
	}

	// Render and write the per-app compose file.
	appCompose, err := renderAppCompose(cfg, projectName)
	if err != nil {
		return fmt.Errorf("rendering app compose: %w", err)
	}
	if err := s.writeRemoteFile(ctx, siteDir+"docker-compose.yml", appCompose); err != nil {
		return fmt.Errorf("writing app docker-compose.yml: %w", err)
	}

	// Start the app container.
	startCmd := fmt.Sprintf("cd %s && docker compose up -d", siteDir)
	if _, err := s.executor.Run(ctx, startCmd); err != nil {
		return fmt.Errorf("starting app container: %w", err)
	}

	return nil
}

// startSidecar pulls the latest sidecar image and starts the sidecar
// container on the remote host.
func (s *Service) startSidecar(ctx context.Context) error {
	pullCmd := fmt.Sprintf("cd %s && docker compose pull", sidecarDir)
	if _, err := s.executor.Run(ctx, pullCmd); err != nil {
		return fmt.Errorf("docker compose pull sidecar: %w", err)
	}

	upCmd := fmt.Sprintf("cd %s && docker compose up -d", sidecarDir)
	if _, err := s.executor.Run(ctx, upCmd); err != nil {
		return fmt.Errorf("docker compose up sidecar: %w", err)
	}

	return nil
}

// writeRemoteFile writes content to a file on the remote host using printf
// piped through tee for idempotent file creation.
func (s *Service) writeRemoteFile(ctx context.Context, remotePath, content string) error {
	// Use a heredoc to write the file content to avoid shell quoting issues.
	cmd := fmt.Sprintf("cat > %s << 'VIBEWARDEN_EOF'\n%s\nVIBEWARDEN_EOF", remotePath, content)
	if _, err := s.executor.Run(ctx, cmd); err != nil {
		return fmt.Errorf("writing %s: %w", remotePath, err)
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
