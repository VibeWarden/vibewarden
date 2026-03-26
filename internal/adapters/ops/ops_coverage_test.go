// Package ops_test provides additional tests to bring coverage above 80%.
package ops_test

import (
	"context"
	"net/http"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

// TestHTTPHealthChecker_NilClientUsesDefault verifies that passing nil uses
// the default HTTP client and the checker still functions correctly.
func TestHTTPHealthChecker_NilClientUsesDefault(t *testing.T) {
	checker := opsadapter.NewHTTPHealthChecker(nil)
	if checker == nil {
		t.Fatal("NewHTTPHealthChecker(nil) returned nil")
	}
}

// TestHTTPHealthChecker_InvalidURL verifies that a malformed URL returns an
// error rather than panicking or silently succeeding.
func TestHTTPHealthChecker_InvalidURL(t *testing.T) {
	checker := opsadapter.NewHTTPHealthChecker(http.DefaultClient)

	_, _, err := checker.CheckHealth(context.Background(), "://invalid-url")
	if err == nil {
		t.Fatal("expected an error for an invalid URL, got nil")
	}
}

// TestComposeAdapter_Version_CancelledContext exercises Version with an
// already-cancelled context when docker is available.  The goal is to hit
// the error-return branch in Version so it is counted as covered.
func TestComposeAdapter_Version_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the call

	_, err := adapter.Version(ctx)
	if err == nil {
		t.Fatal("expected an error because context was cancelled")
	}
}

// TestComposeAdapter_Info_CancelledContext exercises Info with an
// already-cancelled context when docker is available.
func TestComposeAdapter_Info_CancelledContext(t *testing.T) {
	if !dockerAvailable() {
		t.Skip("docker binary not available")
	}

	adapter := opsadapter.NewComposeAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := adapter.Info(ctx)
	if err == nil {
		t.Fatal("expected an error because context was cancelled")
	}
}
