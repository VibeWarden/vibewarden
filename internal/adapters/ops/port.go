package ops

import (
	"context"
	"fmt"
	"net"
)

// NetPortChecker implements ports.PortChecker using net.Listen.
type NetPortChecker struct{}

// NewNetPortChecker creates a new NetPortChecker.
func NewNetPortChecker() *NetPortChecker {
	return &NetPortChecker{}
}

// IsPortAvailable returns true when nothing is listening on host:port.
// It attempts to bind a TCP listener — if binding succeeds the port is
// available; if it fails the port is already in use.
func (p *NetPortChecker) IsPortAvailable(_ context.Context, host string, port int) (bool, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Could not bind — port is in use (or other network error).
		return false, nil //nolint:nilerr
	}
	if err := ln.Close(); err != nil {
		return true, fmt.Errorf("closing probe listener: %w", err)
	}
	return true, nil
}
