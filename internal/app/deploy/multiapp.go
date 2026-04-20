package deploy

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/config"
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

	// AppEnvironment is a map of custom environment variables to inject into
	// the app container.
	AppEnvironment map[string]string
}

// BootstrapSidecar creates the full multi-app directory layout on the remote
// host and starts the shared sidecar container. This is called when Detect
// returns DeployModeFreshInstall.
//
// It produces a local deploy bundle under .vibewarden/deploy/ containing the
// sidecar compose, global.yaml, and the first site's compose and config.
// Files are then rsynced to the remote -- no sed or runtime patching.
//
// The created remote layout:
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

	bundleDir := opts.GeneratedDir
	if bundleDir == "" {
		bundleDir = defaultBundleDir
	}

	// Determine whether a locally-built image will be transferred.
	imageName := cfg.App.Image
	if cfg.App.Build != "" {
		imageName = cfg.ComposeProjectName() + "-app:latest"
	}
	willTransferLocalImage := imageName != "" && isLocalImage(imageName) && s.imageExporter != nil

	// Step 1: produce the local deploy bundle.
	fmt.Fprintln(out, "Bundling sidecar and first site locally...")
	if err := s.BundleSidecar(ctx, cfg, bundleDir); err != nil {
		return fmt.Errorf("bundling sidecar: %w", err)
	}
	if err := s.Bundle(ctx, BundleOptions{
		Config:         cfg,
		ConfigPath:     opts.ConfigPath,
		ProdConfigPath: opts.ProdConfigPath,
		ProjectName:    projectName,
		MultiSite:      true,
		OutputDir:      bundleDir,
		Env:            opts.Env,
	}); err != nil {
		return fmt.Errorf("bundling first site: %w", err)
	}

	// Step 1b: verify remote prerequisites.
	fmt.Fprintln(out, "Verifying remote prerequisites...")
	if err := s.checkRemotePrerequisites(ctx, willTransferLocalImage); err != nil {
		return fmt.Errorf("remote prerequisites check failed: %w", err)
	}

	// Step 2: create remote directories and shared Docker network.
	fmt.Fprintln(out, "Creating multi-app directory layout...")
	siteDir := sitesDir + projectName + "/"
	mkdirCmd := fmt.Sprintf("mkdir -p %s %s", sidecarDir, siteDir)
	if _, err := s.executor.Run(ctx, mkdirCmd); err != nil {
		return fmt.Errorf("creating directory layout: %w", err)
	}

	fmt.Fprintln(out, "Creating shared Docker network...")
	netCmd := fmt.Sprintf("docker network create %s 2>/dev/null || true", multiappNetwork)
	if _, err := s.executor.Run(ctx, netCmd); err != nil {
		return fmt.Errorf("creating docker network: %w", err)
	}

	// Step 3: rsync sidecar bundle to remote.
	fmt.Fprintln(out, "Transferring sidecar files...")
	sidecarBundleSrc := filepath.Join(bundleDir, ".sidecar")
	if err := s.executor.Transfer(ctx, sidecarBundleSrc, sidecarDir, true); err != nil {
		return fmt.Errorf("transferring sidecar bundle: %w", err)
	}

	// Step 4: rsync site bundle to remote.
	fmt.Fprintf(out, "Transferring site %q files...\n", projectName)
	siteBundleSrc := filepath.Join(bundleDir, "sites", projectName)
	if err := s.executor.Transfer(ctx, siteBundleSrc, siteDir, true); err != nil {
		return fmt.Errorf("transferring site bundle: %w", err)
	}

	// Step 4b: transfer local image if applicable.
	if willTransferLocalImage {
		if err := s.transferLocalImage(ctx, imageName, siteDir, out); err != nil {
			return fmt.Errorf("transferring local image: %w", err)
		}
	}

	// Step 5: start the app container.
	fmt.Fprintf(out, "Starting site %q...\n", projectName)
	startCmd := fmt.Sprintf("cd %s && docker compose up -d", siteDir)
	if _, err := s.executor.Run(ctx, startCmd); err != nil {
		return fmt.Errorf("starting app container: %w", err)
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
	healthURL := healthCheckURL(port, cfg.TLS.Enabled)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s (via SSH)...\n", healthURL)
	if !s.waitHealthy(ctx, port, cfg.TLS.Enabled, out) {
		fmt.Fprintln(out, "Bootstrap completed but health check failed — verify with: vibew deploy status")
		return ErrHealthCheck
	}

	fmt.Fprintln(out, "Bootstrap complete.")
	return nil
}

// DeployMultiApp adds a new site to an existing VibeWarden sidecar
// installation. This is called when Detect returns DeployModeAddSite.
//
// It produces a local deploy bundle with the per-app configuration and compose
// file, rsyncs it to ~/vibewarden/sites/<project>/, and then restarts the
// sidecar to pick up the new site configuration.
func (s *Service) DeployMultiApp(ctx context.Context, cfg *config.Config, opts RunOptions) error {
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	projectName := opts.ProjectName
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}

	bundleDir := opts.GeneratedDir
	if bundleDir == "" {
		bundleDir = defaultBundleDir
	}

	// Determine whether a locally-built image will be transferred.
	imageName := cfg.App.Image
	if cfg.App.Build != "" {
		imageName = cfg.ComposeProjectName() + "-app:latest"
	}
	willTransferLocalImage := imageName != "" && isLocalImage(imageName) && s.imageExporter != nil

	// Step 1: produce the local deploy bundle for this site.
	fmt.Fprintf(out, "Bundling site %q locally...\n", projectName)
	if err := s.Bundle(ctx, BundleOptions{
		Config:         cfg,
		ConfigPath:     opts.ConfigPath,
		ProdConfigPath: opts.ProdConfigPath,
		ProjectName:    projectName,
		MultiSite:      true,
		OutputDir:      bundleDir,
		Env:            opts.Env,
	}); err != nil {
		return fmt.Errorf("bundling site: %w", err)
	}

	// Step 1b: verify remote prerequisites.
	fmt.Fprintln(out, "Verifying remote prerequisites...")
	if err := s.checkRemotePrerequisites(ctx, willTransferLocalImage); err != nil {
		return fmt.Errorf("remote prerequisites check failed: %w", err)
	}

	// Step 2: ensure remote site directory exists.
	siteDir := sitesDir + projectName + "/"
	if _, err := s.executor.Run(ctx, "mkdir -p "+siteDir); err != nil {
		return fmt.Errorf("creating site directory: %w", err)
	}

	// Step 3: rsync site bundle to remote.
	fmt.Fprintf(out, "Transferring site %q files...\n", projectName)
	siteBundleSrc := filepath.Join(bundleDir, "sites", projectName)
	if err := s.executor.Transfer(ctx, siteBundleSrc, siteDir, true); err != nil {
		return fmt.Errorf("transferring site bundle: %w", err)
	}

	// Step 3b: transfer local image if applicable.
	if willTransferLocalImage {
		if err := s.transferLocalImage(ctx, imageName, siteDir, out); err != nil {
			return fmt.Errorf("transferring local image: %w", err)
		}
	}

	// Step 4: start the app container.
	fmt.Fprintf(out, "Starting site %q...\n", projectName)
	startCmd := fmt.Sprintf("cd %s && docker compose up -d", siteDir)
	if _, err := s.executor.Run(ctx, startCmd); err != nil {
		return fmt.Errorf("starting app container: %w", err)
	}

	// Step 5: restart the sidecar to pick up the new site.
	fmt.Fprintln(out, "Restarting sidecar to load new site...")
	restartCmd := fmt.Sprintf("cd %s && docker compose restart vibewarden", sidecarDir)
	if _, err := s.executor.Run(ctx, restartCmd); err != nil {
		return fmt.Errorf("restarting sidecar: %w", err)
	}

	// Step 6: health check.
	port := cfg.Server.Port
	if port == 0 {
		port = defaultHealthPort
	}
	healthURL := healthCheckURL(port, cfg.TLS.Enabled)
	fmt.Fprintf(out, "Waiting for sidecar health check at %s (via SSH)...\n", healthURL)
	if !s.waitHealthy(ctx, port, cfg.TLS.Enabled, out) {
		fmt.Fprintln(out, "Site deployed but health check failed — verify with: vibew deploy status")
		return ErrHealthCheck
	}

	fmt.Fprintln(out, "Site deployed.")
	return nil
}

// appContainerName returns the Docker container name for a site's app
// container. This must match the container_name in the app-compose template.
func appContainerName(projectName string) string {
	return "vibewarden-" + projectName + "-app"
}

// isLocalUpstreamHost returns true if host is a loopback or wildcard address
// that would not work in Docker container-to-container networking.
func isLocalUpstreamHost(host string) bool {
	switch host {
	case "0.0.0.0", "127.0.0.1", "localhost":
		return true
	default:
		return false
	}
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
