package ops_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ---------------------------------------------------------------------------
// Stub implementations
// ---------------------------------------------------------------------------

// stubDNSResolver is a test double for ports.DNSResolver.
type stubDNSResolver struct {
	addrs []string
	err   error
}

func (s *stubDNSResolver) LookupHost(_ context.Context, _ string) ([]string, error) {
	return s.addrs, s.err
}

// stubImageInspector is a test double for ports.ImageInspector.
type stubImageInspector struct {
	info ports.ImageInfo
	err  error
}

func (s *stubImageInspector) Inspect(_ context.Context, _ string) (ports.ImageInfo, error) {
	return s.info, s.err
}

// ---------------------------------------------------------------------------
// checkDNSResolves
// ---------------------------------------------------------------------------

func TestCheckDNSResolves_PASS_WARN_FAIL(t *testing.T) {
	nxdomainErr := &net.DNSError{IsNotFound: true, Name: "missing.example.com"}
	genericErr := errors.New("network unreachable")

	tests := []struct {
		name          string
		domain        string
		resolverAddrs []string
		resolverErr   error
		wantSeverity  ops.Severity
		wantSubstr    string
	}{
		{
			name:          "OK — single address",
			domain:        "demo.example.com",
			resolverAddrs: []string{"178.104.159.3"},
			wantSeverity:  ops.SeverityOK,
			wantSubstr:    "178.104.159.3",
		},
		{
			name:          "OK — multiple addresses truncated to 3",
			domain:        "demo.example.com",
			resolverAddrs: []string{"1.2.3.4", "5.6.7.8", "9.10.11.12", "13.14.15.16"},
			wantSeverity:  ops.SeverityOK,
			wantSubstr:    "...",
		},
		{
			name:          "OK — exactly 3 addresses no ellipsis",
			domain:        "demo.example.com",
			resolverAddrs: []string{"1.2.3.4", "5.6.7.8", "9.10.11.12"},
			wantSeverity:  ops.SeverityOK,
			wantSubstr:    "demo.example.com",
		},
		{
			name:         "WARN — NXDOMAIN error",
			domain:       "missing.example.com",
			resolverErr:  nxdomainErr,
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "NXDOMAIN",
		},
		{
			name:          "WARN — empty addrs (no error)",
			domain:        "missing.example.com",
			resolverAddrs: []string{},
			wantSeverity:  ops.SeverityWarn,
			wantSubstr:    "NXDOMAIN",
		},
		{
			name:         "WARN — generic network error",
			domain:       "demo.example.com",
			resolverErr:  genericErr,
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "lookup failed",
		},
		{
			name:         "OK — empty domain skips check",
			domain:       "",
			wantSeverity: ops.SeverityOK,
			wantSubstr:   "skipping DNS check",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.TLS.Domain = tt.domain

			resolver := &stubDNSResolver{addrs: tt.resolverAddrs, err: tt.resolverErr}
			inspector := &stubImageInspector{err: ports.ErrImageNotFound}

			svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
				WithPreflight("production", resolver, inspector)

			var buf bytes.Buffer
			_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q\ngot:\n%s", tt.wantSubstr, out)
			}
			// Verify severity via badge in human output.
			badge := severityBadge(tt.wantSeverity)
			// Only check badge when we expect a specific severity.
			// For OK checks the report may have other [OK] rows too, so just
			// verify the DNS resolves row contains the right badge by scanning lines.
			foundRow := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "DNS resolves") {
					foundRow = true
					if !strings.Contains(line, badge) {
						t.Errorf("DNS resolves row: want badge %q\nline: %s", badge, line)
					}
					break
				}
			}
			if !foundRow {
				t.Errorf("DNS resolves row not found in output:\n%s", out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkProductionPort
// ---------------------------------------------------------------------------

func TestCheckProductionPort_PASS_WARN(t *testing.T) {
	tests := []struct {
		name         string
		port         int
		wantSeverity ops.Severity
		wantSubstr   string
	}{
		{
			name:         "PASS — port 443",
			port:         443,
			wantSeverity: ops.SeverityOK,
			wantSubstr:   "server.port = 443",
		},
		{
			name:         "WARN — port 8443",
			port:         8443,
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "expected 443 for production",
		},
		{
			name:         "WARN — port 0 (defaults to 8443)",
			port:         0,
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "expected 443 for production",
		},
		{
			name:         "WARN — arbitrary port",
			port:         9000,
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "9000",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Server.Port = tt.port
			// Need LE provider for TLS email check to produce "not using LE".
			cfg.TLS.Provider = "external"

			resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
			inspector := &stubImageInspector{err: ports.ErrImageNotFound}

			svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
				WithPreflight("production", resolver, inspector)

			var buf bytes.Buffer
			_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q\ngot:\n%s", tt.wantSubstr, out)
			}
			badge := severityBadge(tt.wantSeverity)
			foundRow := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "Production port") {
					foundRow = true
					if !strings.Contains(line, badge) {
						t.Errorf("Production port row: want badge %q\nline: %s", badge, line)
					}
					break
				}
			}
			if !foundRow {
				t.Errorf("Production port row not found in output:\n%s", out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkTargetPlatform
// ---------------------------------------------------------------------------

func TestCheckTargetPlatform_PASS_FAIL(t *testing.T) {
	tests := []struct {
		name           string
		targetPlatform string
		wantSeverity   ops.Severity
		wantSubstr     string
	}{
		{
			name:           "PASS — linux/amd64",
			targetPlatform: "linux/amd64",
			wantSeverity:   ops.SeverityOK,
			wantSubstr:     "linux/amd64",
		},
		{
			name:           "PASS — linux/arm64",
			targetPlatform: "linux/arm64",
			wantSeverity:   ops.SeverityOK,
			wantSubstr:     "linux/arm64",
		},
		{
			name:           "FAIL — empty platform",
			targetPlatform: "",
			wantSeverity:   ops.SeverityFail,
			wantSubstr:     "deploy.target_platform is empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Deploy.TargetPlatform = tt.targetPlatform
			cfg.TLS.Provider = "external"

			resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
			inspector := &stubImageInspector{err: ports.ErrImageNotFound}

			svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
				WithPreflight("production", resolver, inspector)

			var buf bytes.Buffer
			_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q\ngot:\n%s", tt.wantSubstr, out)
			}
			badge := severityBadge(tt.wantSeverity)
			foundRow := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "Target platform") {
					foundRow = true
					if !strings.Contains(line, badge) {
						t.Errorf("Target platform row: want badge %q\nline: %s", badge, line)
					}
					break
				}
			}
			if !foundRow {
				t.Errorf("Target platform row not found in output:\n%s", out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkImageArch
// ---------------------------------------------------------------------------

func TestCheckImageArch_PASS_WARN_FAIL(t *testing.T) {
	tests := []struct {
		name           string
		appImage       string
		projectName    string
		targetPlatform string
		inspectorInfo  ports.ImageInfo
		inspectorErr   error
		wantSeverity   ops.Severity
		wantSubstr     string
	}{
		{
			name:           "PASS — arch matches target platform",
			appImage:       "myapp:latest",
			targetPlatform: "linux/amd64",
			inspectorInfo:  ports.ImageInfo{OS: "linux", Architecture: "amd64"},
			wantSeverity:   ops.SeverityOK,
			wantSubstr:     "matches deploy.target_platform",
		},
		{
			name:           "FAIL — arch mismatch",
			appImage:       "myapp:latest",
			targetPlatform: "linux/amd64",
			inspectorInfo:  ports.ImageInfo{OS: "linux", Architecture: "arm64"},
			wantSeverity:   ops.SeverityFail,
			wantSubstr:     "does not match deploy.target_platform",
		},
		{
			name:           "WARN — image not found locally",
			appImage:       "myapp:latest",
			targetPlatform: "linux/amd64",
			inspectorErr:   ports.ErrImageNotFound,
			wantSeverity:   ops.SeverityWarn,
			wantSubstr:     "not found locally",
		},
		{
			name:           "WARN — docker unavailable",
			appImage:       "myapp:latest",
			targetPlatform: "linux/amd64",
			inspectorErr:   ports.ErrDockerUnavailable,
			wantSeverity:   ops.SeverityWarn,
			wantSubstr:     "Docker unavailable",
		},
		{
			name:           "WARN — generic inspect error",
			appImage:       "myapp:latest",
			targetPlatform: "linux/amd64",
			inspectorErr:   errors.New("some unexpected error"),
			wantSeverity:   ops.SeverityWarn,
			wantSubstr:     "could not inspect image",
		},
		{
			name:           "PASS — no image configured skips check",
			appImage:       "",
			projectName:    "",
			targetPlatform: "linux/amd64",
			wantSeverity:   ops.SeverityOK,
			wantSubstr:     "skipping arch check",
		},
		{
			name:           "PASS — uses project name fallback tag",
			appImage:       "",
			projectName:    "myproject",
			targetPlatform: "linux/amd64",
			inspectorInfo:  ports.ImageInfo{OS: "linux", Architecture: "amd64"},
			wantSeverity:   ops.SeverityOK,
			wantSubstr:     "matches deploy.target_platform",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.App.Image = tt.appImage
			cfg.Name = tt.projectName
			cfg.Deploy.TargetPlatform = tt.targetPlatform
			cfg.TLS.Provider = "external"

			resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
			inspector := &stubImageInspector{info: tt.inspectorInfo, err: tt.inspectorErr}

			svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
				WithPreflight("production", resolver, inspector)

			var buf bytes.Buffer
			_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q\ngot:\n%s", tt.wantSubstr, out)
			}
			badge := severityBadge(tt.wantSeverity)
			foundRow := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "App image arch") {
					foundRow = true
					if !strings.Contains(line, badge) {
						t.Errorf("App image arch row: want badge %q\nline: %s", badge, line)
					}
					break
				}
			}
			if !foundRow {
				t.Errorf("App image arch row not found in output:\n%s", out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checkTLSEmail
// ---------------------------------------------------------------------------

func TestCheckTLSEmail_PASS_WARN(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		email        string
		wantSeverity ops.Severity
		wantSubstr   string
	}{
		{
			name:         "WARN — letsencrypt without email",
			provider:     "letsencrypt",
			email:        "",
			wantSeverity: ops.SeverityWarn,
			wantSubstr:   "empty (Let's Encrypt accepts anonymous",
		},
		{
			name:         "PASS — letsencrypt with email",
			provider:     "letsencrypt",
			email:        "admin@example.com",
			wantSeverity: ops.SeverityOK,
			wantSubstr:   "admin@example.com",
		},
		{
			name:         "PASS — self-signed, no email required",
			provider:     "self-signed",
			email:        "",
			wantSeverity: ops.SeverityOK,
			wantSubstr:   "not using Let's Encrypt",
		},
		{
			name:         "PASS — external, no email required",
			provider:     "external",
			email:        "",
			wantSeverity: ops.SeverityOK,
			wantSubstr:   "not using Let's Encrypt",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.TLS.Provider = tt.provider
			cfg.TLS.Email = tt.email

			resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
			inspector := &stubImageInspector{err: ports.ErrImageNotFound}

			svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
				WithPreflight("production", resolver, inspector)

			var buf bytes.Buffer
			_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
			if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			out := buf.String()
			if !strings.Contains(out, tt.wantSubstr) {
				t.Errorf("output missing %q\ngot:\n%s", tt.wantSubstr, out)
			}
			badge := severityBadge(tt.wantSeverity)
			foundRow := false
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "TLS email") {
					foundRow = true
					if !strings.Contains(line, badge) {
						t.Errorf("TLS email row: want badge %q\nline: %s", badge, line)
					}
					break
				}
			}
			if !foundRow {
				t.Errorf("TLS email row not found in output:\n%s", out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DoctorService integration: WithPreflight wiring
// ---------------------------------------------------------------------------

// TestDoctorService_Run_PreflightSection_AppendedAfterStatic verifies that when
// WithPreflight is set, the Preflight section header appears AFTER the static
// Config & Docker section in the output.
func TestDoctorService_Run_PreflightSection_AppendedAfterStatic(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "demo.example.com"
	cfg.TLS.Email = "admin@example.com"
	cfg.Server.Port = 443
	cfg.Deploy.TargetPlatform = "linux/amd64"

	resolver := &stubDNSResolver{addrs: []string{"178.104.159.3"}}
	inspector := &stubImageInspector{err: ports.ErrImageNotFound}

	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithPreflight("production", resolver, inspector)

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Config & Docker") {
		t.Errorf("expected 'Config & Docker' section header\ngot:\n%s", out)
	}
	if !strings.Contains(out, "Preflight: production") {
		t.Errorf("expected 'Preflight: production' section header\ngot:\n%s", out)
	}

	// Config & Docker must appear before Preflight in output.
	cfgIdx := strings.Index(out, "Config & Docker")
	preIdx := strings.Index(out, "Preflight: production")
	if cfgIdx >= preIdx {
		t.Errorf("expected 'Config & Docker' before 'Preflight: production'\ngot:\n%s", out)
	}

	// All 5 preflight check names must be present.
	for _, checkName := range []string{
		"DNS resolves",
		"Production port",
		"Target platform",
		"App image arch",
		"TLS email",
	} {
		if !strings.Contains(out, checkName) {
			t.Errorf("expected preflight check %q in output\ngot:\n%s", checkName, out)
		}
	}
}

// TestDoctorService_Run_PreflightDisabled_NoSection verifies that without
// WithPreflight, no "Preflight:" header appears in the output (regression guard).
func TestDoctorService_Run_PreflightDisabled_NoSection(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.Provider = "external"

	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{})

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Preflight:") {
		t.Errorf("expected no 'Preflight:' section when WithPreflight is not set\ngot:\n%s", out)
	}
}

// TestDoctorService_Run_PreflightFAIL_AffectsExitCode verifies that a FAIL in
// the preflight section (e.g. empty target_platform) causes allOK == false.
func TestDoctorService_Run_PreflightFAIL_AffectsExitCode(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.Provider = "external"
	cfg.Deploy.TargetPlatform = "" // will FAIL

	resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
	inspector := &stubImageInspector{err: ports.ErrImageNotFound}

	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithPreflight("staging", resolver, inspector)

	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if allOK {
		t.Errorf("expected allOK = false when Target platform is empty (FAIL)\noutput:\n%s", buf.String())
	}
}

// TestDoctorService_Run_Preflight_FiveRowsInFixedOrder verifies that the
// orchestrator emits exactly 5 rows tagged with the preflight section when all
// stubs return OK, and that their order is DNS → port → platform → arch → email.
func TestDoctorService_Run_Preflight_FiveRowsInFixedOrder(t *testing.T) {
	cfg := &config.Config{}
	cfg.TLS.Provider = "letsencrypt"
	cfg.TLS.Domain = "demo.example.com"
	cfg.TLS.Email = "admin@example.com"
	cfg.Server.Port = 443
	cfg.Deploy.TargetPlatform = "linux/amd64"
	cfg.App.Image = "myapp:latest"

	resolver := &stubDNSResolver{addrs: []string{"1.2.3.4"}}
	inspector := &stubImageInspector{info: ports.ImageInfo{OS: "linux", Architecture: "amd64"}}

	svc := ops.NewDoctorService(noContainersCompose(), &fakePortChecker{}).
		WithPreflight("production", resolver, inspector)

	var buf bytes.Buffer
	_, err := svc.Run(context.Background(), cfg, defaultOpts(t), &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	// Verify order by index positions.
	checkNames := []string{
		"DNS resolves",
		"Production port",
		"Target platform",
		"App image arch",
		"TLS email",
	}
	prevIdx := -1
	for _, name := range checkNames {
		idx := strings.Index(out, name)
		if idx < 0 {
			t.Errorf("check %q not found in output:\n%s", name, out)
			continue
		}
		if idx <= prevIdx {
			t.Errorf("check %q appears before previous check (out of order)\n"+
				"output:\n%s", name, out)
		}
		prevIdx = idx
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// severityBadge maps a Severity to the text badge used in human output.
func severityBadge(s ops.Severity) string {
	switch s {
	case ops.SeverityOK:
		return "[OK]"
	case ops.SeverityWarn:
		return "[WARN]"
	default:
		return "[FAIL]"
	}
}
