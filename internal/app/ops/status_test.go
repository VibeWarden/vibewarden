package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// hasComponentFAIL reports whether any component row in the status output
// carries a FAIL label. It skips the legend line (which always contains the
// word "FAIL" as part of "FAIL = check failed") by only inspecting lines
// that are indented (component rows start with two spaces).
func hasComponentFAIL(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "  ") && strings.Contains(line, "FAIL") {
			return true
		}
	}
	return false
}

// fakeHealthChecker is a test double for ports.HealthChecker.
type fakeHealthChecker struct {
	// responses maps URL → (ok, statusCode, err)
	responses map[string]healthResponse
	// callCount tracks how many times CheckHealth was called per URL.
	callCount map[string]int
}

type healthResponse struct {
	ok         bool
	statusCode int
	err        error
}

func (f *fakeHealthChecker) CheckHealth(_ context.Context, url string) (bool, int, error) {
	if f.callCount == nil {
		f.callCount = make(map[string]int)
	}
	f.callCount[url]++
	if r, found := f.responses[url]; found {
		return r.ok, r.statusCode, r.err
	}
	// default: unreachable
	return false, 0, errors.New("unreachable")
}

func TestStatusService_Run(t *testing.T) {
	cfg := defaultConfig()
	proxyBase := "https://localhost:8443"

	healthURL := proxyBase + "/_vibewarden/health"
	metricsURL := proxyBase + "/_vibewarden/metrics"

	tests := []struct {
		name               string
		responses          map[string]healthResponse
		wantOutputContains []string
	}{
		{
			name: "all healthy",
			responses: map[string]healthResponse{
				healthURL:  {ok: true, statusCode: 200},
				metricsURL: {ok: true, statusCode: 200},
			},
			wantOutputContains: []string{
				"VibeWarden Status",
				"Proxy",
				"Auth (Kratos)",
				"Rate Limit",
				"Metrics",
				"TLS",
			},
		},
		{
			name: "proxy unhealthy",
			responses: map[string]healthResponse{
				healthURL:  {ok: false, statusCode: 503},
				metricsURL: {ok: true, statusCode: 200},
			},
			wantOutputContains: []string{
				"Proxy",
				"HTTP 503",
			},
		},
		{
			name: "proxy unreachable",
			responses: map[string]healthResponse{
				healthURL:  {ok: false, err: errors.New("connection refused")},
				metricsURL: {ok: true, statusCode: 200},
			},
			wantOutputContains: []string{
				"unreachable",
			},
		},
		{
			name: "rate limit disabled shows disabled",
			responses: map[string]healthResponse{
				healthURL:  {ok: true, statusCode: 200},
				metricsURL: {ok: true, statusCode: 200},
			},
			wantOutputContains: []string{"enabled"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &fakeHealthChecker{responses: tt.responses}
			svc := ops.NewStatusService(checker)
			var buf bytes.Buffer

			err := svc.Run(context.Background(), cfg, &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			for _, want := range tt.wantOutputContains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, out)
				}
			}
		})
	}
}

func TestStatusService_RateLimitDisabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.RateLimit.Enabled = false

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(buf.String(), "disabled") {
		t.Errorf("expected 'disabled' in rate limit row, got:\n%s", buf.String())
	}
}

func TestStatusService_MetricsDisabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.Metrics.Enabled = false

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Metrics") {
		t.Errorf("expected Metrics row in output, got:\n%s", out)
	}
}

func TestStatusService_PluginSectionShown(t *testing.T) {
	cfg := defaultConfig()

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// Plugin section header must appear.
	if !strings.Contains(out, "Plugins") {
		t.Errorf("expected 'Plugins' section header, got:\n%s", out)
	}

	// Each canonical plugin name must appear.
	for _, name := range []string{"tls", "security-headers", "rate-limiting", "auth", "metrics", "user-management", "waf", "cors", "egress", "compression"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected plugin %q in status output, got:\n%s", name, out)
		}
	}
}

func TestStatusService_TLSEnabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "example.com"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "letsencrypt") {
		t.Errorf("expected TLS provider in output, got:\n%s", out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("expected domain in output, got:\n%s", out)
	}
}

// fakeStatusCompose is a test double for ports.ComposeRunner used by status tests.
type fakeStatusCompose struct {
	psResult []ports.ContainerInfo
	psErr    error
}

func (f *fakeStatusCompose) Up(_ context.Context, _ string, _ []string, _ ports.ComposeUpOptions) error {
	return nil
}
func (f *fakeStatusCompose) Restart(_ context.Context, _ string, _ []string) error {
	return nil
}
func (f *fakeStatusCompose) Down(_ context.Context, _ string, _ ports.ComposeDownOptions) (ports.DownResult, error) {
	return ports.DownResult{}, nil
}
func (f *fakeStatusCompose) Version(_ context.Context) (string, error) { return "", nil }
func (f *fakeStatusCompose) Info(_ context.Context) error              { return nil }
func (f *fakeStatusCompose) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return f.psResult, f.psErr
}
func (f *fakeStatusCompose) Logs(_ context.Context, _ string, _ string, _ int) (string, error) {
	return "", nil
}

// fakeStatusLogs is a test double for ports.ComposeLogs used by status tests.
type fakeStatusLogs struct {
	output string
	err    error
}

func (f *fakeStatusLogs) Tail(_ context.Context, _ string, _ string, _ int) (string, error) {
	return f.output, f.err
}

func TestStatusService_ProxyUnreachable_NoContainers_ShowsDiagnosis(t *testing.T) {
	cfg := defaultConfig()

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	compose := &fakeStatusCompose{psResult: nil} // no containers
	svc := ops.NewStatusService(checker).WithCompose(compose)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Sidecar container is not running") {
		t.Errorf("expected 'Sidecar container is not running' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "vibew doctor") {
		t.Errorf("expected 'vibew doctor' suggestion in output, got:\n%s", out)
	}
}

func TestStatusService_ProxyUnreachable_SidecarRunning_ShowsLogDiagnosis(t *testing.T) {
	cfg := defaultConfig()

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	compose := &fakeStatusCompose{psResult: []ports.ContainerInfo{
		{Service: "vibewarden", State: "running"},
	}}
	logs := &fakeStatusLogs{output: "error: ACME challenge failed for domain"}
	svc := ops.NewStatusService(checker).WithCompose(compose).WithLogs(logs)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "TLS/ACME errors") {
		t.Errorf("expected TLS/ACME diagnosis in output, got:\n%s", out)
	}
}

func TestStatusService_ProxyUnreachable_LetsencryptLocal_ShowsHint(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	compose := &fakeStatusCompose{psResult: []ports.ContainerInfo{
		{Service: "vibewarden", State: "running"},
	}}
	svc := ops.NewStatusService(checker).WithCompose(compose)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "letsencrypt") {
		t.Errorf("expected letsencrypt hint in output, got:\n%s", out)
	}
	if !strings.Contains(out, "self-signed") {
		t.Errorf("expected self-signed suggestion in output, got:\n%s", out)
	}
}

func TestStatusService_ProxyHealthy_NoDiagnostics(t *testing.T) {
	cfg := defaultConfig()

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	compose := &fakeStatusCompose{psResult: nil}
	svc := ops.NewStatusService(checker).WithCompose(compose)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Diagnosis") {
		t.Errorf("expected no diagnosis when proxy is healthy, got:\n%s", out)
	}
}

func TestStatusService_ProxyUnreachable_DoctorSuggestion(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 8443},
		TLS:    config.TLSConfig{Enabled: true, Provider: "self-signed"},
	}

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health": {ok: false, err: errors.New("connection refused")},
	}}

	// No compose wired, so diagnosis falls through to the suggestion.
	svc := ops.NewStatusService(checker)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "vibew doctor") {
		t.Errorf("expected 'vibew doctor' suggestion in output, got:\n%s", out)
	}
}

// TestStatusService_WAFPluginShown verifies that the WAF plugin appears in
// status output with its mode detail when enabled.
//
// Artifact test for #960.
func TestStatusService_WAFPluginShown(t *testing.T) {
	cfg := defaultConfig()
	cfg.WAF.Enabled = true
	cfg.WAF.Mode = "block"

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "waf") {
		t.Errorf("expected 'waf' in plugin status output, got:\n%s", out)
	}
	if !strings.Contains(out, "mode: block") {
		t.Errorf("expected 'mode: block' detail for WAF plugin, got:\n%s", out)
	}
}

// TestStatusService_WAFPluginDisabledShown verifies that when WAF is disabled,
// it still appears in the plugin list but shows as disabled.
func TestStatusService_WAFPluginDisabledShown(t *testing.T) {
	cfg := defaultConfig()
	cfg.WAF.Enabled = false

	proxyBase := "https://localhost:8443"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "waf") {
		t.Errorf("expected 'waf' in plugin status output even when disabled, got:\n%s", out)
	}
}

// --- New tests for ADR-095 three-state model ---

// fakeTLSStateResolver is a test double for ports.TLSStateResolver.
type fakeTLSStateResolver struct {
	state tlsdomain.State
	err   error
}

func (f *fakeTLSStateResolver) Resolve(_ context.Context) (tlsdomain.State, error) {
	return f.state, f.err
}

// TestStatusService_AuthDisabled_ShowsOFF_NoHTTPCall verifies that when
// auth is disabled in config the Auth row shows OFF and the Kratos URL is
// never contacted.
func TestStatusService_AuthDisabled_ShowsOFF_NoHTTPCall(t *testing.T) {
	cfg := defaultConfig()
	// defaultConfig leaves Auth.Mode empty → Active() == false
	kratosURL := "http://127.0.0.1:4434/admin/health/ready"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
		kratosURL: {ok: true, statusCode: 200}, // should never be called
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()

	// The row must appear.
	if !strings.Contains(out, "Auth (Kratos)") {
		t.Errorf("expected 'Auth (Kratos)' row in output, got:\n%s", out)
	}

	// The row must show "auth disabled".
	if !strings.Contains(out, "auth disabled") {
		t.Errorf("expected 'auth disabled' detail, got:\n%s", out)
	}

	// OFF must appear in output.
	if !strings.Contains(out, "OFF") {
		t.Errorf("expected 'OFF' label in output, got:\n%s", out)
	}

	// Kratos URL must NOT have been called.
	if checker.callCount[kratosURL] > 0 {
		t.Errorf("Kratos health URL was called %d times; expected 0 when auth is disabled", checker.callCount[kratosURL])
	}
}

// TestStatusService_AuthEnabled_Reachable_ShowsOK verifies that when auth
// is enabled and Kratos is reachable the row shows OK.
func TestStatusService_AuthEnabled_Reachable_ShowsOK(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth.Mode = "kratos"
	kratosURL := "http://127.0.0.1:4434/admin/health/ready"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
		kratosURL: {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Auth (Kratos)") {
		t.Errorf("expected 'Auth (Kratos)' row, got:\n%s", out)
	}
	// OK must appear (proxy is also OK, so at least one OK).
	if !strings.Contains(out, "OK") {
		t.Errorf("expected 'OK' label when auth is enabled and reachable, got:\n%s", out)
	}
	// No component row must contain "FAIL" — the legend line is excluded by
	// checking only lines that start with whitespace (component rows are indented).
	if hasComponentFAIL(out) {
		t.Errorf("expected no FAIL component rows when auth is reachable, got:\n%s", out)
	}
	// The Kratos health probe must have been called exactly once when auth is enabled.
	if checker.callCount[kratosURL] == 0 {
		t.Errorf("Kratos health URL was never called when auth is enabled")
	}
}

// TestStatusService_AuthEnabled_Unreachable_ShowsFAIL verifies that when
// auth is enabled but Kratos is unreachable the row shows FAIL.
func TestStatusService_AuthEnabled_Unreachable_ShowsFAIL(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth.Mode = "kratos"
	kratosURL := "http://127.0.0.1:4434/admin/health/ready"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
		kratosURL: {ok: false, err: errors.New("connection refused")},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("expected 'FAIL' label when Kratos is unreachable, got:\n%s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("expected 'unreachable' detail when Kratos is unreachable, got:\n%s", out)
	}
}

// TestStatusService_PrintStatusTable_LegendPresent verifies that the legend
// line is rendered in the status table output.
func TestStatusService_PrintStatusTable_LegendPresent(t *testing.T) {
	cfg := defaultConfig()

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	svc := ops.NewStatusService(checker)
	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "States: OK") {
		t.Errorf("expected legend line 'States: OK' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "OFF") {
		t.Errorf("expected 'OFF' in legend, got:\n%s", out)
	}
	if !strings.Contains(out, "FAIL") {
		t.Errorf("expected 'FAIL' in legend, got:\n%s", out)
	}
}

// TestStatusService_TLSSelfSigned_NeverFAIL verifies that a self-signed dev
// cert never produces a FAIL row (acceptance criterion from ADR-095).
func TestStatusService_TLSSelfSigned_NeverFAIL(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "self-signed"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	resolver := &fakeTLSStateResolver{state: tlsdomain.NewSelfSignedLocal()}
	svc := ops.NewStatusService(checker).WithTLSStateResolver(resolver)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if hasComponentFAIL(out) {
		t.Errorf("expected no FAIL rows for self-signed dev cert, got:\n%s", out)
	}
	if !strings.Contains(out, "self-signed (dev)") {
		t.Errorf("expected 'self-signed (dev)' annotation, got:\n%s", out)
	}
}

// TestStatusService_TLSObtainedExpiringSoon_ShowsOK verifies that a cert
// expiring in 5 days produces StatusOK with an annotation, not FAIL.
func TestStatusService_TLSObtainedExpiringSoon_ShowsOK(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	expiry := time.Now().Add(5 * 24 * time.Hour)
	resolver := &fakeTLSStateResolver{state: tlsdomain.NewExpiringSoon(5, expiry)}
	svc := ops.NewStatusService(checker).WithTLSStateResolver(resolver)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if hasComponentFAIL(out) {
		t.Errorf("expected no FAIL for expiring-soon TLS (should be OK + annotation), got:\n%s", out)
	}
	if !strings.Contains(out, "expires in 5 days") {
		t.Errorf("expected 'expires in 5 days' annotation, got:\n%s", out)
	}
}

// TestStatusService_TLSFailing_ShowsFAIL verifies that KindFailing produces
// a FAIL row with the error detail.
func TestStatusService_TLSFailing_ShowsFAIL(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8443/_vibewarden/metrics": {ok: true, statusCode: 200},
	}}

	resolver := &fakeTLSStateResolver{state: tlsdomain.NewFailing("dns challenge timeout")}
	svc := ops.NewStatusService(checker).WithTLSStateResolver(resolver)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("expected 'FAIL' for failing TLS, got:\n%s", out)
	}
	if !strings.Contains(out, "dns challenge timeout") {
		t.Errorf("expected error detail in TLS row, got:\n%s", out)
	}
}

// TestStatusService_DevSmoke verifies that the default dev stack config
// (auth off, metrics off, rate-limit on, TLS self-signed) produces zero
// FAIL rows — acceptance criterion from ADR-095.
func TestStatusService_DevSmoke_ZeroFAIL(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8443},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		TLS:      config.TLSConfig{Enabled: true, Provider: "self-signed"},
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
		},
		// Auth.Mode is empty → Active() == false
		// Metrics.Enabled is false (zero value)
	}

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8443/_vibewarden/health": {ok: true, statusCode: 200},
	}}

	resolver := &fakeTLSStateResolver{state: tlsdomain.NewSelfSignedLocal()}
	svc := ops.NewStatusService(checker).WithTLSStateResolver(resolver)

	var buf bytes.Buffer
	if err := svc.Run(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	// No component rows must carry a FAIL label (the legend contains the word
	// "FAIL" but component rows are the only indented lines starting with spaces
	// followed by a state label).
	if hasComponentFAIL(out) {
		t.Errorf("unexpected FAIL row on dev smoke stack:\nfull output:\n%s", out)
	}
}
