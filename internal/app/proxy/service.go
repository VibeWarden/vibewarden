// Package proxy provides the application service for the reverse proxy.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

const defaultShutdownTimeout = 30 * time.Second

// Service orchestrates the reverse proxy lifecycle.
// It starts the proxy server and handles graceful shutdown on context cancellation.
type Service struct {
	server ports.ProxyServer
	logger *slog.Logger
}

// NewService creates a new proxy service with the given server implementation.
func NewService(server ports.ProxyServer, logger *slog.Logger) *Service {
	return &Service{
		server: server,
		logger: logger,
	}
}

// Run starts the proxy server and blocks until the context is cancelled or an error occurs.
// On context cancellation, it initiates a graceful shutdown with a 30-second timeout.
func (s *Service) Run(ctx context.Context) error {
	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		errCh <- s.server.Start(serverCtx)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, stopping proxy")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer shutdownCancel()

		if err := s.server.Stop(shutdownCtx); err != nil {
			return fmt.Errorf("stopping proxy: %w", err)
		}
		return ctx.Err()

	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("proxy server error: %w", err)
		}
		return nil
	}
}

// Reload reloads the proxy configuration without dropping active connections.
func (s *Service) Reload(ctx context.Context) error {
	s.logger.Info("reloading proxy configuration")
	if err := s.server.Reload(ctx); err != nil {
		return fmt.Errorf("reloading proxy: %w", err)
	}
	return nil
}
