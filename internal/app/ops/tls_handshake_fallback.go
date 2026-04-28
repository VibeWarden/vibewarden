package ops

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// inlineHandshakeResolver is an app-layer fallback TLS state resolver.
// It performs the same TLS handshake as the caddy adapter's
// HandshakeResolver but lives here so internal/app/ops does not need to
// import internal/adapters/caddy. Composition roots should prefer the
// adapter version; this helper keeps older wire points (and the existing
// doctor tests) working when no resolver is injected.
type inlineHandshakeResolver struct {
	cfg     *config.Config
	host    string
	port    int
	timeout time.Duration
	now     func() time.Time
}

// newInlineHandshakeResolver constructs the fallback resolver. Returns a
// ports.TLSStateResolver so callers program against the port.
func newInlineHandshakeResolver(cfg *config.Config, host string, port int) ports.TLSStateResolver {
	return &inlineHandshakeResolver{
		cfg:     cfg,
		host:    host,
		port:    port,
		timeout: 3 * time.Second,
		now:     time.Now,
	}
}

func (r *inlineHandshakeResolver) Resolve(ctx context.Context) (tlsdomain.State, error) {
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

	dialer := tls.Dialer{Config: &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // leaf inspection only
	}}
	conn, err := dialer.DialContext(handshakeCtx, "tcp", addr)
	if err != nil {
		return tlsdomain.NewUnknown(), nil
	}
	defer func() { _ = conn.Close() }()

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return tlsdomain.NewUnknown(), nil
	}
	certs := tlsConn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return tlsdomain.NewUnknown(), nil
	}
	leaf := certs[0]

	if tlsdomain.IsCaddyLocalIssuer(leaf.Issuer.CommonName) {
		return tlsdomain.NewSelfSignedLocal(), nil
	}

	now := r.now()
	if now.After(leaf.NotAfter) {
		return tlsdomain.NewExpiringSoon(0, leaf.NotAfter), nil
	}
	daysLeft := int(leaf.NotAfter.Sub(now).Hours() / 24)
	if daysLeft <= localTLSCertExpiryWarnDays {
		return tlsdomain.NewExpiringSoon(daysLeft, leaf.NotAfter), nil
	}
	return tlsdomain.NewObtained(leaf.NotAfter), nil
}
