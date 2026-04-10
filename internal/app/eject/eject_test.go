package eject_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/eject"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeBuilder is a test double for eject.ConfigBuilder.
type fakeBuilder struct {
	called bool
	got    *ports.ProxyConfig
	result map[string]any
	err    error
}

func (f *fakeBuilder) Build(cfg *ports.ProxyConfig) (map[string]any, error) {
	f.called = true
	f.got = cfg
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return map[string]any{"apps": map[string]any{}}, nil
}

func minimalConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: 3000,
		},
		TLS: config.TLSConfig{
			Provider: "self-signed",
		},
	}
}

func TestService_Eject_NilConfig(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(nil)
	if err == nil {
		t.Fatal("expected error for nil config, got nil")
	}
}

func TestService_Eject_BuilderError(t *testing.T) {
	b := &fakeBuilder{err: errors.New("build failed")}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig())
	if err == nil {
		t.Fatal("expected error when builder fails, got nil")
	}
}

func TestService_Eject_ReturnsBuilderResult(t *testing.T) {
	want := map[string]any{"apps": map[string]any{"http": "ok"}}
	b := &fakeBuilder{result: want}
	svc := eject.NewService(b)

	got, err := svc.Eject(minimalConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestService_Eject_CallsBuilder(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	cfg := minimalConfig()
	_, err := svc.Eject(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !b.called {
		t.Error("expected builder.Build to be called, was not")
	}
}

func TestService_Eject_ProxyConfigListenAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		wantAddr string
	}{
		{"localhost default", "127.0.0.1", 8080, "127.0.0.1:8080"},
		{"all interfaces", "0.0.0.0", 443, "0.0.0.0:443"},
		{"custom host", "10.0.0.1", 9000, "10.0.0.1:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.Server.Host = tt.host
			cfg.Server.Port = tt.port

			_, err := svc.Eject(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if b.got == nil {
				t.Fatal("expected builder.Build to be called with a ProxyConfig")
			}

			if b.got.ListenAddr != tt.wantAddr {
				t.Errorf("ListenAddr = %q, want %q", b.got.ListenAddr, tt.wantAddr)
			}
		})
	}
}

func TestService_Eject_ProxyConfigUpstreamAddr(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		wantAddr string
	}{
		{"default upstream", "127.0.0.1", 3000, "127.0.0.1:3000"},
		{"external upstream", "app.internal", 8000, "app.internal:8000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.Upstream.Host = tt.host
			cfg.Upstream.Port = tt.port

			_, err := svc.Eject(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if b.got.UpstreamAddr != tt.wantAddr {
				t.Errorf("UpstreamAddr = %q, want %q", b.got.UpstreamAddr, tt.wantAddr)
			}
		})
	}
}

func TestService_Eject_TLSConfig(t *testing.T) {
	tests := []struct {
		name         string
		tlsCfg       config.TLSConfig
		wantEnabled  bool
		wantProvider ports.TLSProvider
		wantDomain   string
	}{
		{
			name:        "tls disabled",
			tlsCfg:      config.TLSConfig{Enabled: false, Provider: "self-signed"},
			wantEnabled: false,
		},
		{
			name:         "self-signed",
			tlsCfg:       config.TLSConfig{Enabled: true, Provider: "self-signed", Domain: "localhost"},
			wantEnabled:  true,
			wantProvider: ports.TLSProviderSelfSigned,
			wantDomain:   "localhost",
		},
		{
			name:         "letsencrypt",
			tlsCfg:       config.TLSConfig{Enabled: true, Provider: "letsencrypt", Domain: "example.com"},
			wantEnabled:  true,
			wantProvider: ports.TLSProviderLetsEncrypt,
			wantDomain:   "example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &fakeBuilder{}
			svc := eject.NewService(b)

			cfg := minimalConfig()
			cfg.TLS = tt.tlsCfg

			_, err := svc.Eject(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			got := b.got.TLS
			if got.Enabled != tt.wantEnabled {
				t.Errorf("TLS.Enabled = %v, want %v", got.Enabled, tt.wantEnabled)
			}
			if tt.wantEnabled {
				if got.Provider != tt.wantProvider {
					t.Errorf("TLS.Provider = %q, want %q", got.Provider, tt.wantProvider)
				}
				if got.Domain != tt.wantDomain {
					t.Errorf("TLS.Domain = %q, want %q", got.Domain, tt.wantDomain)
				}
			}
		})
	}
}

func TestService_Eject_VersionIsEjected(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.got.Version != "ejected" {
		t.Errorf("Version = %q, want %q", b.got.Version, "ejected")
	}
}

func TestService_Eject_InternalAddrsOmitted(t *testing.T) {
	b := &fakeBuilder{}
	svc := eject.NewService(b)

	_, err := svc.Eject(minimalConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if b.got.Metrics.InternalAddr != "" {
		t.Errorf("Metrics.InternalAddr should be empty, got %q", b.got.Metrics.InternalAddr)
	}
	if b.got.Admin.InternalAddr != "" {
		t.Errorf("Admin.InternalAddr should be empty, got %q", b.got.Admin.InternalAddr)
	}
	if b.got.Readiness.InternalAddr != "" {
		t.Errorf("Readiness.InternalAddr should be empty, got %q", b.got.Readiness.InternalAddr)
	}
}

func TestErrUnsupportedFormat_Error(t *testing.T) {
	err := eject.ErrUnsupportedFormat{Format: eject.Format("nginx")}
	msg := err.Error()

	if msg == "" {
		t.Error("expected non-empty error message")
	}

	want := "nginx"
	if len(msg) < len(want) {
		t.Errorf("error message %q does not mention format", msg)
	}
}
