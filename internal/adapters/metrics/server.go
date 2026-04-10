package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Server is an internal HTTP server that exposes the Prometheus metrics handler
// on a localhost-only listener. Caddy reverse-proxies the public
// /_vibewarden/metrics route to this server, keeping the metrics handler
// decoupled from Caddy's module system.
type Server struct {
	handler  http.Handler
	listener net.Listener
	server   *http.Server
	logger   *slog.Logger
}

// NewServer creates a Server that will serve the given handler.
// logger is used to report unexpected serve errors; pass slog.Default() if no
// custom logger is needed. Call Start to bind the listener and begin accepting
// connections.
func NewServer(handler http.Handler, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{handler: handler, logger: logger}
}

// Start binds a random localhost TCP port, starts serving the metrics handler,
// and returns immediately. The server continues running until Stop is called.
// Start must be called before Addr.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("binding metrics server listener: %w", err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.Handle("/metrics", s.handler)

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		// Serve until stopped; ErrServerClosed is the expected shutdown signal.
		if serveErr := s.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.logger.Error("metrics server stopped unexpectedly", "err", serveErr)
		}
	}()

	return nil
}

// Addr returns the host:port the server is listening on.
// Addr must only be called after a successful Start.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Stop gracefully shuts down the server using the provided context.
func (s *Server) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down metrics server: %w", err)
	}
	return nil
}
