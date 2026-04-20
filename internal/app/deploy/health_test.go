package deploy

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/health"
)

func TestClassifyContainerStatus(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		wantCategory health.FailureCategory
		wantDetail   string
	}{
		{
			name:         "empty status means no containers",
			status:       "",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "No containers found",
		},
		{
			name:         "exited container",
			status:       "vibewarden  Exited (1) 5 seconds ago",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "have exited",
		},
		{
			name:         "dead container",
			status:       "vibewarden  Dead",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "have exited",
		},
		{
			name:         "restarting container",
			status:       "vibewarden  Restarting (1) 3 seconds ago",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "crash loop",
		},
		{
			name:         "unhealthy container",
			status:       "vibewarden  Up 30s (unhealthy)",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "unhealthy status",
		},
		{
			name:         "failed to fetch status",
			status:       "(failed to fetch status: connection refused)",
			wantCategory: health.CategoryContainerUnhealthy,
			wantDetail:   "Could not determine",
		},
		{
			name:         "running container returns no category",
			status:       "vibewarden  Up 10 seconds (healthy)",
			wantCategory: "",
			wantDetail:   "",
		},
		{
			name:         "multiple containers all up",
			status:       "vibewarden  Up 10s\napp  Up 10s",
			wantCategory: "",
			wantDetail:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCategory, gotDetail := classifyContainerStatus(tt.status)
			if gotCategory != tt.wantCategory {
				t.Errorf("classifyContainerStatus(%q) category = %q, want %q", tt.status, gotCategory, tt.wantCategory)
			}
			if tt.wantDetail != "" && !strings.Contains(gotDetail, tt.wantDetail) {
				t.Errorf("classifyContainerStatus(%q) detail = %q, want substring %q", tt.status, gotDetail, tt.wantDetail)
			}
		})
	}
}

func TestDetectTLSError(t *testing.T) {
	tests := []struct {
		name       string
		logs       string
		wantDetail string
	}{
		{
			name:       "empty logs",
			logs:       "",
			wantDetail: "",
		},
		{
			name:       "no TLS errors",
			logs:       "INFO starting server\nINFO listening on :8443",
			wantDetail: "",
		},
		{
			name:       "ACME challenge failed",
			logs:       "ERROR acme challenge failed: HTTP-01 challenge failed",
			wantDetail: "ACME challenge failed",
		},
		{
			name:       "TLS handshake error",
			logs:       "ERROR tls handshake error from 10.0.0.1: remote error",
			wantDetail: "TLS handshake error",
		},
		{
			name:       "certificate verify failed",
			logs:       "ERROR certificate verify failed: expired",
			wantDetail: "Certificate verification failed",
		},
		{
			name:       "no certificate available",
			logs:       "ERROR no certificate available for domain",
			wantDetail: "No certificate available",
		},
		{
			name:       "no certificates available",
			logs:       "WARN no certificates available for domain",
			wantDetail: "No TLS certificates available",
		},
		{
			name:       "unable to obtain certificate",
			logs:       "ERROR unable to obtain certificate for app.example.com",
			wantDetail: "Unable to obtain TLS certificate",
		},
		{
			name:       "dns problem",
			logs:       "ERROR dns problem: NXDOMAIN looking up A for app.example.com",
			wantDetail: "DNS problem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTLSError(tt.logs)
			if tt.wantDetail == "" {
				if got != "" {
					t.Errorf("detectTLSError() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantDetail) {
				t.Errorf("detectTLSError() = %q, want substring %q", got, tt.wantDetail)
			}
		})
	}
}

// fakeExec is a minimal RemoteExecutor fake for health diagnostic tests.
type fakeExec struct {
	responses map[string]struct {
		output string
		err    error
	}
}

func (f *fakeExec) Run(_ context.Context, cmd string) (string, error) {
	if r, ok := f.responses[cmd]; ok {
		return r.output, r.err
	}
	return "", nil
}

func (f *fakeExec) RunStream(_ context.Context, _ string, _, _ io.Writer) error {
	return nil
}

func (f *fakeExec) Transfer(_ context.Context, _, _ string, _ bool) error {
	return nil
}

func (f *fakeExec) TransferFile(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeExec) TransferExcluding(_ context.Context, _, _ string, _ bool, _ []string) error {
	return nil
}

func (f *fakeExec) DryRunTransfer(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

func TestDiagnoseHealthFailure_ContainerExited(t *testing.T) {
	exec := &fakeExec{
		responses: map[string]struct {
			output string
			err    error
		}{
			"cd ~/vibewarden/proj/ && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null": {
				output: "vibewarden  Exited (1) 10 seconds ago",
			},
			"cd ~/vibewarden/proj/ && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null": {
				output: "ERROR: failed to bind port",
			},
		},
	}

	svc := &Service{executor: exec}
	d := svc.diagnoseHealthFailure(context.Background(), "~/vibewarden/proj/", 8443, false)

	if d.Category() != health.CategoryContainerUnhealthy {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryContainerUnhealthy)
	}
	if !strings.Contains(d.Detail(), "have exited") {
		t.Errorf("Detail() = %q, want substring 'have exited'", d.Detail())
	}
}

func TestDiagnoseHealthFailure_TLSError(t *testing.T) {
	exec := &fakeExec{
		responses: map[string]struct {
			output string
			err    error
		}{
			"cd ~/vibewarden/proj/ && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null": {
				output: "vibewarden  Up 30 seconds",
			},
			"cd ~/vibewarden/proj/ && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null": {
				output: "ERROR acme challenge failed: timeout waiting for response",
			},
		},
	}

	svc := &Service{executor: exec}
	d := svc.diagnoseHealthFailure(context.Background(), "~/vibewarden/proj/", 8443, true)

	if d.Category() != health.CategoryTLSError {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryTLSError)
	}
	if !strings.Contains(d.Detail(), "ACME challenge failed") {
		t.Errorf("Detail() = %q, want substring 'ACME challenge failed'", d.Detail())
	}
}

func TestDiagnoseHealthFailure_UpstreamUnreachable(t *testing.T) {
	exec := &fakeExec{
		responses: map[string]struct {
			output string
			err    error
		}{
			"cd ~/vibewarden/proj/ && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null": {
				output: "vibewarden  Up 30 seconds",
			},
			"cd ~/vibewarden/proj/ && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null": {
				output: "INFO listening on :8443\nINFO serving",
			},
			"cd ~/vibewarden/proj/ && docker compose ps vibewarden --format '{{.Status}}' 2>/dev/null": {
				output: "Up 30 seconds",
			},
			"curl -sk -o /dev/null -w '%{http_code}' http://localhost:8443/_vibewarden/health 2>/dev/null || echo 000": {
				output: "502",
			},
		},
	}

	svc := &Service{executor: exec}
	d := svc.diagnoseHealthFailure(context.Background(), "~/vibewarden/proj/", 8443, false)

	if d.Category() != health.CategoryUpstreamUnreachable {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryUpstreamUnreachable)
	}
	if !strings.Contains(d.Detail(), "upstream application returned 502") {
		t.Errorf("Detail() = %q, want substring 'upstream application returned 502'", d.Detail())
	}
}

func TestDiagnoseHealthFailure_Timeout(t *testing.T) {
	exec := &fakeExec{
		responses: map[string]struct {
			output string
			err    error
		}{
			"cd ~/vibewarden/proj/ && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null": {
				output: "vibewarden  Up 30 seconds",
			},
			"cd ~/vibewarden/proj/ && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null": {
				output: "INFO starting server\nINFO listening on :8443",
			},
			"cd ~/vibewarden/proj/ && docker compose ps vibewarden --format '{{.Status}}' 2>/dev/null": {
				output: "Up 30 seconds",
			},
			"curl -sk -o /dev/null -w '%{http_code}' http://localhost:8443/_vibewarden/health 2>/dev/null || echo 000": {
				output: "000",
			},
		},
	}

	svc := &Service{executor: exec}
	d := svc.diagnoseHealthFailure(context.Background(), "~/vibewarden/proj/", 8443, false)

	if d.Category() != health.CategoryTimeout {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryTimeout)
	}
}

func TestDiagnoseHealthFailure_TLSEnabled_ProbeUsesHTTPS(t *testing.T) {
	exec := &fakeExec{
		responses: map[string]struct {
			output string
			err    error
		}{
			"cd ~/vibewarden/proj/ && docker compose ps --format '{{.Name}}  {{.Status}}' 2>/dev/null": {
				output: "vibewarden  Up 30 seconds",
			},
			"cd ~/vibewarden/proj/ && docker compose logs vibewarden --tail=30 --no-color 2>/dev/null": {
				output: "INFO serving",
			},
			"cd ~/vibewarden/proj/ && docker compose ps vibewarden --format '{{.Status}}' 2>/dev/null": {
				output: "Up 30 seconds",
			},
			"curl -sk -o /dev/null -w '%{http_code}' https://localhost:443/_vibewarden/health 2>/dev/null || echo 000": {
				output: "503",
			},
		},
	}

	svc := &Service{executor: exec}
	d := svc.diagnoseHealthFailure(context.Background(), "~/vibewarden/proj/", 443, true)

	if d.Category() != health.CategoryUpstreamUnreachable {
		t.Errorf("Category() = %q, want %q", d.Category(), health.CategoryUpstreamUnreachable)
	}
	if !strings.Contains(d.Detail(), "503") {
		t.Errorf("Detail() = %q, want substring '503'", d.Detail())
	}
}
