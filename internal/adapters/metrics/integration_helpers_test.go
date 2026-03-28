//go:build integration

package metrics_test

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// findFreePort returns a free TCP port on localhost.
func findFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// waitForAddr polls the given host:port until a TCP connection succeeds or the
// timeout elapses.
func waitForAddr(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("server at %s did not become ready within %s", addr, timeout)
}

// assertBodyContains fails the test if body does not contain the given substring.
func assertBodyContains(t *testing.T, body, substring string) {
	t.Helper()
	if !strings.Contains(body, substring) {
		t.Errorf("metrics output does not contain %q\n\nFull output:\n%s",
			substring, body)
	}
}
