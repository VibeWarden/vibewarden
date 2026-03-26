package caddy

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
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

func TestAdapter_StopWithoutStart(t *testing.T) {
	cfg := &ports.ProxyConfig{
		ListenAddr:   "127.0.0.1:8080",
		UpstreamAddr: "127.0.0.1:3000",
	}
	adapter := NewAdapter(cfg, slog.Default(), nil)

	// Stopping without starting should not panic (Caddy handles this gracefully)
	err := adapter.Stop(context.Background())
	// We don't assert on err here since Caddy may return an error
	// when stopping without having been started — this is acceptable.
	_ = err
}
