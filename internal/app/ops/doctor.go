package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fatih/color"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Severity classifies the outcome of a single doctor check.
type Severity string

const (
	// SeverityOK means the check passed.
	SeverityOK Severity = "OK"
	// SeverityWarn means the check found something worth noting but not critical.
	SeverityWarn Severity = "WARN"
	// SeverityFail means the check found a critical problem.
	SeverityFail Severity = "FAIL"
)

// CheckResult holds the result of a single doctor check.
type CheckResult struct {
	// Name is a short human-readable label for the check.
	Name string `json:"name"`
	// Severity is the outcome classification: OK, WARN, or FAIL.
	Severity Severity `json:"severity"`
	// Detail is an optional explanation (shown on success and failure).
	Detail string `json:"detail,omitempty"`
}

// OK returns true when the check severity is OK.
func (c CheckResult) OK() bool { return c.Severity == SeverityOK }

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

// DoctorOptions controls how Run behaves.
type DoctorOptions struct {
	// ConfigPath is the path to the vibewarden.yaml file (used in the report label).
	ConfigPath string
	// WorkDir is the working directory used to resolve relative paths such as
	// .vibewarden/generated/docker-compose.yml.  Defaults to the current directory.
	WorkDir string
	// JSON requests machine-readable JSON output instead of the human-readable table.
	JSON bool
}

// Run executes all diagnostics and writes the report to out.
// It never returns an error just because individual checks fail; the exit
// behaviour is determined by the CLI command inspecting the results.
func (s *DoctorService) Run(ctx context.Context, cfg *config.Config, opts DoctorOptions, out io.Writer) (allOK bool, err error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "."
	}

	checks := s.runChecks(ctx, cfg, opts.ConfigPath, workDir)

	if opts.JSON {
		if err := printDoctorJSON(checks, out); err != nil {
			return false, fmt.Errorf("encoding JSON output: %w", err)
		}
	} else {
		printDoctorReport(checks, out)
	}

	allOK = true
	for _, c := range checks {
		if c.Severity == SeverityFail {
			allOK = false
			break
		}
	}
	return allOK, nil
}

// runChecks executes every diagnostic check and returns the aggregated results.
func (s *DoctorService) runChecks(ctx context.Context, cfg *config.Config, configPath, workDir string) []CheckResult {
	var results []CheckResult

	// 1. vibewarden.yaml present and valid
	results = append(results, checkConfigFile(cfg, configPath))

	// 2. Is Docker running?
	results = append(results, s.checkDockerRunning(ctx))

	// 3. Is Docker Compose v2 available?
	results = append(results, s.checkDockerCompose(ctx))

	// 4. Required ports available
	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8080
	}
	proxyHost := cfg.Server.Host
	if proxyHost == "" {
		proxyHost = "127.0.0.1"
	}
	results = append(results, s.checkPort(ctx, "Proxy port", proxyHost, proxyPort))

	// 5. Generated files present
	generatedCompose := filepath.Join(workDir, ".vibewarden", "generated", "docker-compose.yml")
	results = append(results, checkGeneratedFiles(generatedCompose))

	// 6. If stack is running: container health
	results = append(results, s.checkContainerHealth(ctx, generatedCompose))

	return results
}

func (s *DoctorService) checkDockerRunning(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := s.compose.Info(checkCtx); err != nil {
		return CheckResult{
			Name:     "Docker daemon",
			Severity: SeverityFail,
			Detail:   "not running — start Docker Desktop or the Docker service",
		}
	}
	return CheckResult{
		Name:     "Docker daemon",
		Severity: SeverityOK,
		Detail:   "running",
	}
}

func (s *DoctorService) checkDockerCompose(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	version, err := s.compose.Version(checkCtx)
	if err != nil {
		return CheckResult{
			Name:     "Docker Compose",
			Severity: SeverityFail,
			Detail:   "not available — install Docker Compose v2",
		}
	}
	return CheckResult{
		Name:     "Docker Compose",
		Severity: SeverityOK,
		Detail:   sanitizeOneLine(version),
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
			Name:     "Config file",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("%s not found or invalid", label),
		}
	}
	return CheckResult{
		Name:     "Config file",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("%s — valid", label),
	}
}

func (s *DoctorService) checkPort(ctx context.Context, label, host string, port int) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	available, err := s.portChecker.IsPortAvailable(checkCtx, host, port)
	if err != nil {
		return CheckResult{
			Name:     label,
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("port %d check failed: %v", port, err),
		}
	}
	if !available {
		return CheckResult{
			Name:     label,
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("port %d is already in use", port),
		}
	}
	return CheckResult{
		Name:     label,
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("port %d is available", port),
	}
}

// checkGeneratedFiles verifies that the generated docker-compose.yml exists.
func checkGeneratedFiles(composePath string) CheckResult {
	_, err := os.Stat(composePath)
	if err != nil {
		return CheckResult{
			Name:     "Generated files",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("%s not found — run 'vibewarden generate' first", composePath),
		}
	}
	return CheckResult{
		Name:     "Generated files",
		Severity: SeverityOK,
		Detail:   composePath,
	}
}

// checkContainerHealth runs "docker compose ps" and reports the health of each
// container.  When no containers are running it is treated as a warning.
func (s *DoctorService) checkContainerHealth(ctx context.Context, composePath string) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	containers, err := s.compose.PS(checkCtx, composePath)
	if err != nil {
		// PS failing is not catastrophic — the stack may not have been started.
		return CheckResult{
			Name:     "Container health",
			Severity: SeverityWarn,
			Detail:   "could not query containers — stack may not be running",
		}
	}
	if len(containers) == 0 {
		return CheckResult{
			Name:     "Container health",
			Severity: SeverityWarn,
			Detail:   "no containers found — run 'vibewarden dev' to start the stack",
		}
	}

	var unhealthy []string
	for _, c := range containers {
		if c.State != "running" || (c.Health != "" && c.Health != "healthy") {
			unhealthy = append(unhealthy, fmt.Sprintf("%s (%s/%s)", c.Service, c.State, c.Health))
		}
	}
	if len(unhealthy) > 0 {
		return CheckResult{
			Name:     "Container health",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("unhealthy containers: %v", unhealthy),
		}
	}
	return CheckResult{
		Name:     "Container health",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("%d container(s) running", len(containers)),
	}
}

// printDoctorReport renders the check results to out using ANSI colour codes.
func printDoctorReport(results []CheckResult, out io.Writer) {
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "VibeWarden Doctor")
	fmt.Fprintln(out, "─────────────────────────────────────────")

	for _, r := range results {
		var badge string
		switch r.Severity {
		case SeverityOK:
			badge = green("[OK]")
		case SeverityWarn:
			badge = yellow("[WARN]")
		default:
			badge = red("[FAIL]")
		}
		if r.Detail != "" {
			fmt.Fprintf(out, "  %-14s  %-22s  %s\n", badge, r.Name, r.Detail)
		} else {
			fmt.Fprintf(out, "  %-14s  %s\n", badge, r.Name)
		}
	}
	fmt.Fprintln(out, "")
}

// printDoctorJSON encodes results as a JSON array to out.
func printDoctorJSON(results []CheckResult, out io.Writer) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
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
