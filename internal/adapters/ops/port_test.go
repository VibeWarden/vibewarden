package ops_test

import (
	"context"
	"net"
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
)

func TestNetPortChecker_IsPortAvailable_Available(t *testing.T) {
	checker := opsadapter.NewNetPortChecker()

	// Use a random high port that should be free.
	available, err := checker.IsPortAvailable(context.Background(), "127.0.0.1", 19876)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !available {
		t.Error("expected port 19876 to be available")
	}
}

func TestNetPortChecker_IsPortAvailable_InUse(t *testing.T) {
	// Bind a listener to occupy the port, then check it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not start test listener: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	addr := ln.Addr().(*net.TCPAddr)
	checker := opsadapter.NewNetPortChecker()

	available, checkErr := checker.IsPortAvailable(context.Background(), "127.0.0.1", addr.Port)
	if checkErr != nil {
		t.Fatalf("unexpected error: %v", checkErr)
	}
	if available {
		t.Errorf("expected port %d to be in use, but checker reported available", addr.Port)
	}
}
