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

// fakeCompose is a test double for ports.ComposeRunner.
type fakeCompose struct {
	upErr      error
	versionStr string
	versionErr error
	infoErr    error
	psResult   []ports.ContainerInfo
	psErr      error

	capturedComposeFile string
	capturedProfiles    []string
}

func (f *fakeCompose) Up(_ context.Context, composeFile string, profiles []string) error {
	f.capturedComposeFile = composeFile
	f.capturedProfiles = profiles
	return f.upErr
}

func (f *fakeCompose) Version(_ context.Context) (string, error) {
	return f.versionStr, f.versionErr
}

func (f *fakeCompose) Info(_ context.Context) error {
	return f.infoErr
}

func (f *fakeCompose) PS(_ context.Context, _ string) ([]ports.ContainerInfo, error) {
	return f.psResult, f.psErr
}

// fakeGenerator is a test double for ports.ConfigGenerator.
type fakeGenerator struct {
	generateErr       error
	capturedOutputDir string
	generateCalled    bool
}

func (f *fakeGenerator) Generate(_ context.Context, _ *config.Config, outputDir string) error {
	f.generateCalled = true
	f.capturedOutputDir = outputDir
	return f.generateErr
}

func defaultConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8443},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		TLS:      config.TLSConfig{Enabled: true, Provider: "self-signed"},
		RateLimit: config.RateLimitConfig{
			Enabled: true,
			PerIP:   config.RateLimitRuleConfig{RequestsPerSecond: 10, Burst: 20},
		},
		Metrics: config.MetricsConfig{Enabled: true},
		Kratos:  config.KratosConfig{PublicURL: "http://127.0.0.1:4433", AdminURL: "http://127.0.0.1:4434"},
	}
}

func TestDevService_Run(t *testing.T) {
	tests := []struct {
		name               string
		opts               ops.DevOptions
		upErr              error
		wantErr            bool
		wantProfiles       []string
		wantOutputContains []string
	}{
		{
			name:         "baseline stack — no observability",
			opts:         ops.DevOptions{Observability: false},
			wantErr:      false,
			wantProfiles: nil,
			wantOutputContains: []string{
				"Proxy (VibeWarden):",
				"https://localhost:8443",
				"vibewarden status",
			},
		},
		{
			name:         "observability profile enabled",
			opts:         ops.DevOptions{Observability: true},
			wantErr:      false,
			wantProfiles: []string{"observability"},
			wantOutputContains: []string{
				"Prometheus:",
				"Grafana:",
				"Observability profile enabled",
			},
		},
		{
			name:    "docker compose up returns error",
			opts:    ops.DevOptions{},
			upErr:   errors.New("docker not running"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &fakeCompose{upErr: tt.upErr}
			svc := ops.NewDevService(fc)
			cfg := defaultConfig()
			var buf bytes.Buffer

			err := svc.Run(context.Background(), cfg, tt.opts, &buf)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				out := buf.String()
				for _, want := range tt.wantOutputContains {
					if !strings.Contains(out, want) {
						t.Errorf("output missing %q\ngot:\n%s", want, out)
					}
				}

				// Check profiles forwarded to compose
				if len(tt.wantProfiles) == 0 && len(fc.capturedProfiles) != 0 {
					t.Errorf("expected no profiles, got %v", fc.capturedProfiles)
				}
				for i, p := range tt.wantProfiles {
					if i >= len(fc.capturedProfiles) || fc.capturedProfiles[i] != p {
						t.Errorf("profile[%d] = %q, want %q", i, fc.capturedProfiles[i], p)
					}
				}
			}
		})
	}
}

func TestDevService_WithGenerator_CallsGenerateBeforeUp(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if !fg.generateCalled {
		t.Error("expected Generate() to be called, but it was not")
	}
	if fg.capturedOutputDir != ".vibewarden/generated" {
		t.Errorf("Generate() called with outputDir=%q, want %q", fg.capturedOutputDir, ".vibewarden/generated")
	}
}

func TestDevService_WithGenerator_PassesGeneratedComposeFilePath(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	wantComposeFile := ".vibewarden/generated/docker-compose.yml"
	if fc.capturedComposeFile != wantComposeFile {
		t.Errorf("Up() called with composeFile=%q, want %q", fc.capturedComposeFile, wantComposeFile)
	}
}

func TestDevService_WithGenerator_GenerateError_ReturnsError(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{generateErr: errors.New("template render failed")}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf)
	if err == nil {
		t.Fatal("Run() expected error when Generate() fails, got nil")
	}
}

func TestDevService_WithoutGenerator_UsesEmptyComposeFile(t *testing.T) {
	// Without a generator, Up should be called with an empty composeFile so
	// that docker compose uses its default discovery behaviour.
	fc := &fakeCompose{}
	svc := ops.NewDevService(fc)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	if fc.capturedComposeFile != "" {
		t.Errorf("Up() called with composeFile=%q, want empty string for backward compat", fc.capturedComposeFile)
	}
}

func TestDevService_WithGenerator_PrintsGeneratedOutputMessage(t *testing.T) {
	fc := &fakeCompose{}
	fg := &fakeGenerator{}
	svc := ops.NewDevServiceWithGenerator(fc, fg)
	cfg := defaultConfig()
	var buf bytes.Buffer

	if err := svc.Run(context.Background(), cfg, ops.DevOptions{}, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, ".vibewarden/generated") {
		t.Errorf("expected output to mention generated dir, got:\n%s", out)
	}
}
