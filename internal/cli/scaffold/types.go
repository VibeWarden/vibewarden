// Package scaffold provides value objects and types for the VibeWarden
// project scaffolding subsystem.
package scaffold

// ProjectType represents the detected project type.
type ProjectType string

const (
	// ProjectTypeNode indicates a Node.js project (package.json present).
	ProjectTypeNode ProjectType = "node"
	// ProjectTypeGo indicates a Go project (go.mod present).
	ProjectTypeGo ProjectType = "go"
	// ProjectTypePython indicates a Python project (requirements.txt present).
	ProjectTypePython ProjectType = "python"
	// ProjectTypeUnknown indicates no recognised project type was detected.
	ProjectTypeUnknown ProjectType = "unknown"
)

// ProjectConfig holds detected state about the project directory.
// It is a value object — equality by value, no mutation after construction.
type ProjectConfig struct {
	// Type is the detected project type.
	Type ProjectType

	// DetectedPort is the upstream port inferred from project files.
	// Zero means no port was detected.
	DetectedPort int

	// HasDockerCompose is true when docker-compose.yml already exists.
	HasDockerCompose bool

	// HasVibeWardenConfig is true when vibewarden.yaml already exists.
	HasVibeWardenConfig bool
}

// ScaffoldOptions holds the user-supplied options that drive file generation.
// It is a value object — all fields set at construction.
type ScaffoldOptions struct {
	// UpstreamPort is the port that the user's application listens on.
	UpstreamPort int

	// AuthEnabled enables Ory Kratos authentication scaffolding.
	AuthEnabled bool

	// RateLimitEnabled enables rate limiting scaffolding.
	RateLimitEnabled bool

	// TLSEnabled enables TLS scaffolding.
	TLSEnabled bool

	// TLSDomain is the domain for the TLS certificate.
	// Required when TLSEnabled is true.
	TLSDomain string

	// Force allows overwriting existing files without prompting.
	Force bool
}

// TemplateData is the data passed to every template when rendering.
type TemplateData struct {
	// UpstreamPort is the port of the protected application.
	UpstreamPort int

	// AuthEnabled controls whether auth-related sections are rendered.
	AuthEnabled bool

	// RateLimitEnabled controls whether rate-limit sections are rendered.
	RateLimitEnabled bool

	// TLSEnabled controls whether TLS sections are rendered.
	TLSEnabled bool

	// TLSDomain is the domain for TLS.
	TLSDomain string
}
