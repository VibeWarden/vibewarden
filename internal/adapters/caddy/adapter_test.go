package caddy

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/site"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeEventLogger is a spy that captures all events emitted through it.
// It implements ports.EventLogger without any real I/O.
type fakeEventLogger struct {
	logged []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.logged = append(f.logged, ev)
	return nil
}

func TestNewAdapter(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	logger := slog.Default()

	adapter := NewAdapter(cfg, logger, nil)

	if adapter == nil {
		t.Fatal("NewAdapter() returned nil")
	}
	if adapter.config != cfg {
		t.Error("NewAdapter() did not set config correctly")
	}
	if adapter.logger != logger {
		t.Error("NewAdapter() did not set logger correctly")
	}
}

func TestNewAdapter_WithEventLogger(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	spy := &fakeEventLogger{}

	adapter := NewAdapter(cfg, slog.Default(), spy)

	if adapter == nil {
		t.Fatal("NewAdapter() returned nil")
	}
	if adapter.eventLogger != spy {
		t.Error("NewAdapter() did not set eventLogger correctly")
	}
}

func TestAdapter_BuildConfigJSON(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ports.ProxyConfig
		wantErr bool
	}{
		{
			name: "valid local config",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantErr: false,
		},
		{
			name: "valid config with security headers",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				SecurityHeaders: ports.SecurityHeadersConfig{
					Enabled:            true,
					ContentTypeNosniff: true,
					FrameOption:        "DENY",
				},
			},
			wantErr: false,
		},
		{
			name: "missing listen addr produces error",
			cfg: &ports.ProxyConfig{
				UpstreamAddr: "127.0.0.1:3000",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := NewAdapter(tt.cfg, slog.Default(), nil)

			data, err := adapter.buildConfigJSON()
			if (err != nil) != tt.wantErr {
				t.Errorf("buildConfigJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(data) == 0 {
				t.Error("buildConfigJSON() returned empty data")
			}
		})
	}
}

func TestNewMultiSiteAdapter(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	registry := site.NewRegistry()
	logger := slog.Default()

	adapter := NewMultiSiteAdapter(cfg, registry, logger, nil)

	if adapter == nil {
		t.Fatal("NewMultiSiteAdapter() returned nil")
	}
	if adapter.config != cfg {
		t.Error("NewMultiSiteAdapter() did not set config correctly")
	}
	if adapter.registry != registry {
		t.Error("NewMultiSiteAdapter() did not set registry correctly")
	}
	if adapter.logger != logger {
		t.Error("NewMultiSiteAdapter() did not set logger correctly")
	}
}

func TestNewMultiSiteAdapter_WithEventLogger(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	registry := site.NewRegistry()
	spy := &fakeEventLogger{}

	adapter := NewMultiSiteAdapter(cfg, registry, slog.Default(), spy)

	if adapter == nil {
		t.Fatal("NewMultiSiteAdapter() returned nil")
	}
	if adapter.eventLogger != spy {
		t.Error("NewMultiSiteAdapter() did not set eventLogger correctly")
	}
}

func TestAdapter_BuildConfigJSON_MultiSiteMode(t *testing.T) {
	registry := site.NewRegistry()
	global := site.DefaultGlobalConfig()
	registry.SetGlobal(global)

	siteCfg := &config.Config{
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: 3001,
		},
		TLS: config.TLSConfig{
			Enabled:  true,
			Provider: "letsencrypt",
			Domain:   "app1.example.com",
		},
	}
	s, err := site.NewSite("app1", "/tmp/app1/vibewarden.yaml", siteCfg)
	if err != nil {
		t.Fatalf("NewSite() error = %v", err)
	}
	registry.Add(s)

	proxyCfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
	}
	adapter := NewMultiSiteAdapter(proxyCfg, registry, slog.Default(), nil)

	data, err := adapter.buildConfigJSON()
	if err != nil {
		t.Fatalf("buildConfigJSON() in multi-site mode error = %v", err)
	}
	if len(data) == 0 {
		t.Error("buildConfigJSON() returned empty data in multi-site mode")
	}
}

func TestAdapter_BuildConfigJSON_BackwardCompat_NoRegistry(t *testing.T) {
	// When registry is nil, the adapter uses single-site mode (BuildCaddyConfig).
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	adapter := NewAdapter(cfg, slog.Default(), nil)

	data, err := adapter.buildConfigJSON()
	if err != nil {
		t.Fatalf("buildConfigJSON() in single-site mode error = %v", err)
	}
	if len(data) == 0 {
		t.Error("buildConfigJSON() returned empty data in single-site mode")
	}

	// Verify the adapter does NOT have a registry set.
	if adapter.registry != nil {
		t.Error("NewAdapter() should not set a registry")
	}
}

func TestAdapter_BuildConfigJSON_MultiSite_DefaultGlobal(t *testing.T) {
	// When the registry has no explicit global config, defaults should be used.
	registry := site.NewRegistry()
	// Do NOT call registry.SetGlobal() — test the fallback path.

	siteCfg := &config.Config{
		Upstream: config.UpstreamConfig{
			Host: "127.0.0.1",
			Port: 3001,
		},
		TLS: config.TLSConfig{
			Enabled:  true,
			Provider: "letsencrypt",
			Domain:   "app.example.com",
		},
	}
	s, err := site.NewSite("app", "/tmp/app/vibewarden.yaml", siteCfg)
	if err != nil {
		t.Fatalf("NewSite() error = %v", err)
	}
	registry.Add(s)

	proxyCfg := &ports.ProxyConfig{
		ListenAddr:   "0.0.0.0:443",
		UpstreamAddr: "127.0.0.1:3000",
	}
	adapter := NewMultiSiteAdapter(proxyCfg, registry, slog.Default(), nil)

	data, err := adapter.buildConfigJSON()
	if err != nil {
		t.Fatalf("buildConfigJSON() with default global error = %v", err)
	}
	if len(data) == 0 {
		t.Error("buildConfigJSON() returned empty data with default global")
	}
}

func TestAdapter_StopWithoutStart(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	adapter := NewAdapter(cfg, slog.Default(), nil)

	// Stopping without starting should not panic (Caddy handles this gracefully).
	// Caddy returns nil when Stop is called on an unstarted instance.
	err := adapter.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() on unstarted adapter returned unexpected error: %v", err)
	}
}
