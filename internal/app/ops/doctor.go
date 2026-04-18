package ops

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	// Section groups the check for display purposes (e.g. "Config & Docker",
	// "Local Runtime", "Production"). Empty for legacy checks.
	Section string `json:"section,omitempty"`
}

// OK returns true when the check severity is OK.
func (c CheckResult) OK() bool { return c.Severity == SeverityOK }

// DoctorService orchestrates the "vibew doctor" use case.
// Every check runs independently — a failing check does not stop subsequent ones.
type DoctorService struct {
	compose        ports.ComposeRunner
	portChecker    ports.PortChecker
	healthChecker  ports.HealthChecker
	remoteExecutor ports.RemoteExecutor
}

// NewDoctorService creates a new DoctorService.
func NewDoctorService(compose ports.ComposeRunner, portChecker ports.PortChecker, healthChecker ports.HealthChecker) *DoctorService {
	return &DoctorService{
		compose:       compose,
		portChecker:   portChecker,
		healthChecker: healthChecker,
	}
}

// WithRemoteExecutor returns a copy of the DoctorService with the given
// RemoteExecutor set for production checks. When nil, production checks are
// skipped.
func (s *DoctorService) WithRemoteExecutor(executor ports.RemoteExecutor) *DoctorService {
	cp := *s
	cp.remoteExecutor = executor
	return &cp
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
	// Target is the SSH target for production checks (e.g. "ssh://user@host").
	// When empty, production checks are skipped.
	Target string
}

// Run executes all diagnostics and writes the report to out.
// It never returns an error just because individual checks fail; the exit
// behaviour is determined by the CLI command inspecting the results.
func (s *DoctorService) Run(ctx context.Context, cfg *config.Config, opts DoctorOptions, out io.Writer) (allOK bool, err error) {
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "."
	}

	checks := s.runChecks(ctx, cfg, opts, workDir)

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

// sectionConfigDocker is the section header for config and Docker checks.
const sectionConfigDocker = "Config & Docker"

// sectionLocalRuntime is the section header for local runtime checks.
const sectionLocalRuntime = "Local Runtime"

// sectionProduction is the section header for production checks.
const sectionProduction = "Production"

// localTLSCertExpiryWarnDays is the number of days before expiry that triggers
// a warning for local TLS certificates.
const localTLSCertExpiryWarnDays = 7

// remoteTLSCertExpiryWarnDays is the number of days before expiry that triggers
// a warning for remote TLS certificates.
const remoteTLSCertExpiryWarnDays = 30

// runChecks executes every diagnostic check and returns the aggregated results.
func (s *DoctorService) runChecks(ctx context.Context, cfg *config.Config, opts DoctorOptions, workDir string) []CheckResult {
	var results []CheckResult

	// --- Layer 1: Config & Docker ---
	results = append(results, withSection(checkConfigFile(cfg, opts.ConfigPath), sectionConfigDocker))
	results = append(results, withSection(s.checkDockerRunning(ctx), sectionConfigDocker))
	results = append(results, withSection(s.checkDockerCompose(ctx), sectionConfigDocker))

	proxyPort := cfg.Server.Port
	if proxyPort == 0 {
		proxyPort = 8443
	}
	proxyHost := cfg.Server.Host
	if proxyHost == "" {
		proxyHost = "127.0.0.1"
	}
	results = append(results, withSection(s.checkPort(ctx, "Proxy port", proxyHost, proxyPort), sectionConfigDocker))

	generatedCompose := filepath.Join(workDir, ".vibewarden", "generated", "docker-compose.yml")
	results = append(results, withSection(checkGeneratedFiles(generatedCompose), sectionConfigDocker))
	results = append(results, withSection(s.checkContainerHealth(ctx, generatedCompose), sectionConfigDocker))

	// --- Layer 2: Local Runtime ---
	results = append(results, withSection(s.checkUpstreamReachable(ctx, cfg), sectionLocalRuntime))
	results = append(results, withSection(checkTLSCertValid(cfg, workDir), sectionLocalRuntime))

	// --- Layer 3: Production (only when target is set and executor is available) ---
	if opts.Target != "" && s.remoteExecutor != nil {
		results = append(results, withSection(s.checkSSHConnectivity(ctx), sectionProduction))
		results = append(results, withSection(s.checkRemoteContainerHealth(ctx), sectionProduction))
		if cfg.TLS.Domain != "" {
			results = append(results, withSection(checkDomainDNS(cfg.TLS.Domain, opts.Target), sectionProduction))
			results = append(results, withSection(checkRemoteTLSCert(cfg.TLS.Domain), sectionProduction))
		}
	}

	return results
}

// withSection returns a copy of the CheckResult with the Section field set.
func withSection(r CheckResult, section string) CheckResult {
	r.Section = section
	return r
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
			Detail:   fmt.Sprintf("%s not found — run 'vibew generate' first", composePath),
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
			Detail:   "no containers found — run 'vibew dev' to start the stack",
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

// checkUpstreamReachable verifies that the upstream application responds to
// HTTP health checks. This uses the HealthChecker port already injected into
// the service.
func (s *DoctorService) checkUpstreamReachable(ctx context.Context, cfg *config.Config) CheckResult {
	host := cfg.Upstream.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Upstream.Port
	if port == 0 {
		port = 3000
	}

	url := fmt.Sprintf("http://%s:%d", host, port)

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	ok, statusCode, err := s.healthChecker.CheckHealth(checkCtx, url)
	if err != nil {
		return CheckResult{
			Name:     "Upstream reachable",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("%s — unreachable (app may not be started yet)", url),
		}
	}
	if !ok {
		return CheckResult{
			Name:     "Upstream reachable",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("%s — responded with HTTP %d", url, statusCode),
		}
	}
	return CheckResult{
		Name:     "Upstream reachable",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("%s — HTTP %d", url, statusCode),
	}
}

// checkTLSCertValid checks the local self-signed TLS certificate for expiry.
// It only runs when the TLS provider is "self-signed" and cert files exist in
// the generated certs directory.
func checkTLSCertValid(cfg *config.Config, workDir string) CheckResult {
	if cfg.TLS.Provider != "self-signed" {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityOK,
			Detail:   fmt.Sprintf("provider is %q — skipping local cert check", cfg.TLS.Provider),
		}
	}

	certPath := filepath.Join(workDir, ".vibewarden", "generated", "certs", "server.crt")
	certPEM, err := os.ReadFile(certPath) //nolint:gosec // path is built from workDir + fixed relative path
	if err != nil {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("%s not found — run 'vibew generate' to create certs", certPath),
		}
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("%s — failed to decode PEM block", certPath),
		}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("%s — failed to parse certificate: %v", certPath, err),
		}
	}

	now := time.Now()
	if now.After(cert.NotAfter) {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("expired on %s", cert.NotAfter.Format(time.DateOnly)),
		}
	}

	daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)
	if daysUntilExpiry <= localTLSCertExpiryWarnDays {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("expires in %d day(s) on %s", daysUntilExpiry, cert.NotAfter.Format(time.DateOnly)),
		}
	}

	return CheckResult{
		Name:     "TLS certificate",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("valid until %s (%d days)", cert.NotAfter.Format(time.DateOnly), daysUntilExpiry),
	}
}

// checkSSHConnectivity tries to run a simple command on the remote host to
// verify SSH access.
func (s *DoctorService) checkSSHConnectivity(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := s.remoteExecutor.Run(checkCtx, "echo ok")
	if err != nil {
		return CheckResult{
			Name:     "SSH connectivity",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("could not connect: %v", err),
		}
	}
	return CheckResult{
		Name:     "SSH connectivity",
		Severity: SeverityOK,
		Detail:   "connected successfully",
	}
}

// checkRemoteContainerHealth runs "docker compose ps" on the remote host via
// SSH and reports the health of each container.
func (s *DoctorService) checkRemoteContainerHealth(ctx context.Context) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	output, err := s.remoteExecutor.Run(checkCtx, "docker compose ps --format json 2>/dev/null || docker-compose ps 2>/dev/null")
	if err != nil {
		return CheckResult{
			Name:     "Remote containers",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("could not query remote containers: %v", err),
		}
	}

	output = strings.TrimSpace(output)
	if output == "" {
		return CheckResult{
			Name:     "Remote containers",
			Severity: SeverityWarn,
			Detail:   "no containers found on remote host",
		}
	}

	// Try to parse JSON lines output (docker compose ps --format json).
	var unhealthy []string
	var total int
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var info struct {
			Service string `json:"Service"`
			State   string `json:"State"`
			Health  string `json:"Health"`
		}
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			// Not JSON — fallback to reporting raw output as OK.
			return CheckResult{
				Name:     "Remote containers",
				Severity: SeverityOK,
				Detail:   "containers found (non-JSON output)",
			}
		}
		total++
		if info.State != "running" || (info.Health != "" && info.Health != "healthy") {
			unhealthy = append(unhealthy, fmt.Sprintf("%s (%s/%s)", info.Service, info.State, info.Health))
		}
	}

	if len(unhealthy) > 0 {
		return CheckResult{
			Name:     "Remote containers",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("unhealthy containers: %v", unhealthy),
		}
	}
	return CheckResult{
		Name:     "Remote containers",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("%d container(s) running", total),
	}
}

// checkDomainDNS resolves the configured TLS domain and verifies it points to
// the target host IP. Uses net.LookupHost from stdlib.
func checkDomainDNS(domain, target string) CheckResult {
	addrs, err := net.LookupHost(domain)
	if err != nil {
		return CheckResult{
			Name:     "Domain DNS",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("could not resolve %s: %v", domain, err),
		}
	}

	if len(addrs) == 0 {
		return CheckResult{
			Name:     "Domain DNS",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("no DNS records found for %s", domain),
		}
	}

	// Extract the host from the SSH target URL (ssh://user@host[:port]).
	targetIP := extractHostFromTarget(target)

	// Resolve the target host in case it is a hostname rather than an IP.
	targetAddrs, err := net.LookupHost(targetIP)
	if err != nil {
		targetAddrs = []string{targetIP}
	}

	targetSet := make(map[string]bool, len(targetAddrs))
	for _, a := range targetAddrs {
		targetSet[a] = true
	}

	for _, a := range addrs {
		if targetSet[a] {
			return CheckResult{
				Name:     "Domain DNS",
				Severity: SeverityOK,
				Detail:   fmt.Sprintf("%s resolves to %s (matches target)", domain, a),
			}
		}
	}

	return CheckResult{
		Name:     "Domain DNS",
		Severity: SeverityWarn,
		Detail:   fmt.Sprintf("%s resolves to %s but target is %s", domain, strings.Join(addrs, ", "), targetIP),
	}
}

// extractHostFromTarget parses the host component from an ssh://user@host[:port]
// URL. If parsing fails it returns the raw input.
func extractHostFromTarget(raw string) string {
	// Try standard URL parsing: ssh://user@host:port
	if strings.Contains(raw, "://") {
		parts := strings.SplitN(raw, "://", 2)
		if len(parts) == 2 {
			after := parts[1]
			// Remove user@ prefix
			if idx := strings.Index(after, "@"); idx >= 0 {
				after = after[idx+1:]
			}
			// Remove :port suffix
			if host, _, err := net.SplitHostPort(after); err == nil {
				return host
			}
			return after
		}
	}
	return raw
}

// checkRemoteTLSCert connects to domain:443 and checks the certificate expiry.
func checkRemoteTLSCert(domain string) CheckResult {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 10 * time.Second},
		"tcp",
		domain+":443",
		&tls.Config{
			// We accept any cert — the purpose is to inspect expiry, not validate trust.
			InsecureSkipVerify: true, //nolint:gosec
		},
	)
	if err != nil {
		return CheckResult{
			Name:     "Remote TLS cert",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("could not connect to %s:443: %v", domain, err),
		}
	}
	defer conn.Close() //nolint:errcheck

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return CheckResult{
			Name:     "Remote TLS cert",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("no certificates presented by %s:443", domain),
		}
	}

	leaf := certs[0]
	now := time.Now()

	if now.After(leaf.NotAfter) {
		return CheckResult{
			Name:     "Remote TLS cert",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("expired on %s", leaf.NotAfter.Format(time.DateOnly)),
		}
	}

	daysUntilExpiry := int(time.Until(leaf.NotAfter).Hours() / 24)
	if daysUntilExpiry <= remoteTLSCertExpiryWarnDays {
		return CheckResult{
			Name:     "Remote TLS cert",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("expires in %d day(s) on %s", daysUntilExpiry, leaf.NotAfter.Format(time.DateOnly)),
		}
	}

	return CheckResult{
		Name:     "Remote TLS cert",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("valid until %s (%d days)", leaf.NotAfter.Format(time.DateOnly), daysUntilExpiry),
	}
}

// printDoctorReport renders the check results to out using ANSI colour codes.
// Results are grouped by section with headers.
func printDoctorReport(results []CheckResult, out io.Writer) {
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "VibeWarden Doctor")
	fmt.Fprintln(out, "─────────────────────────────────────────")

	currentSection := ""
	for _, r := range results {
		if r.Section != "" && r.Section != currentSection {
			currentSection = r.Section
			fmt.Fprintf(out, "\n  %s\n", bold(currentSection))
		}

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
