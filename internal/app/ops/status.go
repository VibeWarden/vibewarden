package ops

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ComponentStatus represents the three-state health of a single component.
// The State field replaces the legacy boolean Healthy field (ADR-095).
type ComponentStatus struct {
	// Name is a human-readable component label.
	Name string
	// State is the tri-state rendering tag: StatusOK, StatusOFF, or StatusFAIL.
	State StatusState
	// Detail is an optional extra detail line (e.g. provider, URL, reason).
	Detail string
}

// StatusService orchestrates the "vibew status" use case.
// It queries each component and returns a structured summary.
// When a ComposeRunner is wired, it provides additional diagnostic details
// when the proxy is unreachable (container state, log snippets).
type StatusService struct {
	health   ports.HealthChecker
	compose  ports.ComposeRunner    // optional; nil disables container diagnostics
	logs     ports.ComposeLogs      // optional; nil disables log-based diagnostics
	tlsState ports.TLSStateResolver // optional; nil falls back to config-only rendering
}

// NewStatusService creates a new StatusService.
func NewStatusService(health ports.HealthChecker) *StatusService {
	return &StatusService{health: health}
}

// WithCompose returns a copy of the StatusService with the given ComposeRunner
// wired for diagnostic container status checks.
func (s *StatusService) WithCompose(compose ports.ComposeRunner) *StatusService {
	cp := *s
	cp.compose = compose
	return &cp
}

// WithLogs returns a copy of the StatusService with the given ComposeLogs
// wired for diagnostic log tail checks.
func (s *StatusService) WithLogs(logs ports.ComposeLogs) *StatusService {
	cp := *s
	cp.logs = logs
	return &cp
}

// WithTLSStateResolver returns a copy of the StatusService with the given TLS
// state resolver wired to render the TLS row with state-aware detail
// (obtained/obtaining/self-signed/...). When nil, the TLS row falls back to
// the legacy config-only rendering.
func (s *StatusService) WithTLSStateResolver(r ports.TLSStateResolver) *StatusService {
	cp := *s
	cp.tlsState = r
	return &cp
}

// Run queries all components and writes the status dashboard to out.
func (s *StatusService) Run(ctx context.Context, cfg *config.Config, out io.Writer) error {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8443
	}
	proxyBase := fmt.Sprintf("%s://localhost:%d", scheme, proxyPort)

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	statuses := s.gatherStatuses(checkCtx, cfg, proxyBase)
	pluginStatuses := gatherPluginStatuses(cfg)
	printStatusTable(statuses, pluginStatuses, out)

	// When the proxy is unreachable, print additional diagnostic details.
	for _, st := range statuses {
		if st.Name == "Proxy" && st.State == StatusFAIL {
			s.diagnoseProxy(checkCtx, cfg, out)
			break
		}
	}

	return nil
}

// gatherStatuses collects the health of each component.
func (s *StatusService) gatherStatuses(ctx context.Context, cfg *config.Config, proxyBase string) []ComponentStatus {
	var statuses []ComponentStatus

	// Proxy health — always-on infrastructure; always probed.
	statuses = append(statuses, s.checkHTTP(ctx, "Proxy", proxyBase+"/_vibewarden/health", proxyBase))

	// Auth (Kratos) — gated on cfg.Auth.Active(). When auth is disabled in
	// config, return StatusOFF immediately without any HTTP call (ADR-095).
	kratosURL := cfg.Kratos.AdminURL
	if kratosURL == "" {
		kratosURL = "http://127.0.0.1:4434"
	}
	if cfg.Auth.Active() {
		// The bundled config addresses Kratos by its compose service name,
		// which is unreachable from the host. Probe the published host port
		// instead so a healthy stack is not reported as FAIL (#1337).
		probeBase, rewrittenFrom := hostKratosAdminURL(kratosURL)
		detail := probeBase
		if rewrittenFrom != "" {
			detail = fmt.Sprintf("%s (published port for container-internal %s)", probeBase, rewrittenFrom)
		}
		statuses = append(statuses, s.checkHTTP(ctx, "Auth (Kratos)", probeBase+"/admin/health/ready", detail))
	} else {
		statuses = append(statuses, ComponentStatus{
			Name:   "Auth (Kratos)",
			State:  StatusOFF,
			Detail: "auth disabled",
		})
	}

	// Rate limit — config only, no HTTP check. Always StatusOK.
	rlStatus := ComponentStatus{
		Name:   "Rate Limit",
		State:  StatusOK,
		Detail: "disabled",
	}
	if cfg.RateLimit.Enabled {
		rlStatus.Detail = fmt.Sprintf("enabled (%.0f req/s per IP)", cfg.RateLimit.PerIP.RequestsPerSecond)
	}
	statuses = append(statuses, rlStatus)

	// Metrics — gated on cfg.Metrics.Enabled.
	if cfg.Metrics.Enabled {
		statuses = append(statuses, s.checkHTTP(ctx, "Metrics", proxyBase+"/_vibewarden/metrics", proxyBase))
	} else {
		statuses = append(statuses, ComponentStatus{
			Name:   "Metrics",
			State:  StatusOFF,
			Detail: "disabled",
		})
	}

	// TLS — prefer the state-aware resolver when wired. The renderer
	// produces a StatusState plus the canonical detail string.
	// When no resolver is wired we fall back to the legacy config-only detail.
	statuses = append(statuses, s.tlsComponentStatus(ctx, cfg))

	return statuses
}

// tlsComponentStatus builds the TLS row for the status dashboard. When a
// TLS state resolver is wired it produces state-aware output (ADR-095);
// otherwise it falls back to the pre-#1090 config-only detail.
func (s *StatusService) tlsComponentStatus(ctx context.Context, cfg *config.Config) ComponentStatus {
	if s.tlsState != nil {
		state, err := s.tlsState.Resolve(ctx)
		if err == nil {
			detail, status := renderTLSStatusLine(state)
			return ComponentStatus{Name: "TLS", State: status, Detail: detail}
		}
		// On resolver error, fall through to config-only detail so we
		// never crash the status dashboard.
	}

	detail := fmt.Sprintf("disabled — provider: %s", cfg.TLS.Provider)
	if cfg.TLS.Enabled {
		domain := cfg.TLS.Domain
		if domain == "" {
			domain = "self-signed"
		}
		detail = fmt.Sprintf("enabled — provider: %s, domain: %s", cfg.TLS.Provider, domain)
	}
	return ComponentStatus{Name: "TLS", State: StatusOK, Detail: detail}
}

// PluginStatus represents the enabled/disabled state of a single plugin
// as reported from the current configuration.
type PluginStatus struct {
	// Name is the canonical plugin identifier (e.g. "tls").
	Name string
	// Enabled is true when the plugin is enabled in the config.
	Enabled bool
	// Detail is an optional extra detail line shown in the status output.
	Detail string
}

// gatherPluginStatuses builds a slice of PluginStatus from cfg.
// Status is derived from config only — no live HTTP checks are made.
func gatherPluginStatuses(cfg *config.Config) []PluginStatus {
	var ps []PluginStatus

	// TLS
	tlsDetail := fmt.Sprintf("provider: %s", cfg.TLS.Provider)
	if cfg.TLS.Enabled && cfg.TLS.Domain != "" {
		tlsDetail = fmt.Sprintf("provider: %s, domain: %s", cfg.TLS.Provider, cfg.TLS.Domain)
	}
	ps = append(ps, PluginStatus{Name: "tls", Enabled: cfg.TLS.Enabled, Detail: tlsDetail})

	// Security headers
	ps = append(ps, PluginStatus{Name: "security-headers", Enabled: cfg.SecurityHeaders.Enabled})

	// Rate limiting
	rlDetail := ""
	if cfg.RateLimit.Enabled {
		rlDetail = fmt.Sprintf("store: memory, %.0f req/s per IP", cfg.RateLimit.PerIP.RequestsPerSecond)
	}
	ps = append(ps, PluginStatus{Name: "rate-limiting", Enabled: cfg.RateLimit.Enabled, Detail: rlDetail})

	// Auth
	authActive := cfg.Auth.Active()
	authDetail := ""
	if authActive {
		authDetail = fmt.Sprintf("kratos: %s", cfg.Kratos.PublicURL)
	}
	ps = append(ps, PluginStatus{Name: "auth", Enabled: authActive, Detail: authDetail})

	// Metrics
	ps = append(ps, PluginStatus{Name: "metrics", Enabled: cfg.Metrics.Enabled})

	// User management
	ps = append(ps, PluginStatus{Name: "user-management", Enabled: cfg.Admin.Enabled})

	// WAF
	wafDetail := ""
	if cfg.WAF.Enabled {
		wafDetail = fmt.Sprintf("mode: %s", cfg.WAF.Mode)
	}
	ps = append(ps, PluginStatus{Name: "waf", Enabled: cfg.WAF.Enabled, Detail: wafDetail})

	// CORS
	ps = append(ps, PluginStatus{Name: "cors", Enabled: cfg.CORS.Enabled})

	// Egress
	ps = append(ps, PluginStatus{Name: "egress", Enabled: cfg.Egress.Enabled})

	// Compression
	ps = append(ps, PluginStatus{Name: "compression", Enabled: cfg.Compression.Enabled})

	return ps
}

// checkHTTP performs a health check against url and returns a ComponentStatus.
// On success it returns StatusOK; on failure StatusFAIL.
func (s *StatusService) checkHTTP(ctx context.Context, name, url, base string) ComponentStatus {
	ok, code, err := s.health.CheckHealth(ctx, url)
	if err != nil {
		return ComponentStatus{
			Name:   name,
			State:  StatusFAIL,
			Detail: fmt.Sprintf("unreachable (%s)", base),
		}
	}
	if !ok {
		return ComponentStatus{
			Name:   name,
			State:  StatusFAIL,
			Detail: fmt.Sprintf("HTTP %d (%s)", code, base),
		}
	}
	return ComponentStatus{
		Name:   name,
		State:  StatusOK,
		Detail: base,
	}
}

// diagnoseProxy prints additional diagnostic details when the proxy is
// unreachable. It checks whether the sidecar container is running and
// inspects recent log lines for common error patterns.
func (s *StatusService) diagnoseProxy(ctx context.Context, cfg *config.Config, out io.Writer) {
	composeFile := filepath.Join(generatedOutputDir, "docker-compose.yml")

	// Step 1: check container state via docker compose ps.
	if s.compose != nil {
		containers, err := s.compose.PS(ctx, composeFile)
		if err == nil {
			sidecarRunning := false
			for _, c := range containers {
				if c.Service == "vibewarden" || c.Service == "sidecar" || c.Service == "proxy" {
					if c.State == "running" {
						sidecarRunning = true
					}
					break
				}
			}

			if len(containers) == 0 || !sidecarRunning {
				fmt.Fprintln(out, "  Diagnosis: Sidecar container is not running -- run 'vibew dev' to start")
				fmt.Fprintln(out, "  Suggestion: Run 'vibew doctor' for detailed diagnostics")
				return
			}
		}
	}

	// Step 2: check sidecar logs for ACME/TLS errors.
	if s.logs != nil {
		logOutput, err := s.logs.Tail(ctx, composeFile, "vibewarden", 20)
		if err == nil && logOutput != "" {
			lower := strings.ToLower(logOutput)
			if strings.Contains(lower, "acme") || strings.Contains(lower, "challenge") || strings.Contains(lower, "tls") {
				fmt.Fprintln(out, "  Diagnosis: Recent sidecar logs contain TLS/ACME errors")
			}
		}
	}

	// Step 3: letsencrypt + localhost is a known misconfiguration.
	if cfg.TLS.Enabled && cfg.TLS.Provider == "letsencrypt" {
		fmt.Fprintln(out, "  Diagnosis: tls.provider is 'letsencrypt' -- ACME HTTP-01 challenges require a")
		fmt.Fprintln(out, "  publicly reachable server. Use tls.provider: self-signed for local dev.")
	}

	fmt.Fprintln(out, "  Suggestion: Run 'vibew doctor' for detailed diagnostics")
}

// printStatusTable renders the component and plugin statuses as a table.
// Component rows use coloured text labels (OK / OFF / FAIL); the plugins
// sub-table is unchanged (enabled/disabled with glyphs).
func printStatusTable(statuses []ComponentStatus, pluginStatuses []PluginStatus, out io.Writer) {
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "VibeWarden Status")
	fmt.Fprintln(out, "─────────────────────────────────────────")
	fmt.Fprintln(out, "States: OK = healthy   OFF = disabled   FAIL = check failed")
	fmt.Fprintln(out, "")

	for _, s := range statuses {
		label := s.State.coloredLabel()
		if s.Detail != "" {
			fmt.Fprintf(out, "  %s  %-20s  %s\n", label, s.Name, s.Detail)
		} else {
			fmt.Fprintf(out, "  %s  %s\n", label, s.Name)
		}
	}

	if len(pluginStatuses) > 0 {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Plugins")
		fmt.Fprintln(out, "─────────────────────────────────────────")
		for _, p := range pluginStatuses {
			mark := cyan("-")
			statusStr := "disabled"
			if p.Enabled {
				mark = green("✓")
				statusStr = "enabled"
			}
			line := fmt.Sprintf("  %s  %-20s  %s", mark, p.Name, statusStr)
			if p.Detail != "" {
				line += fmt.Sprintf("  (%s)", p.Detail)
			}
			fmt.Fprintln(out, line)
		}
	}

	fmt.Fprintln(out, "")
}
