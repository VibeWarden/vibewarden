package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	apptlspreflight "github.com/vibewarden/vibewarden/internal/app/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	domaintlspreflight "github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
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
	// "Local Runtime"). Empty for legacy checks.
	Section string `json:"section,omitempty"`
}

// OK returns true when the check severity is OK.
func (c CheckResult) OK() bool { return c.Severity == SeverityOK }

// DoctorService orchestrates the "vibew doctor" use case.
// Every check runs independently — a failing check does not stop subsequent ones.
type DoctorService struct {
	compose        ports.ComposeRunner
	portChecker    ports.PortChecker
	imageChecker   ports.DockerImageChecker
	ownerProbe     ports.PortOwnerProbe
	tlsState       ports.TLSStateResolver   // optional; nil falls back to handshake-on-demand
	leRateLimitSvc *apptlspreflight.Service // optional; nil skips LE rate-limit check
}

// NewDoctorService creates a new DoctorService.
func NewDoctorService(compose ports.ComposeRunner, portChecker ports.PortChecker) *DoctorService {
	return &DoctorService{
		compose:     compose,
		portChecker: portChecker,
	}
}

// WithImageChecker returns a copy of the DoctorService with the given
// DockerImageChecker set for image tag consistency checks. When nil, the
// image tag check is skipped.
func (s *DoctorService) WithImageChecker(checker ports.DockerImageChecker) *DoctorService {
	cp := *s
	cp.imageChecker = checker
	return &cp
}

// WithPortOwnerProbe returns a copy of the DoctorService with the given
// PortOwnerProbe wired for port-ownership detection. When nil (or no probe
// is supplied) the proxy-port check falls back to "busy = FAIL" semantics —
// this preserves back-compat with existing tests that do not set a probe.
// See ADR-084.
func (s *DoctorService) WithPortOwnerProbe(probe ports.PortOwnerProbe) *DoctorService {
	cp := *s
	cp.ownerProbe = probe
	return &cp
}

// WithTLSStateResolver returns a copy of the DoctorService with the given
// resolver wired for the TLS check. When nil, the doctor builds a
// HandshakeResolver on the fly per invocation — this preserves behaviour
// for existing tests that construct the service without a resolver.
func (s *DoctorService) WithTLSStateResolver(r ports.TLSStateResolver) *DoctorService {
	cp := *s
	cp.tlsState = r
	return &cp
}

// WithLERateLimitService returns a copy of the DoctorService with the given
// tlspreflight.Service wired for the LE rate-limit preflight check. When nil,
// the check is skipped entirely (no CheckResult is emitted).
func (s *DoctorService) WithLERateLimitService(svc *apptlspreflight.Service) *DoctorService {
	cp := *s
	cp.leRateLimitSvc = svc
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
	// SkipLEPreflight skips the Let's Encrypt rate-limit preflight check.
	// Equivalent to setting tls.skip_rate_limit_check: true in vibewarden.yaml.
	// Both this flag and the config key are frozen by ADR-090.
	SkipLEPreflight bool
	// LERegisteredDomains is the deduplicated list of eTLD+1 domains to check
	// for LE rate limits. The CLI wiring is responsible for normalising FQDNs
	// via publicsuffix.EffectiveTLDPlusOne before populating this field.
	LERegisteredDomains []string
	// LESkippedDomains is the list of FQDNs whose registered domain could not
	// be derived (e.g. single-label hostnames like "localhost"). The doctor
	// emits a SeverityWarn CheckResult for each entry per ADR-090.
	LESkippedDomains []string
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

// localTLSCertExpiryWarnDays is the number of days before expiry that triggers
// a warning for local TLS certificates.
const localTLSCertExpiryWarnDays = 7

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
	results = append(results, withSection(checkACMEEmail(cfg), sectionConfigDocker))
	if s.imageChecker != nil && cfg.App.Image != "" {
		results = append(results, withSection(s.checkImageTagConsistency(ctx, cfg.App.Image), sectionConfigDocker))
	}

	// LE rate-limit preflight — only when all guards pass (see ADR-090 §(i)).
	// LESkippedDomains are also surfaced here so single-label hostnames
	// (e.g. "localhost") produce a WARN instead of silent omission.
	if s.leRateLimitSvc != nil &&
		!opts.SkipLEPreflight &&
		!cfg.TLS.SkipRateLimitCheck &&
		cfg.TLS.Enabled &&
		strings.EqualFold(cfg.TLS.Provider, "letsencrypt") &&
		cfg.TLS.Domain != "" &&
		cfg.TLS.ACMECA == "" {
		for _, cr := range s.checkLERateLimit(ctx, cfg, opts) {
			results = append(results, withSection(cr, sectionConfigDocker))
		}
	}

	// --- Layer 2: Local Runtime ---
	results = append(results, withSection(s.checkTLSCertValid(ctx, cfg, proxyHost, proxyPort), sectionLocalRuntime))

	// --- Dockerfile contract checks ---
	// Omitted entirely when no Dockerfile is present in the project root.
	results = append(results, s.checkDockerfile(ctx, workDir, cfg)...)

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

// checkPort combines the TCP-bind probe (PortChecker) with the optional
// ownership probe (PortOwnerProbe) to distinguish the expected "sibling
// vibew dev is running" state from a real foreign-process conflict. See
// ADR-084 for the decision table.
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
	if available {
		return CheckResult{
			Name:     label,
			Severity: SeverityOK,
			Detail:   fmt.Sprintf("port %d is available", port),
		}
	}

	// Port is bound. Ask the owner probe who is there.
	if s.ownerProbe != nil {
		if s.ownerProbe.ProbeOwner(checkCtx, host, port) == ports.OwnerVibeWarden {
			return CheckResult{
				Name:     label,
				Severity: SeverityOK,
				Detail:   fmt.Sprintf("port %d in use by local vibew dev (expected)", port),
			}
		}
	}

	return CheckResult{
		Name:     label,
		Severity: SeverityFail,
		Detail:   fmt.Sprintf("port %d is already in use", port),
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

// checkTLSCertValid renders the "TLS certificate" doctor check by
// consuming a resolved TLSState. When a resolver is wired on the
// DoctorService it is used directly; otherwise the service falls back to
// its built-in handshake path (preserved for tests and for composition
// roots that pre-date #1090). See PM spec #1090 / #1078 and ADR-084.
//
// The renderer in doctor_tls_render.go is the single source of truth for
// severity and detail mapping. This method only handles the resolver
// selection and the legacy "provider != self-signed" short-circuit.
func (s *DoctorService) checkTLSCertValid(ctx context.Context, cfg *config.Config, host string, port int) CheckResult {
	// Disabled short-circuit — renderer produces an OK result.
	if cfg != nil && !cfg.TLS.Enabled {
		return renderTLSDoctorCheck(tlsdomain.NewDisabled(), time.Now)
	}

	// Legacy behaviour for non-self-signed providers without a resolver:
	// skip the check. (External/ACME providers need an in-process
	// resolver to observe cert state; the composition root wires that
	// today, but older callers may not.)
	if s.tlsState == nil && cfg != nil && cfg.TLS.Provider != "self-signed" {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityOK,
			Detail:   fmt.Sprintf("provider is %q — skipping local cert check", cfg.TLS.Provider),
		}
	}

	resolver := s.tlsState
	if resolver == nil {
		resolver = newInlineHandshakeResolver(cfg, host, port)
	}

	state, err := resolver.Resolve(ctx)
	if err != nil {
		return CheckResult{
			Name:     "TLS certificate",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("resolver error: %v", err),
		}
	}

	return renderTLSDoctorCheck(state, time.Now)
}

// checkACMEEmail verifies that an ACME account email is configured when the
// ACME CA URL contains "zerossl". ZeroSSL requires an email address for
// External Account Binding (EAB) registration.
func checkACMEEmail(cfg *config.Config) CheckResult {
	acmeCA := strings.ToLower(cfg.TLS.ACMECA)
	if !strings.Contains(acmeCA, "zerossl") {
		return CheckResult{
			Name:     "ACME email",
			Severity: SeverityOK,
			Detail:   "not using ZeroSSL — email not required",
		}
	}
	if cfg.TLS.Email == "" {
		return CheckResult{
			Name:     "ACME email",
			Severity: SeverityFail,
			Detail:   "ZeroSSL requires tls.email for EAB registration",
		}
	}
	return CheckResult{
		Name:     "ACME email",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("email configured: %s", cfg.TLS.Email),
	}
}

// checkImageTagConsistency verifies that the configured app.image exists in
// the local Docker daemon. A missing image often causes deploy failures because
// docker save cannot export an image that does not exist locally.
func (s *DoctorService) checkImageTagConsistency(ctx context.Context, image string) CheckResult {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	exists, err := s.imageChecker.ImageExists(checkCtx, image)
	if err != nil {
		return CheckResult{
			Name:     "Image tag",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("could not check image %q: %v", image, err),
		}
	}
	if !exists {
		return CheckResult{
			Name:     "Image tag",
			Severity: SeverityFail,
			Detail:   fmt.Sprintf("image %q not found locally — build it before deploying", image),
		}
	}
	return CheckResult{
		Name:     "Image tag",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("image %q exists locally", image),
	}
}

// checkLERateLimit runs the LE rate-limit preflight for each registered domain
// in opts.LERegisteredDomains and maps each domain/tlspreflight.Result to a
// CheckResult. It never returns an error — query failures produce WARN results.
//
// Domains in opts.LESkippedDomains (single-label hostnames whose eTLD+1 could
// not be derived) are also included as SeverityWarn results per ADR-090.
func (s *DoctorService) checkLERateLimit(ctx context.Context, _ *config.Config, opts DoctorOptions) []CheckResult {
	if len(opts.LERegisteredDomains) == 0 && len(opts.LESkippedDomains) == 0 {
		return nil
	}

	out := make([]CheckResult, 0, len(opts.LERegisteredDomains)+len(opts.LESkippedDomains))

	// Emit a WARN CheckResult for each domain that could not be normalised to
	// eTLD+1 (e.g. "localhost"). No CT query is issued for these.
	for _, fqdn := range opts.LESkippedDomains {
		pr := domaintlspreflight.NewSkipped(fqdn)
		out = append(out, CheckResult{
			Name:     fmt.Sprintf("LE rate-limit: %s", pr.Domain),
			Severity: SeverityWarn,
			Detail:   pr.Detail,
		})
	}

	if len(opts.LERegisteredDomains) > 0 {
		preflightResults := s.leRateLimitSvc.Check(ctx, opts.LERegisteredDomains)
		for _, pr := range preflightResults {
			cr := CheckResult{
				Name:   fmt.Sprintf("LE rate-limit: %s", pr.Domain),
				Detail: pr.Detail,
			}
			switch pr.Status {
			case domaintlspreflight.StatusFail:
				cr.Severity = SeverityFail
			case domaintlspreflight.StatusWarn:
				cr.Severity = SeverityWarn
			default:
				cr.Severity = SeverityOK
			}
			out = append(out, cr)
		}
	}
	return out
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
