// Package scaffold contains the core value objects for the VibeWarden
// project scaffolding subsystem. This package has zero external dependencies
// and is safe to import from any layer.
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
//
//nolint:revive // ScaffoldOptions is the established public API name used across the scaffold package
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

// TemplateData is the data passed to every scaffold template when rendering.
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

	// Version is the VibeWarden release version to pin in .vibewarden-version.
	// When empty the wrapper falls back to the latest GitHub release at runtime.
	Version string
}

// Language represents a supported programming language for project scaffolding.
type Language string

const (
	// LanguageGo is the Go programming language.
	LanguageGo Language = "go"
	// LanguageKotlin is the Kotlin programming language.
	LanguageKotlin Language = "kotlin"
	// LanguageTypeScript is the TypeScript programming language.
	LanguageTypeScript Language = "typescript"
)

// SanitizePackageName converts a project name into a valid JVM/TypeScript package
// identifier by replacing hyphens and dots with underscores and lowercasing.
func SanitizePackageName(name string) string {
	var b []byte
	for _, c := range []byte(name) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32) // lowercase
		case c == '-', c == '.':
			b = append(b, '_')
		default:
			b = append(b, '_')
		}
	}
	return string(b)
}

// InitProjectData is the data passed to project scaffold templates when rendering.
type InitProjectData struct {
	// ProjectName is the name of the project (directory name, used in go.mod).
	ProjectName string

	// ModulePath is the Go module path (e.g., "github.com/user/myproject").
	ModulePath string

	// PackageName is the sanitized project name safe for JVM/TypeScript package identifiers.
	// Hyphens are replaced with underscores, everything is lowercased.
	PackageName string

	// GroupID is the JVM group identifier (e.g., "com.mycompany").
	// Defaults to the sanitized project name when not specified.
	GroupID string

	// Port is the HTTP port the generated app listens on.
	Port int

	// Language is the target programming language.
	Language Language

	// Description is an optional one-line description of what the project builds.
	// When set it is included in PROJECT.md, CLAUDE.md, and agent files so that
	// AI coding assistants have context about the project's purpose from the start.
	Description string
}

// AgentType identifies the target AI coding assistant for context generation.
type AgentType string

const (
	// AgentTypeClaude targets Claude Code (.claude/CLAUDE.md).
	AgentTypeClaude AgentType = "claude"
	// AgentTypeCursor targets Cursor (.cursor/rules).
	AgentTypeCursor AgentType = "cursor"
	// AgentTypeGeneric targets generic AGENTS.md (OpenAI Codex, Gemini CLI, etc.).
	AgentTypeGeneric AgentType = "generic"
	// AgentTypeAll generates context files for all supported agent types.
	AgentTypeAll AgentType = "all"
)

// AgentContextData is the data passed to agent context templates when rendering.
// It is a superset of TemplateData enriched with agent-specific metadata.
type AgentContextData struct {
	// UpstreamPort is the port of the protected application.
	UpstreamPort int

	// AuthEnabled indicates whether authentication is configured.
	AuthEnabled bool

	// RateLimitEnabled indicates whether rate limiting is configured.
	RateLimitEnabled bool

	// TLSEnabled indicates whether TLS is configured.
	TLSEnabled bool

	// RateLimitRPS is the configured requests-per-second limit.
	// Only meaningful when RateLimitEnabled is true.
	RateLimitRPS int

	// AdminEnabled indicates whether the admin API is enabled.
	AdminEnabled bool
}
