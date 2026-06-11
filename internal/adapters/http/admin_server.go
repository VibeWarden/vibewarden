package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// AdminServer is an internal HTTP server that serves the admin API handlers on
// a localhost-only listener. Caddy reverse-proxies the public
// /_vibewarden/admin/* routes to this server after the AdminAuthHandler has
// already verified the X-Admin-Key bearer token.
//
// The server also mounts the embedded admin UI (AdminUIHandler) at
// /_vibewarden/admin/ui/. UI assets are served without a token because the
// auth middleware carves out that prefix before forwarding to this server.
type AdminServer struct {
	handlers  *AdminHandlers
	uiHandler *AdminUIHandler
	listener  net.Listener
	server    *http.Server
	logger    *slog.Logger
}

// NewAdminServer creates an AdminServer backed by the supplied handlers.
// logger is used to report unexpected serve errors; pass slog.Default() when
// no custom logger is available. Call Start to bind the listener and begin
// accepting connections.
func NewAdminServer(handlers *AdminHandlers, logger *slog.Logger) *AdminServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &AdminServer{
		handlers: handlers,
		logger:   logger,
	}
}

// WithUIHandler returns a copy of the AdminServer with the AdminUIHandler set.
// When set, Start registers the UI routes on the mux. Call this before Start.
func (s *AdminServer) WithUIHandler(h *AdminUIHandler) *AdminServer {
	return &AdminServer{
		handlers:  s.handlers,
		uiHandler: h,
		listener:  s.listener,
		server:    s.server,
		logger:    s.logger,
	}
}

// Start binds a random localhost TCP port, registers the admin routes, and
// begins serving. Start returns immediately; the server continues running until
// Stop is called.
//
// Start must be called before Addr.
func (s *AdminServer) Start() error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("binding admin server listener: %w", err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	s.handlers.RegisterRoutes(mux)
	RegisterDocsRoute(mux)
	if s.uiHandler != nil {
		RegisterAdminUIRoutes(mux, s.uiHandler)
	}

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if serveErr := s.server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.logger.Error("admin server stopped unexpectedly", "err", serveErr)
		}
	}()

	return nil
}

// Addr returns the host:port the server is listening on.
// Addr must only be called after a successful Start.
func (s *AdminServer) Addr() string {
	return s.listener.Addr().String()
}

// Stop gracefully shuts down the admin server using the provided context.
func (s *AdminServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down admin server: %w", err)
	}
	return nil
}
