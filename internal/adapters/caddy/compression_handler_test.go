package caddy

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestBuildCompressionHandlerJSON(t *testing.T) {
	tests := []struct {
		name           string
		cfg            ports.CompressionConfig
		wantHandler    string
		wantAlgorithms []string
	}{
		{
			name: "default algorithms when empty",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: nil,
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{"zstd", "gzip"},
		},
		{
			name: "gzip only",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: []string{"gzip"},
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{"gzip"},
		},
		{
			name: "zstd only",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: []string{"zstd"},
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{"zstd"},
		},
		{
			name: "gzip and zstd explicit order",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: []string{"gzip", "zstd"},
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{"gzip", "zstd"},
		},
		{
			name: "unknown algorithm is ignored",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: []string{"br", "gzip"},
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{"gzip"},
		},
		{
			name: "all unknown algorithms produce empty encodings",
			cfg: ports.CompressionConfig{
				Enabled:    true,
				Algorithms: []string{"br", "deflate"},
			},
			wantHandler:    "encode",
			wantAlgorithms: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildCompressionHandlerJSON(tt.cfg)

			handler, _ := result["handler"].(string)
			if handler != tt.wantHandler {
				t.Errorf("handler = %q, want %q", handler, tt.wantHandler)
			}

			encodings, ok := result["encodings"].(map[string]any)
			if !ok {
				t.Fatal("encodings not found or wrong type in result")
			}

			// Verify each expected algorithm key is present.
			for _, algo := range tt.wantAlgorithms {
				if _, found := encodings[algo]; !found {
					t.Errorf("expected algorithm %q not found in encodings", algo)
				}
			}

			// Verify no extra algorithm keys are present.
			if len(encodings) != len(tt.wantAlgorithms) {
				t.Errorf("encodings has %d entries, want %d", len(encodings), len(tt.wantAlgorithms))
			}
		})
	}
}

func TestBuildCaddyConfig_Compression(t *testing.T) {
	tests := []struct {
		name              string
		cfg               *ports.ProxyConfig
		wantEncodeHandler bool
		wantAlgorithms    []string
	}{
		{
			name: "compression enabled with defaults produces encode handler",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Compression: ports.CompressionConfig{
					Enabled:    true,
					Algorithms: []string{"zstd", "gzip"},
				},
			},
			wantEncodeHandler: true,
			wantAlgorithms:    []string{"zstd", "gzip"},
		},
		{
			name: "compression disabled omits encode handler",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Compression: ports.CompressionConfig{
					Enabled: false,
				},
			},
			wantEncodeHandler: false,
		},
		{
			name: "compression enabled gzip only",
			cfg: &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Compression: ports.CompressionConfig{
					Enabled:    true,
					Algorithms: []string{"gzip"},
				},
			},
			wantEncodeHandler: true,
			wantAlgorithms:    []string{"gzip"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildCaddyConfig(tt.cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() unexpected error: %v", err)
			}

			handlers := extractCatchAllHandlers(t, result)

			encodeHandler := findHandlerByType(handlers, "encode")
			if tt.wantEncodeHandler && encodeHandler == nil {
				t.Fatal("expected encode handler in catch-all route, but not found")
			}
			if !tt.wantEncodeHandler && encodeHandler != nil {
				t.Fatal("unexpected encode handler found in catch-all route")
			}

			if encodeHandler == nil {
				return
			}

			encodings, ok := encodeHandler["encodings"].(map[string]any)
			if !ok {
				t.Fatal("encodings not found or wrong type in encode handler")
			}

			for _, algo := range tt.wantAlgorithms {
				if _, found := encodings[algo]; !found {
					t.Errorf("expected algorithm %q not found in encodings", algo)
				}
			}
		})
	}
}

// extractCatchAllHandlers returns the handlers slice from the catch-all route
// (the last route in the vibewarden server routes list).
func extractCatchAllHandlers(t *testing.T, result map[string]any) []map[string]any {
	t.Helper()

	server := extractServer(t, result)

	routes, ok := server["routes"].([]map[string]any)
	if !ok {
		t.Fatal("routes not found or wrong type in server config")
	}
	if len(routes) == 0 {
		t.Fatal("no routes in server config")
	}

	// The catch-all route is always last.
	catchAll := routes[len(routes)-1]
	rawHandlers, ok := catchAll["handle"].([]map[string]any)
	if !ok {
		t.Fatal("handle not found or wrong type in catch-all route")
	}

	return rawHandlers
}

// findHandlerByType returns the first handler map with the given "handler" value,
// or nil if not found.
func findHandlerByType(handlers []map[string]any, handlerType string) map[string]any {
	for _, h := range handlers {
		if h["handler"] == handlerType {
			return h
		}
	}
	return nil
}
