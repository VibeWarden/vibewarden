package egress_test

import (
	"net/http"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestHeadersConfig_ApplyToRequest_InjectHeaders(t *testing.T) {
	tests := []struct {
		name        string
		cfg         egress.HeadersConfig
		incoming    http.Header
		wantPresent map[string]string
		wantAbsent  []string
	}{
		{
			name: "injects new header",
			cfg: egress.HeadersConfig{
				InjectHeaders: map[string]string{"X-Api-Key": "secret123"},
			},
			incoming:    http.Header{},
			wantPresent: map[string]string{"X-Api-Key": "secret123"},
		},
		{
			name: "overwrites existing header",
			cfg: egress.HeadersConfig{
				InjectHeaders: map[string]string{"Authorization": "Bearer new-token"},
			},
			incoming:    http.Header{"Authorization": []string{"Bearer old-token"}},
			wantPresent: map[string]string{"Authorization": "Bearer new-token"},
		},
		{
			name: "strips request headers",
			cfg: egress.HeadersConfig{
				StripRequestHeaders: []string{"Cookie", "X-Internal-Token"},
			},
			incoming: http.Header{
				"Cookie":           []string{"session=abc"},
				"X-Internal-Token": []string{"private"},
				"X-Keep":           []string{"yes"},
			},
			wantAbsent:  []string{"Cookie", "X-Internal-Token"},
			wantPresent: map[string]string{"X-Keep": "yes"},
		},
		{
			name: "always strips X-Inject-Secret",
			cfg:  egress.HeadersConfig{},
			incoming: http.Header{
				"X-Inject-Secret": []string{"my-secret"},
				"X-Normal":        []string{"keep"},
			},
			wantAbsent:  []string{"X-Inject-Secret"},
			wantPresent: map[string]string{"X-Normal": "keep"},
		},
		{
			name: "inject then strip order: inject first strip second",
			cfg: egress.HeadersConfig{
				InjectHeaders:       map[string]string{"X-Foo": "injected"},
				StripRequestHeaders: []string{"X-Foo"},
			},
			incoming:   http.Header{},
			wantAbsent: []string{"X-Foo"},
		},
		{
			name: "does not mutate original header",
			cfg: egress.HeadersConfig{
				InjectHeaders:       map[string]string{"X-Added": "yes"},
				StripRequestHeaders: []string{"X-Remove"},
			},
			incoming: http.Header{
				"X-Remove": []string{"value"},
			},
			wantPresent: map[string]string{"X-Added": "yes"},
			wantAbsent:  []string{"X-Remove"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Snapshot original to verify no mutation.
			original := tt.incoming.Clone()

			got := tt.cfg.ApplyToRequest(tt.incoming)

			// Verify original is untouched — check that X-Inject-Secret was not deleted from original.
			if tt.incoming.Get("X-Inject-Secret") != original.Get("X-Inject-Secret") {
				t.Error("ApplyToRequest mutated the original header map")
			}

			for k, want := range tt.wantPresent {
				if v := got.Get(k); v != want {
					t.Errorf("got[%q] = %q, want %q", k, v, want)
				}
			}
			for _, k := range tt.wantAbsent {
				if v := got.Get(k); v != "" {
					t.Errorf("got[%q] = %q, want absent", k, v)
				}
			}
		})
	}
}

func TestHeadersConfig_ApplyToResponse_StripSensitive(t *testing.T) {
	tests := []struct {
		name        string
		cfg         egress.HeadersConfig
		incoming    http.Header
		wantPresent map[string]string
		wantAbsent  []string
	}{
		{
			name: "strips Server header by default",
			cfg:  egress.HeadersConfig{},
			incoming: http.Header{
				"Server":         []string{"Apache/2.4"},
				"Content-Length": []string{"42"},
			},
			wantAbsent:  []string{"Server"},
			wantPresent: map[string]string{"Content-Length": "42"},
		},
		{
			name: "strips X-Powered-By by default",
			cfg:  egress.HeadersConfig{},
			incoming: http.Header{
				"X-Powered-By": []string{"PHP/8.1"},
				"X-Keep":       []string{"yes"},
			},
			wantAbsent:  []string{"X-Powered-By"},
			wantPresent: map[string]string{"X-Keep": "yes"},
		},
		{
			name: "strips per-route response headers",
			cfg: egress.HeadersConfig{
				StripResponseHeaders: []string{"X-Custom-Internal"},
			},
			incoming: http.Header{
				"X-Custom-Internal": []string{"leak"},
				"X-Rate-Limit":      []string{"100"},
			},
			wantAbsent:  []string{"X-Custom-Internal"},
			wantPresent: map[string]string{"X-Rate-Limit": "100"},
		},
		{
			name: "strips both default and per-route response headers",
			cfg: egress.HeadersConfig{
				StripResponseHeaders: []string{"X-Internal"},
			},
			incoming: http.Header{
				"Server":     []string{"nginx/1.25"},
				"X-Internal": []string{"trace-id"},
				"X-Ok":       []string{"yes"},
			},
			wantAbsent:  []string{"Server", "X-Internal"},
			wantPresent: map[string]string{"X-Ok": "yes"},
		},
		{
			name: "does not mutate original header",
			cfg:  egress.HeadersConfig{},
			incoming: http.Header{
				"Server": []string{"nginx"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalServer := tt.incoming.Get("Server")

			got := tt.cfg.ApplyToResponse(tt.incoming)

			// Verify original is untouched.
			if tt.incoming.Get("Server") != originalServer {
				t.Error("ApplyToResponse mutated the original header map")
			}

			for k, want := range tt.wantPresent {
				if v := got.Get(k); v != want {
					t.Errorf("got[%q] = %q, want %q", k, v, want)
				}
			}
			for _, k := range tt.wantAbsent {
				if v := got.Get(k); v != "" {
					t.Errorf("got[%q] = %q, want absent", k, v)
				}
			}
		})
	}
}

func TestRoute_Headers_Accessor(t *testing.T) {
	cfg := egress.HeadersConfig{
		InjectHeaders:        map[string]string{"X-Api-Key": "key123"},
		StripRequestHeaders:  []string{"Cookie"},
		StripResponseHeaders: []string{"Server"},
	}

	r, err := egress.NewRoute("api", "https://api.example.com/*", egress.WithHeaders(cfg))
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}

	got := r.Headers()
	if got.InjectHeaders["X-Api-Key"] != "key123" {
		t.Errorf("Headers().InjectHeaders[X-Api-Key] = %q, want %q", got.InjectHeaders["X-Api-Key"], "key123")
	}
	if len(got.StripRequestHeaders) != 1 || got.StripRequestHeaders[0] != "Cookie" {
		t.Errorf("Headers().StripRequestHeaders = %v, want [Cookie]", got.StripRequestHeaders)
	}
	if len(got.StripResponseHeaders) != 1 || got.StripResponseHeaders[0] != "Server" {
		t.Errorf("Headers().StripResponseHeaders = %v, want [Server]", got.StripResponseHeaders)
	}
}

func TestRoute_Headers_DefaultIsEmpty(t *testing.T) {
	r, err := egress.NewRoute("plain", "https://api.example.com/*")
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}

	got := r.Headers()
	if len(got.InjectHeaders) != 0 {
		t.Errorf("default InjectHeaders should be empty, got %v", got.InjectHeaders)
	}
	if len(got.StripRequestHeaders) != 0 {
		t.Errorf("default StripRequestHeaders should be empty, got %v", got.StripRequestHeaders)
	}
	if len(got.StripResponseHeaders) != 0 {
		t.Errorf("default StripResponseHeaders should be empty, got %v", got.StripResponseHeaders)
	}
}
