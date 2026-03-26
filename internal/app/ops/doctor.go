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

// CheckResult holds the result of a single doctor check.
type CheckResult struct {
	// Name is a short human-readable label for the check.
	Name string
	// OK is true when the check passed.
	OK bool
	// Detail is an optional explanation (shown on success and failure).
	Detail string
}

// DoctorService orchestrates the "vibewarden doctor" use case.
// Every check runs independently — a failing check does not stop subsequent ones.
type DoctorService struct {
	compose       ports.ComposeRunner
	portChecker   ports.PortChecker
	healthChecker ports.HealthChecker
}

// NewDoctorService creates a new DoctorService.
func NewDoctorService(compose ports.ComposeRunner, portChecker ports.PortChecker, healthChecker ports.HealthChecker) *DoctorService {
	return &DoctorService{
		compose:       compose,
		portChecker:   portChecker,
		healthChecker: healthChecker,
	}
}

// Run executes all diagnostics and writes the report to out.
// It never returns an error just because individual checks fail; the exit
// behaviour is determined by the CLI command inspecting the results.
func (s *DoctorService) Run(ctx context.Context, cfg *config.Config, configPath string, out io.Writer) (allOK bool, err error) {
	checks := s.runChecks(ctx, cfg, configPath)
	printDoctorReport(checks, out)

	allOK = true
	for _, c := range checks {
		if !c.OK {
			allOK = false
			break
		}
	}
	return allOK, nil
}

// runChecks executes every diagnostic check and returns the aggregated results.
func (s *DoctorService) runChecks(ctx context.Context, cfg *config.Config, configPath string) []CheckResult {
	var results []CheckResult

	// 1. Is Docker running?
	results = append(results, s.checkDockerRunning(ctx))

	// 2. Is Docker Compose available?
	results = append(results, s.checkDockerCompose(ctx))

	// 3. vibewarden.yaml present and valid
	results = append(results, checkConfigFile(cfg, configPath))

	// 4. Required ports available
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8080
	}
	results = append(results, s.checkPort(ctx, "Proxy port", cfg.Server.Host, proxyPort))

	// 5. Can reach upstream app?
	upstreamHost := cfg.Upstream.Host
	if upstreamHost == "" {
		upstreamHost = "127.0.0.1"
	}
	upstreamPort := cfg.Upstream.Port
	if upstreamPort == 0 {
		upstreamPort = 3000
	}
	results = append(results, s.checkUpstream(ctx, upstreamHost, upstreamPort))

	return results
}

func (s *DoctorService) checkDockerRunning(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.compose.Info(checkCtx); err != nil {
		return CheckResult{
			Name:   "Docker daemon",
			OK:     false,
			Detail: "not running — start Docker Desktop or the Docker service",
		}
	}
	return CheckResult{
		Name:   "Docker daemon",
		OK:     true,
		Detail: "running",
	}
}

func (s *DoctorService) checkDockerCompose(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	version, err := s.compose.Version(checkCtx)
	if err != nil {
		return CheckResult{
			Name:   "Docker Compose",
			OK:     false,
			Detail: "not available — install Docker Compose v2",
		}
	}
	return CheckResult{
		Name:   "Docker Compose",
		OK:     true,
		Detail: sanitizeOneLine(version),
	}
}

// checkConfigFile validates that a config was loaded (non-nil means valid).
func checkConfigFile(cfg *config.Config, configPath string) CheckResult {
	label := "vibewarden.yaml"
	if configPath != "" {
		label = configPath
	}

	if cfg == nil {
		return CheckResult{
			Name:   "Config file",
			OK:     false,
			Detail: fmt.Sprintf("%s not found or invalid", label),
		}
	}
	return CheckResult{
		Name:   "Config file",
		OK:     true,
		Detail: fmt.Sprintf("%s — valid", label),
	}
}

func (s *DoctorService) checkPort(ctx context.Context, label, host string, port int) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	available, err := s.portChecker.IsPortAvailable(checkCtx, host, port)
	if err != nil {
		return CheckResult{
			Name:   label,
			OK:     false,
			Detail: fmt.Sprintf("port %d check failed: %v", port, err),
		}
	}
	if !available {
		return CheckResult{
			Name:   label,
			OK:     false,
			Detail: fmt.Sprintf("port %d is already in use", port),
		}
	}
	return CheckResult{
		Name:   label,
		OK:     true,
		Detail: fmt.Sprintf("port %d is available", port),
	}
}

func (s *DoctorService) checkUpstream(ctx context.Context, host string, port int) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d", host, port)
	ok, statusCode, err := s.healthChecker.CheckHealth(checkCtx, url)
	if err != nil {
		return CheckResult{
			Name:   "Upstream app",
			OK:     false,
			Detail: fmt.Sprintf("not reachable at %s", url),
		}
	}

	return CheckResult{
		Name:   "Upstream app",
		OK:     ok,
		Detail: fmt.Sprintf("reachable at %s (HTTP %d)", url, statusCode),
	}
}

// printDoctorReport renders the check results to out.
func printDoctorReport(results []CheckResult, out io.Writer) {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "VibeWarden Doctor")
	fmt.Fprintln(out, "─────────────────────────────────────────")

	for _, r := range results {
		mark := green("✓")
		if !r.OK {
			mark = red("✗")
		}
		if r.Detail != "" {
			fmt.Fprintf(out, "  %s  %-22s  %s\n", mark, r.Name, r.Detail)
		} else {
			fmt.Fprintf(out, "  %s  %s\n", mark, r.Name)
		}
	}
	fmt.Fprintln(out, "")
}

// sanitizeOneLine trims a multiline string to its first non-empty line.
func sanitizeOneLine(s string) string {
	for i, c := range s {
		if c == '\n' || c == '\r' {
			return s[:i]
		}
	}
	return s
}
