package csp_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/csp"
)

func TestBuild(t *testing.T) {
	tests := []struct {
		name string
		cfg  csp.Config
		want string
	}{
		{
			name: "empty config produces empty string",
			cfg:  csp.Config{},
			want: "",
		},
		{
			name: "default-src only",
			cfg: csp.Config{
				DefaultSrc: []string{"'self'"},
			},
			want: "default-src 'self'",
		},
		{
			name: "default-src with multiple sources",
			cfg: csp.Config{
				DefaultSrc: []string{"'self'", "https://cdn.example.com"},
			},
			want: "default-src 'self' https://cdn.example.com",
		},
		{
			name: "script-src with unsafe-inline",
			cfg: csp.Config{
				ScriptSrc: []string{"'self'", "'unsafe-inline'"},
			},
			want: "script-src 'self' 'unsafe-inline'",
		},
		{
			name: "none as single source",
			cfg: csp.Config{
				FrameSrc: []string{"'none'"},
			},
			want: "frame-src 'none'",
		},
		{
			name: "full config from issue example",
			cfg: csp.Config{
				DefaultSrc: []string{"'self'"},
				ScriptSrc:  []string{"'self'", "https://cdn.example.com"},
				StyleSrc:   []string{"'self'", "'unsafe-inline'"},
				ImgSrc:     []string{"'self'", "data:"},
				ConnectSrc: []string{"'self'"},
				FontSrc:    []string{"'self'"},
				FrameSrc:   []string{"'none'"},
			},
			want: "default-src 'self'; script-src 'self' https://cdn.example.com; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; frame-src 'none'",
		},
		{
			name: "directive order is deterministic — default-src first",
			cfg: csp.Config{
				FontSrc:    []string{"'self'"},
				DefaultSrc: []string{"'self'"},
				ScriptSrc:  []string{"'none'"},
			},
			want: "default-src 'self'; script-src 'none'; font-src 'self'",
		},
		{
			name: "media-src directive",
			cfg: csp.Config{
				MediaSrc: []string{"'self'", "https://media.example.com"},
			},
			want: "media-src 'self' https://media.example.com",
		},
		{
			name: "object-src none",
			cfg: csp.Config{
				ObjectSrc: []string{"'none'"},
			},
			want: "object-src 'none'",
		},
		{
			name: "manifest-src directive",
			cfg: csp.Config{
				ManifestSrc: []string{"'self'"},
			},
			want: "manifest-src 'self'",
		},
		{
			name: "worker-src directive",
			cfg: csp.Config{
				WorkerSrc: []string{"'self'", "blob:"},
			},
			want: "worker-src 'self' blob:",
		},
		{
			name: "child-src directive",
			cfg: csp.Config{
				ChildSrc: []string{"'self'"},
			},
			want: "child-src 'self'",
		},
		{
			name: "form-action directive",
			cfg: csp.Config{
				FormAction: []string{"'self'"},
			},
			want: "form-action 'self'",
		},
		{
			name: "frame-ancestors directive",
			cfg: csp.Config{
				FrameAncestors: []string{"'none'"},
			},
			want: "frame-ancestors 'none'",
		},
		{
			name: "base-uri directive",
			cfg: csp.Config{
				BaseURI: []string{"'self'"},
			},
			want: "base-uri 'self'",
		},
		{
			name: "all directives populated",
			cfg: csp.Config{
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
			want: "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; font-src 'self'; frame-src 'none'; media-src 'self'; object-src 'none'; manifest-src 'self'; worker-src 'self'; child-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'",
		},
		{
			name: "omitted directives are not emitted",
			cfg: csp.Config{
				DefaultSrc: []string{"'self'"},
				// ScriptSrc intentionally omitted — should not appear in output
				StyleSrc: []string{"'unsafe-inline'"},
			},
			want: "default-src 'self'; style-src 'unsafe-inline'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := csp.Build(tt.cfg)
			if got != tt.want {
				t.Errorf("Build() =\n  %q\nwant:\n  %q", got, tt.want)
			}
		})
	}
}
