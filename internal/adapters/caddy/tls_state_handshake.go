package caddy

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// HandshakeResolver resolves the sidecar's TLS state by performing a
// live TLS handshake and inspecting the leaf certificate. This is the
// authoritative resolver when status and doctor run as CLI subprocesses
// (the common case) because the sidecar is in a different process than
// the command.
//
// The handshake uses InsecureSkipVerify because the caller only needs to
// inspect the leaf — not validate trust. A self-signed dev cert is the
// expected norm locally.
type HandshakeResolver struct {
	cfg     *config.Config
	host    string
	port    int
	timeout time.Duration
	now     func() time.Time
	// dialer is injected for tests. Production uses tls.Dialer.DialContext.
	dial func(ctx context.Context, addr string, tlsCfg *tls.Config) (*tls.ConnectionState, func() error, error)
}

// NewHandshakeResolver builds a HandshakeResolver for the configured
// sidecar listener. It uses cfg.Server.Host/Port for the dial target
// when host/port are zero-valued; explicit host/port wins.
func NewHandshakeResolver(cfg *config.Config, host string, port int) *HandshakeResolver {
	return &HandshakeResolver{
		cfg:     cfg,
		host:    host,
		port:    port,
		timeout: 3 * time.Second,
		now:     time.Now,
		dial:    defaultHandshakeDialer,
	}
}

// Resolve implements ports.TLSStateResolver.
//
// Sequence:
//  1. If TLS is disabled in config → Disabled.
//  2. Dial the configured host:port with TLS + InsecureSkipVerify.
//  3. Inspect the leaf's issuer CN. "Caddy Local Authority" → SelfSignedLocal
//     with no expiry math.
//  4. Otherwise compare NotAfter against now to produce Obtained or
//     ExpiringSoon.
//
// Unreachable / malformed handshakes produce Unknown + nil error so the
// caller can render a neutral message without treating it as a failure.
func (r *HandshakeResolver) Resolve(ctx context.Context) (tlsdomain.State, error) {
	if r.cfg != nil && !r.cfg.TLS.Enabled {
		return tlsdomain.NewDisabled(), nil
	}

	dialHost := r.host
	if dialHost == "" || dialHost == "0.0.0.0" || dialHost == "::" {
		dialHost = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", dialHost, r.port)

	handshakeCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	state, closeFn, err := r.dial(handshakeCtx, addr, &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // leaf inspection only, no trust check
	})
	if err != nil {
		return tlsdomain.NewUnknown(), nil
	}
	defer func() { _ = closeFn() }()

	if state == nil || len(state.PeerCertificates) == 0 {
		return tlsdomain.NewUnknown(), nil
	}

	leaf := state.PeerCertificates[0]

	// Caddy internal issuer → SelfSignedLocal regardless of config.
	if leaf.Issuer.CommonName == caddyLocalIssuerCN {
		return tlsdomain.NewSelfSignedLocal(), nil
	}

	// Non-internal issuer → expiry math.
	now := r.now()
	if now.After(leaf.NotAfter) {
		return tlsdomain.NewExpiringSoon(0, leaf.NotAfter), nil
	}
	daysLeft := int(leaf.NotAfter.Sub(now).Hours() / 24)
	if daysLeft <= tlsCertExpiryWarnDays {
		return tlsdomain.NewExpiringSoon(daysLeft, leaf.NotAfter), nil
	}
	return tlsdomain.NewObtained(leaf.NotAfter), nil
}

// defaultHandshakeDialer performs the real TLS dial and returns the
// ConnectionState plus a Close() function.
func defaultHandshakeDialer(ctx context.Context, addr string, tlsCfg *tls.Config) (*tls.ConnectionState, func() error, error) {
	dialer := tls.Dialer{Config: tlsCfg}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, func() error { return nil }, err
	}

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		_ = conn.Close()
		return nil, func() error { return nil }, fmt.Errorf("unexpected connection type %T", conn)
	}
	cs := tlsConn.ConnectionState()
	return &cs, conn.Close, nil
}
