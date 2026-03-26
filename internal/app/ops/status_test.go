package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
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
	proxyBase := "http://localhost:8080"

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

	proxyBase := "http://localhost:8080"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health":  {ok: true, statusCode: 200},
		proxyBase + "/_vibewarden/metrics": {ok: true, statusCode: 200},
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

	proxyBase := "http://localhost:8080"
	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		proxyBase + "/_vibewarden/health": {ok: true, statusCode: 200},
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

func TestStatusService_TLSEnabled(t *testing.T) {
	cfg := defaultConfig()
	cfg.TLS.Enabled = true
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "example.com"

	checker := &fakeHealthChecker{responses: map[string]healthResponse{
		"https://localhost:8080/_vibewarden/health":  {ok: true, statusCode: 200},
		"https://localhost:8080/_vibewarden/metrics": {ok: true, statusCode: 200},
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
