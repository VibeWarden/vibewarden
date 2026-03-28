package caddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

func TestBuildTimeoutHandlerJSON_NoTimeout(t *testing.T) {
	cfg := ports.ResilienceConfig{Timeout: 0}

	result, err := buildTimeoutHandlerJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result when Timeout is 0, got %v", result)
	}
}

func TestBuildTimeoutHandlerJSON_WithTimeout(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		wantSeconds float64
	}{
		{"30 seconds", 30 * time.Second, 30.0},
		{"1 minute", time.Minute, 60.0},
		{"500 milliseconds", 500 * time.Millisecond, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ports.ResilienceConfig{Timeout: tt.timeout}

			result, err := buildTimeoutHandlerJSON(cfg)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if result["handler"] != "vibewarden_timeout" {
				t.Errorf("handler = %v, want %q", result["handler"], "vibewarden_timeout")
			}

			// Parse the config bytes to verify TimeoutSeconds.
			raw, ok := result["config"].(json.RawMessage)
			if !ok {
				t.Fatalf("config is not json.RawMessage: %T", result["config"])
			}
			var handlerCfg TimeoutHandlerConfig
			if err := json.Unmarshal(raw, &handlerCfg); err != nil {
				t.Fatalf("unmarshal config: %v", err)
			}
			if handlerCfg.TimeoutSeconds != tt.wantSeconds {
				t.Errorf("TimeoutSeconds = %v, want %v", handlerCfg.TimeoutSeconds, tt.wantSeconds)
			}
		})
	}
}

func TestTimeoutResponseWriter_WriteSetsWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	tw := &timeoutResponseWriter{ResponseWriter: rec}

	if tw.written {
		t.Error("written should be false initially")
	}

	n, err := tw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned %d, want 5", n)
	}
	if !tw.written {
		t.Error("written should be true after Write")
	}
}

func TestTimeoutResponseWriter_WriteHeaderSetsWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	tw := &timeoutResponseWriter{ResponseWriter: rec}

	if tw.written {
		t.Error("written should be false initially")
	}

	tw.WriteHeader(http.StatusOK)
	if !tw.written {
		t.Error("written should be true after WriteHeader")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Code = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIsTimeoutError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTimeoutError(tt.err)
			if got != tt.want {
				t.Errorf("isTimeoutError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestBuildCaddyConfig_WithResilience(t *testing.T) {
	tests := []struct {
		name        string
		timeout     time.Duration
		wantTimeout bool
	}{
		{"no timeout configured", 0, false},
		{"30s timeout", 30 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ports.ProxyConfig{
				ListenAddr:   "127.0.0.1:8080",
				UpstreamAddr: "127.0.0.1:3000",
				Resilience:   ports.ResilienceConfig{Timeout: tt.timeout},
			}

			result, err := BuildCaddyConfig(cfg)
			if err != nil {
				t.Fatalf("BuildCaddyConfig() error = %v", err)
			}

			// Walk the config to find the timeout handler in the catch-all route.
			apps, ok := result["apps"].(map[string]any)
			if !ok {
				t.Fatal("apps missing")
			}
			httpApp, ok := apps["http"].(map[string]any)
			if !ok {
				t.Fatal("http app missing")
			}
			servers, ok := httpApp["servers"].(map[string]any)
			if !ok {
				t.Fatal("servers missing")
			}
			server, ok := servers["vibewarden"].(map[string]any)
			if !ok {
				t.Fatal("vibewarden server missing")
			}
			routes, ok := server["routes"].([]map[string]any)
			if !ok {
				t.Fatal("routes missing or wrong type")
			}

			// The catch-all route is the last one.
			catchAll := routes[len(routes)-1]
			handlers, ok := catchAll["handle"].([]map[string]any)
			if !ok {
				t.Fatal("handle missing or wrong type in catch-all route")
			}

			found := false
			for _, h := range handlers {
				if h["handler"] == "vibewarden_timeout" {
					found = true
					break
				}
			}

			if found != tt.wantTimeout {
				t.Errorf("timeout handler present = %v, want %v", found, tt.wantTimeout)
			}
		})
	}
}
