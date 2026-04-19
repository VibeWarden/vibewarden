package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeHealthChecker is a test double for ports.HealthChecker.
type fakeHealthChecker struct {
	// responses maps URL → (ok, statusCode, err)
	responses map[string]healthResponse
}

type healthResponse struct {
	ok         bool
	statusCode int
	err        error
}

func (f *fakeHealthChecker) CheckHealth(_ context.Context, url string) (bool, int, error) {
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
	kratosURL := "http://127.0.0.1:4434/admin/health/ready"

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
				kratosURL:  {ok: true, statusCode: 200},
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
				kratosURL:  {ok: true, statusCode: 200},
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
				kratosURL:  {ok: true, statusCode: 200},
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
				kratosURL:  {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
	for _, name := range []string{"tls", "security-headers", "rate-limiting", "auth", "metrics", "user-management"} {
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
		"http://127.0.0.1:4434/admin/health/ready":   {ok: true, statusCode: 200},
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

func (f *fakeStatusCompose) Up(_ context.Context, _ string, _ []string) error { return nil }
func (f *fakeStatusCompose) Restart(_ context.Context, _ string, _ []string) error {
	return nil
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
		proxyBase + "/_vibewarden/health":          {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: false, err: errors.New("connection refused")},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics":         {ok: true, statusCode: 200},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
		proxyBase + "/_vibewarden/health":          {ok: false, err: errors.New("connection refused")},
		"http://127.0.0.1:4434/admin/health/ready": {ok: true, statusCode: 200},
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
