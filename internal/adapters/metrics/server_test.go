package metrics

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	oteladapter "github.com/vibewarden/vibewarden/internal/adapters/otel"
)

// newTestHandler returns a Prometheus HTTP handler backed by a fresh OTelAdapter.
// It uses NewTestProvider so tests do not need to repeat OTel initialisation boilerplate.
func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	provider, err := oteladapter.NewTestProvider(context.Background())
	if err != nil {
		t.Fatalf("NewTestProvider() error = %v", err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	adapter, err := NewOTelAdapter(provider, nil)
	if err != nil {
		t.Fatalf("NewOTelAdapter() error = %v", err)
	}
	return adapter.Handler()
}

func TestServer_StartAndStop(t *testing.T) {
	srv := NewServer(newTestHandler(t), nil)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr() returned empty string after Start()")
	}

	// Verify the server responds on /metrics.
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	// The OTel Prometheus bridge always emits target_info with service metadata.
	if len(body) == 0 {
		t.Error("metrics response body is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestServer_StopBeforeStart(t *testing.T) {
	// Stop on a never-started server must be a no-op.
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), nil)
	ctx := context.Background()
	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() on unstarted server error = %v, want nil", err)
	}
}

func TestServer_UnknownPathReturns404(t *testing.T) {
	srv := NewServer(newTestHandler(t), nil)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + srv.Addr() + "/unknown")
	if err != nil {
		t.Fatalf("GET /unknown: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
