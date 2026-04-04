package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	opsapp "github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// RegisterDefaultTools registers the four standard VibeWarden MCP tools onto s.
// It is a convenience function that wires the tool definitions and handlers together.
func RegisterDefaultTools(s *Server) {
	s.RegisterTool(statusToolDef(), handleStatus)
	s.RegisterTool(doctorToolDef(), handleDoctor)
	s.RegisterTool(validateToolDef(), handleValidate)
	s.RegisterTool(explainToolDef(), handleExplain)
}

// ---------------------------------------------------------------------------
// vibewarden_status
// ---------------------------------------------------------------------------

func statusToolDef() ToolDefinition {
	return ToolDefinition{
		Name:        "vibewarden_status",
		Description: "Check whether the VibeWarden sidecar is running by calling its health endpoint. Returns the HTTP status and whether the sidecar is healthy.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"config": {
					Type:        "string",
					Description: "Path to vibewarden.yaml (default: ./vibewarden.yaml)",
				},
			},
		},
	}
}

// statusArgs are the optional arguments for vibewarden_status.
type statusArgs struct {
	Config string `json:"config"`
}

func handleStatus(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args statusArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	cfg, err := config.Load(args.Config)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	port := cfg.Server.Port
	if port == 0 {
		port = 8443
	}
	healthURL := fmt.Sprintf("%s://localhost:%d/_vibewarden/health", scheme, port)

	client := &http.Client{Timeout: 5 * time.Second}
	checker := opsadapter.NewHTTPHealthChecker(client)

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ok, code, err := checker.CheckHealth(checkCtx, healthURL)
	if err != nil {
		return text(fmt.Sprintf("sidecar unreachable at %s: %v", healthURL, err)), nil
	}

	if !ok {
		return text(fmt.Sprintf("sidecar returned HTTP %d at %s — not healthy", code, healthURL)), nil
	}

	return text(fmt.Sprintf("sidecar is running and healthy at %s (HTTP %d)", healthURL, code)), nil
}

// ---------------------------------------------------------------------------
// vibewarden_doctor
// ---------------------------------------------------------------------------

func doctorToolDef() ToolDefinition {
	return ToolDefinition{
		Name:        "vibewarden_doctor",
		Description: "Run the full VibeWarden health-check suite (config parsing, Docker availability, required ports, generated files, container health). Returns a structured JSON report.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"config": {
					Type:        "string",
					Description: "Path to vibewarden.yaml (default: ./vibewarden.yaml)",
				},
			},
		},
	}
}

// doctorArgs are the optional arguments for vibewarden_doctor.
type doctorArgs struct {
	Config string `json:"config"`
}

func handleDoctor(ctx context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args doctorArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	cfg, loadErr := config.Load(args.Config)
	if loadErr != nil {
		cfg = &config.Config{}
	}

	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	compose := opsadapter.NewComposeAdapter()
	portChecker := opsadapter.NewNetPortChecker()
	httpClient := &http.Client{Timeout: 5 * time.Second}
	healthChecker := opsadapter.NewHTTPHealthChecker(httpClient)
	svc := opsapp.NewDoctorService(compose, portChecker, healthChecker)

	label := args.Config
	if label == "" {
		label = "vibewarden.yaml"
	}

	opts := opsapp.DoctorOptions{
		ConfigPath: label,
		WorkDir:    workDir,
		JSON:       true,
	}

	var buf bytes.Buffer
	_, runErr := svc.Run(ctx, cfg, opts, &buf)
	if runErr != nil {
		return nil, fmt.Errorf("running doctor: %w", runErr)
	}

	out := buf.String()
	if loadErr != nil {
		out = fmt.Sprintf("warning: could not load config (%v)\n\n%s", loadErr, out)
	}

	return text(out), nil
}

// ---------------------------------------------------------------------------
// vibewarden_validate
// ---------------------------------------------------------------------------

func validateToolDef() ToolDefinition {
	return ToolDefinition{
		Name:        "vibewarden_validate",
		Description: "Validate a vibewarden.yaml configuration file. Returns a list of validation errors, or confirms the configuration is valid.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Path to vibewarden.yaml to validate (default: ./vibewarden.yaml)",
				},
			},
		},
	}
}

// validateArgs are the optional arguments for vibewarden_validate.
type validateArgs struct {
	Path string `json:"path"`
}

func handleValidate(_ context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args validateArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	displayPath := args.Path
	if displayPath == "" {
		displayPath = "vibewarden.yaml"
	}

	cfg, err := config.Load(args.Path)
	if err != nil {
		return text(fmt.Sprintf("Configuration invalid (%s): %v", displayPath, err)), nil
	}

	errs := validateConfig(cfg)
	if len(errs) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "Configuration invalid (%s): %d error(s)\n", displayPath, len(errs))
		for _, e := range errs {
			fmt.Fprintf(&sb, "  - %s\n", e)
		}
		return text(sb.String()), nil
	}

	return text(fmt.Sprintf("Configuration valid (%s)", displayPath)), nil
}

// ---------------------------------------------------------------------------
// vibewarden_explain
// ---------------------------------------------------------------------------

func explainToolDef() ToolDefinition {
	return ToolDefinition{
		Name:        "vibewarden_explain",
		Description: "Explain what a VibeWarden configuration does in plain language. Describe which plugins are enabled, security settings, TLS provider, rate limiting, and any notable options.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {
					Type:        "string",
					Description: "Path to vibewarden.yaml to explain (default: ./vibewarden.yaml)",
				},
			},
		},
	}
}

// explainArgs are the optional arguments for vibewarden_explain.
type explainArgs struct {
	Path string `json:"path"`
}

func handleExplain(_ context.Context, params json.RawMessage) ([]ContentItem, error) {
	var args explainArgs
	if len(params) > 0 {
		if err := json.Unmarshal(params, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments: %w", err)
		}
	}

	displayPath := args.Path
	if displayPath == "" {
		displayPath = "vibewarden.yaml"
	}

	cfg, err := config.Load(args.Path)
	if err != nil {
		return nil, fmt.Errorf("loading config %s: %w", displayPath, err)
	}

	return text(explainConfig(cfg, displayPath)), nil
}

// explainConfig generates a plain-language description of the configuration.
func explainConfig(cfg *config.Config, path string) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Configuration file: %s\n\n", path)

	// Profile
	profile := cfg.Profile
	if profile == "" {
		profile = "dev (default)"
	}
	fmt.Fprintf(&sb, "Profile: %s\n\n", profile)

	// Server / upstream
	fmt.Fprintf(&sb, "Network:\n")
	fmt.Fprintf(&sb, "  - Sidecar listens on port %d\n", cfg.Server.Port)
	fmt.Fprintf(&sb, "  - Forwards traffic to upstream on port %d\n\n", cfg.Upstream.Port)

	// TLS
	fmt.Fprintf(&sb, "TLS:\n")
	if cfg.TLS.Enabled {
		fmt.Fprintf(&sb, "  - Enabled (provider: %s", cfg.TLS.Provider)
		if cfg.TLS.Domain != "" {
			fmt.Fprintf(&sb, ", domain: %s", cfg.TLS.Domain)
		}
		fmt.Fprintf(&sb, ")\n")
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Authentication
	fmt.Fprintf(&sb, "Authentication:\n")
	if cfg.Auth.Enabled {
		kratosURL := cfg.Kratos.PublicURL
		if kratosURL == "" {
			kratosURL = "http://localhost:4433 (default)"
		}
		fmt.Fprintf(&sb, "  - Enabled via Ory Kratos (%s)\n", kratosURL)
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Rate limiting
	fmt.Fprintf(&sb, "Rate limiting:\n")
	if cfg.RateLimit.Enabled {
		fmt.Fprintf(&sb, "  - Enabled: %.0f requests/second per IP, burst up to %d\n",
			cfg.RateLimit.PerIP.RequestsPerSecond, cfg.RateLimit.PerIP.Burst)
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Security headers
	fmt.Fprintf(&sb, "Security headers:\n")
	if cfg.SecurityHeaders.Enabled {
		fmt.Fprintf(&sb, "  - Enabled\n")
		if cfg.SecurityHeaders.HSTSMaxAge > 0 {
			fmt.Fprintf(&sb, "  - HSTS max-age: %d seconds\n", cfg.SecurityHeaders.HSTSMaxAge)
		}
		if cfg.SecurityHeaders.FrameOption != "" {
			fmt.Fprintf(&sb, "  - X-Frame-Options: %s\n", cfg.SecurityHeaders.FrameOption)
		}
		if cfg.SecurityHeaders.ContentTypeNosniff {
			fmt.Fprintf(&sb, "  - X-Content-Type-Options: nosniff\n")
		}
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Admin
	fmt.Fprintf(&sb, "Admin API:\n")
	if cfg.Admin.Enabled {
		fmt.Fprintf(&sb, "  - Enabled (user management)\n")
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Logging
	fmt.Fprintf(&sb, "Logging:\n")
	level := cfg.Log.Level
	if level == "" {
		level = "info (default)"
	}
	format := cfg.Log.Format
	if format == "" {
		format = "json (default)"
	}
	fmt.Fprintf(&sb, "  - Level: %s, format: %s\n\n", level, format)

	// CORS
	fmt.Fprintf(&sb, "CORS:\n")
	if cfg.CORS.Enabled {
		fmt.Fprintf(&sb, "  - Enabled\n")
		if len(cfg.CORS.AllowedOrigins) > 0 {
			fmt.Fprintf(&sb, "  - Allowed origins: %s\n", strings.Join(cfg.CORS.AllowedOrigins, ", "))
		}
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}
	fmt.Fprintln(&sb)

	// Metrics / telemetry
	fmt.Fprintf(&sb, "Metrics:\n")
	if cfg.Metrics.Enabled || cfg.Telemetry.Prometheus.Enabled {
		fmt.Fprintf(&sb, "  - Prometheus metrics enabled\n")
	} else {
		fmt.Fprintf(&sb, "  - Disabled\n")
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// text wraps a plain string in a ContentItem slice.
func text(s string) []ContentItem {
	return []ContentItem{{Type: "text", Text: s}}
}

// validateConfig re-uses the same validation logic as the validate CLI command.
// It is duplicated here to keep the mcp package self-contained without importing
// the cmd package (which would create a circular dependency).
func validateConfig(cfg *config.Config) []string {
	var errs []string

	validTLSProviders := map[string]bool{
		"letsencrypt": true,
		"self-signed": true,
		"external":    true,
	}
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	validLogFormats := map[string]bool{
		"json": true,
		"text": true,
	}
	validFrameOptions := map[string]bool{
		"":           true,
		"DENY":       true,
		"SAMEORIGIN": true,
	}

	if cfg.Server.Port < 1 || cfg.Server.Port > 65535 {
		errs = append(errs, fmt.Sprintf("server.port must be between 1 and 65535, got %d", cfg.Server.Port))
	}
	if cfg.Upstream.Port < 1 || cfg.Upstream.Port > 65535 {
		errs = append(errs, fmt.Sprintf("upstream.port must be between 1 and 65535, got %d", cfg.Upstream.Port))
	}
	if !validTLSProviders[cfg.TLS.Provider] {
		errs = append(errs, fmt.Sprintf("tls.provider must be one of letsencrypt, self-signed, external; got %q", cfg.TLS.Provider))
	}
	if cfg.TLS.Enabled && cfg.TLS.Provider == "letsencrypt" && cfg.TLS.Domain == "" {
		errs = append(errs, "tls.domain is required when tls.provider is letsencrypt")
	}
	if cfg.TLS.Enabled && cfg.TLS.Provider == "external" {
		if cfg.TLS.CertPath == "" {
			errs = append(errs, "tls.cert_path is required when tls.provider is external")
		}
		if cfg.TLS.KeyPath == "" {
			errs = append(errs, "tls.key_path is required when tls.provider is external")
		}
	}
	if !validLogLevels[cfg.Log.Level] {
		errs = append(errs, fmt.Sprintf("log.level must be one of debug, info, warn, error; got %q", cfg.Log.Level))
	}
	if !validLogFormats[cfg.Log.Format] {
		errs = append(errs, fmt.Sprintf("log.format must be one of json, text; got %q", cfg.Log.Format))
	}
	if cfg.Admin.Enabled && cfg.Admin.Token == "" {
		errs = append(errs, "admin.token is required when admin.enabled is true (run: vibew secret generate --admin-token)")
	}
	if !validFrameOptions[cfg.SecurityHeaders.FrameOption] {
		errs = append(errs, fmt.Sprintf("security_headers.frame_option must be DENY, SAMEORIGIN, or empty; got %q", cfg.SecurityHeaders.FrameOption))
	}
	if cfg.RateLimit.Enabled {
		if cfg.RateLimit.PerIP.RequestsPerSecond <= 0 {
			errs = append(errs, "rate_limit.per_ip.requests_per_second must be greater than zero")
		}
		if cfg.RateLimit.PerIP.Burst <= 0 {
			errs = append(errs, "rate_limit.per_ip.burst must be greater than zero")
		}
	}
	if cfg.Admin.Enabled && !cfg.Auth.Enabled {
		errs = append(errs, "user-management plugin requires auth to be enabled (set auth.enabled: true)")
	}

	return errs
}
