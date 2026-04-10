package jwt

import (
	"context"
	"crypto/rsa"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// DevServer is an internal HTTP server that serves the local dev JWKS endpoint
// at DevJWKSPath. It is started during plugin initialisation and reverse-proxied
// through Caddy so external clients can reach it at /_vibewarden/jwks.json.
//
// DevServer is safe to use from a single goroutine for lifecycle operations
// (Start/Stop); the underlying http.Server handles concurrent requests.
type DevServer struct {
	keyPair  *DevKeyPair
	logger   *slog.Logger
	listener net.Listener
	server   *http.Server
}

// NewDevServer creates a DevServer for the given key pair.
// logger may be nil; in that case slog.Default() is used.
func NewDevServer(keyPair *DevKeyPair, logger *slog.Logger) *DevServer {
	if logger == nil {
		logger = slog.Default()
	}
	return &DevServer{
		keyPair: keyPair,
		logger:  logger,
	}
}

// Start binds a random localhost TCP port and begins serving the JWKS and token
// endpoints. It returns immediately; the server runs until Stop is called.
// Start must be called before Addr.
//
// Returns an error if the TCP listener cannot be bound or if the JWKS handler
// cannot be constructed from the key pair.
func (s *DevServer) Start() error {
	jwksHandler, err := NewJWKSHandler(PublicKey(s.keyPair))
	if err != nil {
		return fmt.Errorf("dev jwks server: building jwks handler: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("dev jwks server: binding listener: %w", err)
	}
	s.listener = ln

	mux := http.NewServeMux()
	mux.Handle(DevJWKSPath, jwksHandler)
	mux.Handle(DevTokenPath, NewTokenHandler(s.keyPair))

	s.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if serveErr := s.server.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			s.logger.Error("dev jwks server stopped unexpectedly", slog.String("error", serveErr.Error()))
		}
	}()

	s.logger.Info("dev JWKS server started",
		slog.String("addr", ln.Addr().String()),
		slog.String("jwks_path", DevJWKSPath),
		slog.String("token_path", DevTokenPath),
		slog.String("key_dir", s.keyPair.Dir),
	)

	return nil
}

// Stop gracefully shuts down the dev JWKS HTTP server.
func (s *DevServer) Stop(ctx context.Context) error {
	if s.server == nil {
		return nil
	}
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("dev jwks server: shutting down: %w", err)
	}
	return nil
}

// Addr returns the host:port the internal server is listening on.
// Addr must only be called after a successful Start.
func (s *DevServer) Addr() string {
	return s.listener.Addr().String()
}

// LocalJWKSURL returns the full URL of the local JWKS endpoint using the
// internal server address. This is used to auto-configure the JWT adapter.
func (s *DevServer) LocalJWKSURL() string {
	return "http://" + s.Addr() + DevJWKSPath
}

// PublicKey extracts the RSA public key from a DevKeyPair.
// It is a helper to avoid exposing the PrivateKey field directly.
func PublicKey(kp *DevKeyPair) *rsa.PublicKey {
	return &kp.PrivateKey.PublicKey
}
