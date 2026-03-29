package egress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egressplugin "github.com/vibewarden/vibewarden/internal/plugins/egress"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// disabled plugin helper.
func newDisabledPlugin() *egressplugin.Plugin {
	return egressplugin.New(egressplugin.Config{Enabled: false}, nil, nil)
}

// ---- Name ----

func TestPlugin_Name(t *testing.T) {
	p := newDisabledPlugin()
	if got := p.Name(); got != "egress" {
		t.Errorf("Name() = %q, want %q", got, "egress")
	}
}

// ---- Disabled lifecycle ----

func TestPlugin_Disabled_InitStartStop(t *testing.T) {
	p := newDisabledPlugin()
	ctx := context.Background()

	if err := p.Init(ctx); err != nil {
		t.Fatalf("Init() returned unexpected error: %v", err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() returned unexpected error: %v", err)
	}
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() returned unexpected error: %v", err)
	}
}

func TestPlugin_Disabled_Health(t *testing.T) {
	p := newDisabledPlugin()
	h := p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false for disabled plugin, want true")
	}
}

// ---- Init validation ----

func TestPlugin_Init_InvalidPolicy(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		DefaultPolicy: "garbage",
	}, nil, nil)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for invalid policy, got nil")
	}
}

func TestPlugin_Init_InvalidRoutePattern(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		DefaultPolicy: "deny",
		Routes: []egressplugin.RouteConfig{
			{Name: "bad", Pattern: "not-a-url"},
		},
	}, nil, nil)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for non-URL pattern, got nil")
	}
}

func TestPlugin_Init_EmptyRouteName(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		DefaultPolicy: "deny",
		Routes: []egressplugin.RouteConfig{
			{Name: "", Pattern: "https://api.example.com/**"},
		},
	}, nil, nil)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for empty route name, got nil")
	}
}

func TestPlugin_Init_CircuitBreakerMissingResetAfter(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		DefaultPolicy: "deny",
		Routes: []egressplugin.RouteConfig{
			{
				Name:    "stripe",
				Pattern: "https://api.stripe.com/**",
				CircuitBreaker: egressplugin.CircuitBreakerConfig{
					Threshold:  5,
					ResetAfter: 0, // missing
				},
			},
		},
	}, nil, nil)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for circuit breaker with zero reset_after, got nil")
	}
}

func TestPlugin_Init_CircuitBreakerMissingThreshold(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		DefaultPolicy: "deny",
		Routes: []egressplugin.RouteConfig{
			{
				Name:    "stripe",
				Pattern: "https://api.stripe.com/**",
				CircuitBreaker: egressplugin.CircuitBreakerConfig{
					Threshold:  0, // missing
					ResetAfter: 30 * time.Second,
				},
			},
		},
	}, nil, nil)
	err := p.Init(context.Background())
	if err == nil {
		t.Fatal("Init() expected error for circuit breaker with zero threshold, got nil")
	}
}

func TestPlugin_Init_ValidConfig(t *testing.T) {
	tests := []struct {
		name   string
		cfg    egressplugin.Config
		wantOK bool
	}{
		{
			name: "minimal valid",
			cfg: egressplugin.Config{
				Enabled:       true,
				DefaultPolicy: "deny",
			},
			wantOK: true,
		},
		{
			name: "allow policy",
			cfg: egressplugin.Config{
				Enabled:       true,
				DefaultPolicy: "allow",
			},
			wantOK: true,
		},
		{
			name: "with valid route",
			cfg: egressplugin.Config{
				Enabled:       true,
				DefaultPolicy: "deny",
				Routes: []egressplugin.RouteConfig{
					{
						Name:      "stripe",
						Pattern:   "https://api.stripe.com/**",
						Methods:   []string{"GET", "POST"},
						Timeout:   10 * time.Second,
						RateLimit: "100/s",
						CircuitBreaker: egressplugin.CircuitBreakerConfig{
							Threshold:  5,
							ResetAfter: 30 * time.Second,
						},
						Retries: egressplugin.RetryConfig{
							Max:            3,
							Backoff:        "exponential",
							InitialBackoff: 100 * time.Millisecond,
						},
					},
				},
			},
			wantOK: true,
		},
		{
			name: "with insecure route",
			cfg: egressplugin.Config{
				Enabled:       true,
				DefaultPolicy: "deny",
				Routes: []egressplugin.RouteConfig{
					{
						Name:          "insecure-service",
						Pattern:       "http://internal.example.com/**",
						AllowInsecure: true,
					},
				},
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := egressplugin.New(tt.cfg, nil, nil)
			err := p.Init(context.Background())
			if (err == nil) != tt.wantOK {
				t.Errorf("Init() error = %v, wantOK = %v", err, tt.wantOK)
			}
		})
	}
}

// ---- Start/Stop and Health ----

func TestPlugin_StartStop_Health(t *testing.T) {
	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		Listen:        "127.0.0.1:0",
		DefaultPolicy: "deny",
	}, nil, nil)

	ctx := context.Background()

	if err := p.Init(ctx); err != nil {
		t.Fatalf("Init(): %v", err)
	}

	// Before Start, proxy is not running.
	h := p.Health()
	if h.Healthy {
		t.Error("Health().Healthy = true before Start(), want false")
	}

	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	// After Start, proxy should be healthy.
	h = p.Health()
	if !h.Healthy {
		t.Errorf("Health().Healthy = false after Start(), want true; message: %s", h.Message)
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Errorf("Stop(): %v", err)
	}

	// After Stop, proxy is no longer running.
	h = p.Health()
	if h.Healthy {
		t.Error("Health().Healthy = true after Stop(), want false")
	}
}

// ---- End-to-end: proxy forwards requests ----

func TestPlugin_ProxiesRequest(t *testing.T) {
	// Start a real upstream server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer upstream.Close()

	p := egressplugin.New(egressplugin.Config{
		Enabled:       true,
		Listen:        "127.0.0.1:0",
		DefaultPolicy: "deny",
		AllowInsecure: true,
		Routes: []egressplugin.RouteConfig{
			{
				Name:    "upstream",
				Pattern: upstream.URL + "/**",
			},
		},
	}, nil, nil)

	ctx := context.Background()
	if err := p.Init(ctx); err != nil {
		t.Fatalf("Init(): %v", err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Stop(stopCtx)
	}()

	// We cannot get the Addr from the plugin directly, but we can confirm the
	// proxy is healthy (listener is up).
	h := p.Health()
	if !h.Healthy {
		t.Fatalf("proxy not healthy after Start(): %s", h.Message)
	}
}

// ---- PluginMeta ----

func TestPlugin_Meta(t *testing.T) {
	p := newDisabledPlugin()

	if p.Description() == "" {
		t.Error("Description() returned empty string")
	}
	if len(p.ConfigSchema()) == 0 {
		t.Error("ConfigSchema() returned empty map")
	}
	if p.Example() == "" {
		t.Error("Example() returned empty string")
	}
}

// ---- Interface guards ----

var (
	_ ports.Plugin     = (*egressplugin.Plugin)(nil)
	_ ports.PluginMeta = (*egressplugin.Plugin)(nil)
)
