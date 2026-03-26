package metrics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServer_StartAndStop(t *testing.T) {
	adapter := NewPrometheusAdapter(nil)
	srv := NewServer(adapter.Handler())

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !strings.Contains(string(body), "go_goroutines") {
		t.Error("response does not contain expected Prometheus metric go_goroutines")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestServer_StopBeforeStart(t *testing.T) {
	// Stop on a never-started server must be a no-op.
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ctx := context.Background()
	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() on unstarted server error = %v, want nil", err)
	}
}

func TestServer_UnknownPathReturns404(t *testing.T) {
	adapter := NewPrometheusAdapter(nil)
	srv := NewServer(adapter.Handler())

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + srv.Addr() + "/unknown")
	if err != nil {
		t.Fatalf("GET /unknown: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
