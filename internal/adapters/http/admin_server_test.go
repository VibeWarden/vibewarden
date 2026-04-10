package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	vibehttp "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/domain/user"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newTestAdminServer creates an AdminServer backed by a fake service that
// returns an empty user list. It does not call Start.
func newTestAdminServer() *vibehttp.AdminServer {
	svc := &fakeAdminService{
		listResult: &ports.PaginatedUsers{Users: []user.User{}, Total: 0},
	}
	handlers := vibehttp.NewAdminHandlers(svc, nil)
	return vibehttp.NewAdminServer(handlers, nil)
}

// TestAdminServer_StartAndStop verifies that the server starts, accepts
// connections, and stops cleanly.
func TestAdminServer_StartAndStop(t *testing.T) {
	srv := newTestAdminServer()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr() returned empty string after Start()")
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/_vibewarden/admin/users")
	if err != nil {
		t.Fatalf("GET /_vibewarden/admin/users: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

// TestAdminServer_AddrReturnsLocalhostPort verifies that Addr returns a
// 127.0.0.1:<port> address after Start.
func TestAdminServer_AddrReturnsLocalhostPort(t *testing.T) {
	srv := newTestAdminServer()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	addr := srv.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("Addr() = %q, want 127.0.0.1:<port>", addr)
	}
}

// TestAdminServer_StopBeforeStart verifies that calling Stop on a server that
// was never started is a safe no-op.
func TestAdminServer_StopBeforeStart(t *testing.T) {
	srv := newTestAdminServer()
	ctx := context.Background()
	if err := srv.Stop(ctx); err != nil {
		t.Errorf("Stop() on unstarted server error = %v, want nil", err)
	}
}

// TestAdminServer_RespondsWithJSON verifies that the server returns
// application/json content and a well-formed response body.
func TestAdminServer_RespondsWithJSON(t *testing.T) {
	srv := newTestAdminServer()

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop(context.Background()) //nolint:errcheck

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + srv.Addr() + "/_vibewarden/admin/users")
	if err != nil {
		t.Fatalf("GET /_vibewarden/admin/users: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	ct := resp.Header.Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body is not valid JSON: %v; body: %s", err, body)
	}
}
