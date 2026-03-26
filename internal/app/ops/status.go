package ops

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/fatih/color"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ComponentStatus represents the health of a single component.
type ComponentStatus struct {
	// Name is a human-readable component label.
	Name string
	// Healthy is true when the component is up and responding correctly.
	Healthy bool
	// Detail is an optional extra detail line (e.g. provider, URL, reason).
	Detail string
}

// StatusService orchestrates the "vibewarden status" use case.
// It queries each component and returns a structured summary.
type StatusService struct {
	health ports.HealthChecker
}

// NewStatusService creates a new StatusService.
func NewStatusService(health ports.HealthChecker) *StatusService {
	return &StatusService{health: health}
}

// Run queries all components and writes the status dashboard to out.
func (s *StatusService) Run(ctx context.Context, cfg *config.Config, out io.Writer) error {
	scheme := "http"
	if cfg.TLS.Enabled {
		scheme = "https"
	}
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8080
	}
	proxyBase := fmt.Sprintf("%s://localhost:%d", scheme, proxyPort)

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	statuses := s.gatherStatuses(checkCtx, cfg, proxyBase)
	printStatusTable(statuses, out)
	return nil
}

// gatherStatuses collects the health of each component.
func (s *StatusService) gatherStatuses(ctx context.Context, cfg *config.Config, proxyBase string) []ComponentStatus {
	var statuses []ComponentStatus

	// Proxy health
	statuses = append(statuses, s.checkHTTP(ctx, "Proxy", proxyBase+"/_vibewarden/health", proxyBase))

	// Auth (Kratos)
	kratosURL := cfg.Kratos.AdminURL
	if kratosURL == "" {
		kratosURL = "http://127.0.0.1:4434"
	}
	statuses = append(statuses, s.checkHTTP(ctx, "Auth (Kratos)", kratosURL+"/admin/health/ready", kratosURL))

	// Rate limit — config only, no HTTP check
	rlStatus := ComponentStatus{
		Name:    "Rate Limit",
		Healthy: true,
		Detail:  "disabled",
	}
	if cfg.RateLimit.Enabled {
		rlStatus.Detail = fmt.Sprintf("enabled (%.0f req/s per IP)", cfg.RateLimit.PerIP.RequestsPerSecond)
	}
	statuses = append(statuses, rlStatus)

	// Metrics
	if cfg.Metrics.Enabled {
		statuses = append(statuses, s.checkHTTP(ctx, "Metrics", proxyBase+"/_vibewarden/metrics", proxyBase))
	} else {
		statuses = append(statuses, ComponentStatus{
			Name:    "Metrics",
			Healthy: true,
			Detail:  "disabled",
		})
	}

	// TLS
	tlsDetail := fmt.Sprintf("disabled — provider: %s", cfg.TLS.Provider)
	if cfg.TLS.Enabled {
		domain := cfg.TLS.Domain
		if domain == "" {
			domain = "self-signed"
		}
		tlsDetail = fmt.Sprintf("enabled — provider: %s, domain: %s", cfg.TLS.Provider, domain)
	}
	statuses = append(statuses, ComponentStatus{
		Name:    "TLS",
		Healthy: true,
		Detail:  tlsDetail,
	})

	return statuses
}

// checkHTTP performs a health check against url and returns a ComponentStatus.
func (s *StatusService) checkHTTP(ctx context.Context, name, url, base string) ComponentStatus {
	ok, code, err := s.health.CheckHealth(ctx, url)
	if err != nil {
		return ComponentStatus{
			Name:    name,
			Healthy: false,
			Detail:  fmt.Sprintf("unreachable (%s)", base),
		}
	}
	if !ok {
		return ComponentStatus{
			Name:    name,
			Healthy: false,
			Detail:  fmt.Sprintf("HTTP %d (%s)", code, base),
		}
	}
	return ComponentStatus{
		Name:    name,
		Healthy: true,
		Detail:  base,
	}
}

// printStatusTable renders the component statuses as a table.
func printStatusTable(statuses []ComponentStatus, out io.Writer) {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "VibeWarden Status")
	fmt.Fprintln(out, "─────────────────────────────────────────")

	for _, s := range statuses {
		mark := green("✓")
		if !s.Healthy {
			mark = red("✗")
		}
		if s.Detail != "" {
			fmt.Fprintf(out, "  %s  %-20s  %s\n", mark, s.Name, s.Detail)
		} else {
			fmt.Fprintf(out, "  %s  %s\n", mark, s.Name)
		}
	}
	fmt.Fprintln(out, "")
}
