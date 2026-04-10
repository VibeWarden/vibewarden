package secrets_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/plugins/secrets"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// fakeEventLogger captures emitted events for assertion in tests.
// All methods are safe for concurrent use.
type fakeEventLogger struct {
	mu     sync.Mutex
	events []events.Event
}

func (f *fakeEventLogger) Log(_ context.Context, ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

// snapshot returns a copy of all captured events under the lock.
func (f *fakeEventLogger) snapshot() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]events.Event, len(f.events))
	copy(out, f.events)
	return out
}

var _ ports.EventLogger = (*fakeEventLogger)(nil)

// newOpenBaoTestServer creates a minimal OpenBao HTTP API stub.
// secrets maps path -> key -> value for KV v2 responses.
func newOpenBaoTestServer(t *testing.T, secretsData map[string]map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/sys/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"initialized":true,"sealed":false}`))

		case strings.HasPrefix(r.URL.Path, "/v1/secret/data/"):
			path := strings.TrimPrefix(r.URL.Path, "/v1/secret/data/")
			data, ok := secretsData[path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"errors":[]}`))
				return
			}
			anyData := make(map[string]any, len(data))
			for k, v := range data {
				anyData[k] = v
			}
			resp := map[string]any{"data": map[string]any{"data": anyData}}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasPrefix(r.URL.Path, "/v1/secret/metadata/"):
			w.WriteHeader(http.StatusOK)
			resp := map[string]any{
				"data": map[string]any{
					"created_time":    time.Now().Format(time.RFC3339),
					"updated_time":    time.Now().Format(time.RFC3339),
					"current_version": 1,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestPlugin_Name(t *testing.T) {
	p := secrets.New(secrets.Config{}, nil, slog.Default())
	if got := p.Name(); got != "secrets" {
		t.Errorf("Name() = %q, want %q", got, "secrets")
	}
}

func TestPlugin_DisabledPlugin(t *testing.T) {
	p := secrets.New(secrets.Config{Enabled: false}, nil, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	health := p.Health()
	if !health.Healthy {
		t.Errorf("Health().Healthy = false, want true for disabled plugin")
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() returned %d handlers for disabled plugin, want 0", len(handlers))
	}

	routes := p.ContributeCaddyRoutes()
	if len(routes) != 0 {
		t.Errorf("ContributeCaddyRoutes() returned %d routes for disabled plugin, want 0", len(routes))
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Stop(stopCtx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestPlugin_Init_InvalidProvider(t *testing.T) {
	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "aws-secrets-manager",
		OpenBao:  secrets.OpenBaoConfig{Address: "http://localhost:8200"},
	}, nil, slog.Default())

	err := p.Init(context.Background())
	if err == nil {
		t.Error("Init() expected error for unsupported provider, got nil")
	}
}

func TestPlugin_Init_MissingAddress(t *testing.T) {
	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		// Address intentionally empty
	}, nil, slog.Default())

	err := p.Init(context.Background())
	if err == nil {
		t.Error("Init() expected error for missing address, got nil")
	}
}

func TestPlugin_Init_UnhealthyOpenBao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sealed.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "test"},
		},
	}, nil, slog.Default())

	err := p.Init(context.Background())
	if err == nil {
		t.Error("Init() expected error for sealed OpenBao, got nil")
	}
}

func TestPlugin_Init_StaticSecretFetch(t *testing.T) {
	srv := newOpenBaoTestServer(t, map[string]map[string]string{
		"app/stripe": {"api_key": "sk_test_abc123"},
	})
	defer srv.Close()

	eventLog := &fakeEventLogger{}
	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "root"},
		},
		Inject: secrets.InjectConfig{
			Headers: []secrets.HeaderInjection{
				{SecretPath: "app/stripe", SecretKey: "api_key", Header: "X-Stripe-Key"},
			},
		},
	}, eventLog, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// The cached secret should be available.
	val, ok := p.GetCachedSecret("app/stripe", "api_key")
	if !ok {
		t.Error("GetCachedSecret() returned false for a secret that should be cached")
	}
	if val != "sk_test_abc123" {
		t.Errorf("GetCachedSecret() = %q, want %q", val, "sk_test_abc123")
	}
}

func TestPlugin_ContributeCaddyHandlers_WithHeaders(t *testing.T) {
	srv := newOpenBaoTestServer(t, map[string]map[string]string{
		"app/internal": {"token": "secret-bearer"},
	})
	defer srv.Close()

	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "root"},
		},
		Inject: secrets.InjectConfig{
			Headers: []secrets.HeaderInjection{
				{SecretPath: "app/internal", SecretKey: "token", Header: "X-Internal-Token"},
			},
		},
	}, nil, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 1 {
		t.Fatalf("ContributeCaddyHandlers() returned %d handlers, want 1", len(handlers))
	}

	handler := handlers[0]
	if handler.Priority != 35 {
		t.Errorf("handler.Priority = %d, want 35", handler.Priority)
	}

	h, ok := handler.Handler["handler"].(string)
	if !ok || h != "headers" {
		t.Errorf("handler.Handler[\"handler\"] = %v, want \"headers\"", handler.Handler["handler"])
	}
}

func TestPlugin_ContributeCaddyHandlers_NoHeaders(t *testing.T) {
	srv := newOpenBaoTestServer(t, map[string]map[string]string{})
	defer srv.Close()

	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "root"},
		},
		// No header injections configured.
		Inject: secrets.InjectConfig{},
	}, nil, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	handlers := p.ContributeCaddyHandlers()
	if len(handlers) != 0 {
		t.Errorf("ContributeCaddyHandlers() returned %d handlers, want 0 when no headers configured", len(handlers))
	}
}

func TestPlugin_WriteEnvFile(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "secrets", ".env.secrets")

	srv := newOpenBaoTestServer(t, map[string]map[string]string{
		"app/db": {"password": "sup3rS3cret!", "host": "db.example.com"},
	})
	defer srv.Close()

	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "root"},
		},
		Inject: secrets.InjectConfig{
			EnvFile: envFile,
			Env: []secrets.EnvInjection{
				{SecretPath: "app/db", SecretKey: "password", EnvVar: "DATABASE_PASSWORD"},
				{SecretPath: "app/db", SecretKey: "host", EnvVar: "DATABASE_HOST"},
			},
		},
	}, nil, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that env file was written.
	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", envFile, err)
	}

	envContent := string(content)
	if !strings.Contains(envContent, "DATABASE_PASSWORD=sup3rS3cret!") {
		t.Errorf("env file does not contain DATABASE_PASSWORD; content:\n%s", envContent)
	}
	if !strings.Contains(envContent, "DATABASE_HOST=db.example.com") {
		t.Errorf("env file does not contain DATABASE_HOST; content:\n%s", envContent)
	}

	// Check file permissions are 0600.
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", envFile, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("env file permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestPlugin_HealthCheck_WeakSecret(t *testing.T) {
	srv := newOpenBaoTestServer(t, map[string]map[string]string{
		"app/creds": {"api_key": "password"}, // weak!
	})
	defer srv.Close()

	eventLog := &fakeEventLogger{}
	p := secrets.New(secrets.Config{
		Enabled:  true,
		Provider: "openbao",
		OpenBao: secrets.OpenBaoConfig{
			Address: srv.URL,
			Auth:    secrets.OpenBaoAuthConfig{Method: "token", Token: "root"},
		},
		Inject: secrets.InjectConfig{
			Headers: []secrets.HeaderInjection{
				{SecretPath: "app/creds", SecretKey: "api_key", Header: "X-API-Key"},
			},
		},
		Health: secrets.HealthConfig{
			CheckInterval: 24 * time.Hour, // don't run in background during test
			MaxStaticAge:  90 * 24 * time.Hour,
			WeakPatterns:  []string{"password", "changeme"},
		},
	}, eventLog, slog.Default())

	if err := p.Init(context.Background()); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Start runs the health check loop which emits immediately on first run.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for the health check event.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range eventLog.snapshot() {
			if ev.EventType == events.EventTypeSecretHealthCheck {
				count, _ := ev.Payload["finding_count"].(int)
				if count > 0 {
					_ = p.Stop(context.Background())
					return // found the expected finding
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = p.Stop(context.Background())
	t.Error("did not receive a secret_health_check event with findings within timeout")
}

func TestPlugin_Meta(t *testing.T) {
	p := secrets.New(secrets.Config{}, nil, slog.Default())

	if desc := p.Description(); desc == "" {
		t.Error("Description() returned empty string")
	}

	schema := p.ConfigSchema()
	if len(schema) == 0 {
		t.Error("ConfigSchema() returned empty map")
	}

	example := p.Example()
	if !strings.Contains(example, "openbao") {
		t.Errorf("Example() does not mention openbao; got:\n%s", example)
	}
}
