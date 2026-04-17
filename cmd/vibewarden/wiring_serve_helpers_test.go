package main

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestResolveCSP(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "raw string takes precedence over structured config",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "default-src 'self'; script-src 'none'",
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'", "https://cdn.example.com"},
					},
				},
			},
			want: "default-src 'self'; script-src 'none'",
		},
		{
			name: "structured config used when raw string is empty",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "",
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'"},
						ScriptSrc:  []string{"'self'", "https://cdn.example.com"},
						StyleSrc:   []string{"'self'", "'unsafe-inline'"},
						ImgSrc:     []string{"'self'", "data:"},
						ConnectSrc: []string{"'self'"},
						FontSrc:    []string{"'self'"},
						FrameSrc:   []string{"'none'"},
					},
				},
			},
			want: "default-src 'self'; script-src 'self' https://cdn.example.com; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-src 'none'",
		},
		{
			name: "both empty produces empty string",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "",
					CSP:                   config.CSPConfig{},
				},
			},
			want: "",
		},
		{
			name: "raw string only — no structured config",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					ContentSecurityPolicy: "default-src 'none'",
				},
			},
			want: "default-src 'none'",
		},
		{
			name: "structured config only — default-src self",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					CSP: config.CSPConfig{
						DefaultSrc: []string{"'self'"},
					},
				},
			},
			want: "default-src 'self'",
		},
		{
			name: "all structured directives",
			cfg: &config.Config{
				SecurityHeaders: config.SecurityHeadersConfig{
					CSP: config.CSPConfig{
						DefaultSrc:     []string{"'self'"},
						ScriptSrc:      []string{"'self'"},
						StyleSrc:       []string{"'self'"},
						ImgSrc:         []string{"'self'"},
						ConnectSrc:     []string{"'self'"},
						FontSrc:        []string{"'self'"},
						FrameSrc:       []string{"'none'"},
						MediaSrc:       []string{"'self'"},
						ObjectSrc:      []string{"'none'"},
						ManifestSrc:    []string{"'self'"},
						WorkerSrc:      []string{"'self'"},
						ChildSrc:       []string{"'self'"},
						FormAction:     []string{"'self'"},
						FrameAncestors: []string{"'none'"},
						BaseURI:        []string{"'self'"},
					},
				},
			},
			want: "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; font-src 'self'; frame-src 'none'; media-src 'self'; object-src 'none'; manifest-src 'self'; worker-src 'self'; child-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveCSP(tt.cfg)
			if got != tt.want {
				t.Errorf("resolveCSP() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}

func TestBuildServerTimeoutsConfig(t *testing.T) {
	tests := []struct {
		name      string
		serverCfg config.ServerConfig
		wantRead  time.Duration
		wantWrite time.Duration
		wantIdle  time.Duration
	}{
		{
			name:      "empty strings use defaults",
			serverCfg: config.ServerConfig{},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "explicit zero disables timeout",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "0",
				WriteTimeout: "0",
				IdleTimeout:  "0",
			},
			wantRead:  0,
			wantWrite: 0,
			wantIdle:  0,
		},
		{
			name: "custom valid durations are parsed",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "10s",
				WriteTimeout: "20s",
				IdleTimeout:  "90s",
			},
			wantRead:  10 * time.Second,
			wantWrite: 20 * time.Second,
			wantIdle:  90 * time.Second,
		},
		{
			name: "invalid read timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "notaduration",
				WriteTimeout: "60s",
				IdleTimeout:  "120s",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "invalid write timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "30s",
				WriteTimeout: "bad",
				IdleTimeout:  "120s",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "invalid idle timeout falls back to default",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "30s",
				WriteTimeout: "60s",
				IdleTimeout:  "bad",
			},
			wantRead:  30 * time.Second,
			wantWrite: 60 * time.Second,
			wantIdle:  120 * time.Second,
		},
		{
			name: "minute durations are accepted",
			serverCfg: config.ServerConfig{
				ReadTimeout:  "1m",
				WriteTimeout: "2m",
				IdleTimeout:  "5m",
			},
			wantRead:  time.Minute,
			wantWrite: 2 * time.Minute,
			wantIdle:  5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Server: tt.serverCfg}
			got := buildServerTimeoutsConfig(cfg)
			if got.ReadTimeout != tt.wantRead {
				t.Errorf("ReadTimeout = %v, want %v", got.ReadTimeout, tt.wantRead)
			}
			if got.WriteTimeout != tt.wantWrite {
				t.Errorf("WriteTimeout = %v, want %v", got.WriteTimeout, tt.wantWrite)
			}
			if got.IdleTimeout != tt.wantIdle {
				t.Errorf("IdleTimeout = %v, want %v", got.IdleTimeout, tt.wantIdle)
			}
		})
	}
}
