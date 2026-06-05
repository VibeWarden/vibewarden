package bundle

// defaultHealthPort is the port used when the config does not specify one.
// It is consumed by the bundle template when rendering the generated
// README.md (which documents the deploy commands).
const defaultHealthPort = 8443

// SidecarComposeData holds the template data for the sidecar compose file
// rendered by the bundle pipeline.
type SidecarComposeData struct {
	// ListenPort is the port the sidecar binds to for HTTPS traffic.
	ListenPort int

	// Image is the fully-qualified Docker image reference for the sidecar,
	// e.g. "ghcr.io/vibewarden/vibewarden:0.20.0" for a release build or
	// "ghcr.io/vibewarden/vibewarden:latest" for a dev/source build.
	// Populated by renderSidecarCompose via SidecarImageRef.
	Image string

	// PullPolicy is the Docker Compose pull_policy value. Empty string means
	// the field is omitted entirely (default "missing" — correct for pinned
	// immutable release tags). "always" is used for dev/source builds.
	PullPolicy string
}

// AppComposeData holds the template data for a per-app compose file rendered
// by the bundle pipeline.
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
