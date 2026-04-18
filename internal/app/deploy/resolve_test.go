package deploy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestResolveProdConfig(t *testing.T) {
	tests := []struct {
		name       string
		cfg        *config.Config
		project    string
		multiSite  bool
		wantHost   string
		wantMutate bool // if true, verify original cfg is not modified
	}{
		{
			name: "multi-site: 0.0.0.0 rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "vibewarden-blog-app",
		},
		{
			name: "multi-site: 127.0.0.1 rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "127.0.0.1", Port: 3000},
			},
			project:   "api",
			multiSite: true,
			wantHost:  "vibewarden-api-app",
		},
		{
			name: "multi-site: localhost rewritten to container name",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 8080},
			},
			project:   "shop",
			multiSite: true,
			wantHost:  "vibewarden-shop-app",
		},
		{
			name: "multi-site: custom host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "my-backend", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "my-backend",
		},
		{
			name: "multi-site: empty host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "", Port: 3000},
			},
			project:   "blog",
			multiSite: true,
			wantHost:  "",
		},
		{
			name: "single-site with image: 0.0.0.0 rewritten to app",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "app",
		},
		{
			name: "single-site with build: localhost rewritten to app",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
				App:      config.AppConfig{Build: "."},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "app",
		},
		{
			name: "single-site without container: localhost NOT rewritten",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "localhost", Port: 3000},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "localhost",
		},
		{
			name: "single-site: custom host preserved",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "my-custom-host", Port: 3000},
				App:      config.AppConfig{Image: "myapp:latest"},
			},
			project:   "myproject",
			multiSite: false,
			wantHost:  "my-custom-host",
		},
		{
			name: "does not mutate original config",
			cfg: &config.Config{
				Upstream: config.UpstreamConfig{Host: "0.0.0.0", Port: 3000},
			},
			project:    "blog",
			multiSite:  true,
			wantHost:   "vibewarden-blog-app",
			wantMutate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalHost := tt.cfg.Upstream.Host

			got := ResolveProdConfig(tt.cfg, tt.project, tt.multiSite)

			if got.Upstream.Host != tt.wantHost {
				t.Errorf("ResolveProdConfig().Upstream.Host = %q, want %q", got.Upstream.Host, tt.wantHost)
			}

			if tt.wantMutate && tt.cfg.Upstream.Host != originalHost {
				t.Errorf("ResolveProdConfig mutated original config: Host = %q, was %q", tt.cfg.Upstream.Host, originalHost)
			}

			// Verify port is preserved.
			if got.Upstream.Port != tt.cfg.Upstream.Port {
				t.Errorf("ResolveProdConfig().Upstream.Port = %d, want %d", got.Upstream.Port, tt.cfg.Upstream.Port)
			}
		})
	}
}

func TestMarshalConfig(t *testing.T) {
	cfg := &config.Config{
		Server:   config.ServerConfig{Port: 8443},
		Upstream: config.UpstreamConfig{Host: "app", Port: 3000},
		App:      config.AppConfig{Image: "myapp:latest"},
	}

	data, err := MarshalConfig(cfg)
	if err != nil {
		t.Fatalf("MarshalConfig() error = %v", err)
	}

	if len(data) == 0 {
		t.Fatal("MarshalConfig() returned empty data")
	}

	// Verify the output contains expected YAML keys.
	s := string(data)
	if !contains(s, "port: 8443") {
		t.Errorf("MarshalConfig() output should contain 'port: 8443', got:\n%s", s)
	}
	if !contains(s, "host: app") {
		t.Errorf("MarshalConfig() output should contain 'host: app', got:\n%s", s)
	}
}

func TestMarshalConfig_NilConfig(t *testing.T) {
	// Marshalling a nil config should not error (produces empty YAML).
	data, err := MarshalConfig(&config.Config{})
	if err != nil {
		t.Fatalf("MarshalConfig(&Config{}) error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("MarshalConfig(&Config{}) returned empty data")
	}
}

// contains checks whether s contains substr. Using a local helper to avoid
// importing strings in an internal test file.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
