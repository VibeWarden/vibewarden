package main

import (
	"testing"

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
