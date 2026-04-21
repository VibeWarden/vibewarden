// Package caddy provides adapters that embed and control the Caddy web
// server in-process. The TLS state resolver here reports the current TLS
// state of the sidecar by inspecting Caddy's in-memory configuration and
// certmagic cache — no HTTP admin-API roundtrip is required.
package caddy

import (
	"context"
	"crypto/x509"
	"time"

	"github.com/caddyserver/caddy/v2"

	"github.com/vibewarden/vibewarden/internal/config"
	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// caddyLocalIssuerCN is the Common Name that Caddy's internal issuer
// stamps on dev leaf certificates. Seeing this CN is the deterministic
// signal that we are serving a self-signed dev cert and must not be
// warned about by expiry heuristics.
const caddyLocalIssuerCN = "Caddy Local Authority"

// tlsCertExpiryWarnDays mirrors the same constant used by the doctor
// check — kept private here to avoid a cross-package dependency.
const tlsCertExpiryWarnDays = 7

// peerCertProvider is the minimal interface the in-process resolver
// needs to read the current leaf from the certmagic cache. The default
// implementation walks caddy.ActiveContext() at call time; tests inject
// a fake to exercise every State variant without a running Caddy.
type peerCertProvider interface {
	// LeafCert returns the leaf x509 certificate currently being served
	// for the configured server. When no leaf is in the cache it returns
	// (nil, nil). When Caddy is not running in this process it returns
	// ports.ErrNotInProcess.
	LeafCert(ctx context.Context) (*x509.Certificate, error)
}

// defaultPeerCertProvider is the production peerCertProvider. It checks
// caddy.ActiveContext() for a running Caddy instance and, if present,
// would read the certmagic cache. Because status and doctor run as
// separate processes from the sidecar in the common case, this path
// almost always short-circuits with ErrNotInProcess — which is exactly
// what the chain resolver relies on to fall through to the handshake
// adapter.
type defaultPeerCertProvider struct{}

func (defaultPeerCertProvider) LeafCert(_ context.Context) (*x509.Certificate, error) {
	ctx := caddy.ActiveContext()
	if ctx.Context == nil {
		return nil, ports.ErrNotInProcess
	}
	// In-process cert inspection via certmagic is out of scope for v0.17;
	// the handshake fallback is authoritative for now. Reporting
	// ErrNotInProcess ensures the chain resolver falls through correctly.
	return nil, ports.ErrNotInProcess
}

// InProcessResolver reports TLS state by inspecting Caddy's in-memory
// state. It is the primary resolver in the chain composed at the
// composition root.
//
// When Caddy is not running in this process (the normal case for
// `vibew doctor` and `vibew status` CLI subprocesses) Resolve returns
// ports.ErrNotInProcess so the chain resolver can fall through to the
// handshake adapter.
type InProcessResolver struct {
	cfg      *config.Config
	provider peerCertProvider
	now      func() time.Time
}

// NewInProcessResolver constructs an InProcessResolver bound to
// the given config. The resolver reads cfg.TLS.Enabled and
// cfg.TLS.Provider to distinguish Disabled, SelfSignedLocal and
// ACME-like states.
func NewInProcessResolver(cfg *config.Config) *InProcessResolver {
	return &InProcessResolver{
		cfg:      cfg,
		provider: defaultPeerCertProvider{},
		now:      time.Now,
	}
}

// withProvider swaps the peerCertProvider for testing. Not part of the
// public API.
func (r *InProcessResolver) withProvider(p peerCertProvider) *InProcessResolver {
	r.provider = p
	return r
}

// withNow swaps the clock for testing.
func (r *InProcessResolver) withNow(now func() time.Time) *InProcessResolver {
	r.now = now
	return r
}

// Resolve implements ports.TLSStateResolver.
func (r *InProcessResolver) Resolve(ctx context.Context) (tlsdomain.State, error) {
	if r.cfg == nil || !r.cfg.TLS.Enabled {
		return tlsdomain.NewDisabled(), nil
	}

	leaf, err := r.provider.LeafCert(ctx)
	if err != nil {
		// ErrNotInProcess is a signal, not a failure — propagate so
		// the chain can fall through.
		return tlsdomain.NewUnknown(), err
	}

	// No leaf in cache yet.
	if leaf == nil {
		if r.cfg.TLS.Provider == "self-signed" {
			// Self-signed but no leaf cached yet — treat as obtaining.
			return tlsdomain.NewObtaining(), nil
		}
		return tlsdomain.NewObtaining(), nil
	}

	// Self-signed branch is deterministic: issuer CN match → SelfSignedLocal,
	// NO NotAfter inspection. The internal issuer rotates leaves on a
	// short TTL and we trust it to do so.
	if r.cfg.TLS.Provider == "self-signed" && leaf.Issuer.CommonName == caddyLocalIssuerCN {
		return tlsdomain.NewSelfSignedLocal(), nil
	}

	// ACME / external branch: inspect expiry.
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
