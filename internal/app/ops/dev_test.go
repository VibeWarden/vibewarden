package ops_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// fakeCompose is a test double for ports.ComposeRunner.
type fakeCompose struct {
	upErr      error
	versionStr string
	versionErr error
	infoErr    error

	capturedProfiles []string
}

func (f *fakeCompose) Up(_ context.Context, profiles []string) error {
	f.capturedProfiles = profiles
	return f.upErr
}

func (f *fakeCompose) Version(_ context.Context) (string, error) {
	return f.versionStr, f.versionErr
}

func (f *fakeCompose) Info(_ context.Context) error {
	return f.infoErr
}

func defaultConfig() *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
		TLS:      config.TLSConfig{Enabled: false, Provider: "self-signed"},
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
		name            string
		opts            ops.DevOptions
		upErr           error
		wantErr         bool
		wantProfiles    []string
		wantOutputContains []string
	}{
		{
			name:         "baseline stack — no observability",
			opts:         ops.DevOptions{Observability: false},
			wantErr:      false,
			wantProfiles: nil,
			wantOutputContains: []string{
				"Proxy (VibeWarden):",
				"http://localhost:8080",
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
