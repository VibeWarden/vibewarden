package ops

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// healthIdentityHeader is the response header that VibeWarden always emits on
// /_vibewarden/health, regardless of the health.expose_version setting. It is
// the primary ownership signal used to detect a VibeWarden sidecar — decoupled
// from the version string so that detection works even when version exposure is
// suppressed (health.expose_version: false, OWASP A05 hardening).
//
// The canonical value lives in internal/ports so this adapter does not import a
// sibling adapter. The value is always "1" (ports.HealthIdentityHeaderValue).
const healthIdentityHeader = ports.HealthIdentityHeader

// healthSignaturePrefix is the JSON body prefix served by older VibeWarden
// sidecars on /_vibewarden/health. Detection via this prefix is kept as a
// backward-compatibility fallback for sidecars that predate the stable
// X-Vibewarden header (introduced in the fix for #1276). New sidecars emit
// the header; this prefix check handles version-skew during upgrades.
//
// Detection order (see ProbeOwner):
//  1. X-Vibewarden: 1 header present → OwnerVibeWarden (primary, header-based)
//  2. Body starts with healthSignaturePrefix → OwnerVibeWarden (legacy fallback)
//  3. Anything else → OwnerForeign
const healthSignaturePrefix = `{"status":"ok","version":`

// probeReadLimit caps how much of the health response body is read. The
// signature lives in the first ~30 bytes, so 512 bytes is generous and
// prevents a malicious foreign listener from streaming indefinitely.
const probeReadLimit = 512

// defaultProbeTimeout is the per-probe timeout when the caller's context has
// no earlier deadline.
const defaultProbeTimeout = 2 * time.Second

// healthProbePath is the path of the VibeWarden liveness endpoint.
const healthProbePath = "/_vibewarden/health"

// VibeWardenHealthProbe implements ports.PortOwnerProbe by issuing a short
// TLS-protected HTTP GET to /_vibewarden/health and matching the response
// body against the VibeWarden JSON signature.
//
// The probe always runs against localhost during local diagnostics, so it is
// configured with InsecureSkipVerify — self-signed certificates are the norm
// in dev and the probe's purpose is to detect a sibling sidecar, not to
// validate trust.
type VibeWardenHealthProbe struct {
	client *http.Client
}

// NewVibeWardenHealthProbe returns a new probe. If client is nil a dedicated
// client with a 2-second timeout and InsecureSkipVerify is created.
func NewVibeWardenHealthProbe(client *http.Client) *VibeWardenHealthProbe {
	if client == nil {
		client = &http.Client{
			Timeout: defaultProbeTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // local diagnostics; see godoc
				},
			},
		}
	}
	return &VibeWardenHealthProbe{client: client}
}

// ProbeOwner returns the identity of the process bound to host:port.
//
// When host is "0.0.0.0" the probe targets 127.0.0.1 because wildcard
// addresses are not valid HTTP destinations.
//
// Contract (per ADR-084, updated for #1276):
//   - port closed or TLS handshake fails → OwnerForeign.
//   - non-2xx response → OwnerForeign.
//   - 2xx response with X-Vibewarden: 1 header → OwnerVibeWarden (primary path,
//     works even when health.expose_version: false suppresses the version field).
//   - 2xx response whose body starts with `{"status":"ok","version":` →
//     OwnerVibeWarden (legacy fallback for sidecars predating #1276).
//   - 2xx response with any other body and no X-Vibewarden header → OwnerForeign.
//
// Both checks are performed so that the probe works correctly during rolling
// upgrades where a mix of old and new sidecar versions may be present.
//
// The function never returns OwnerUnknown — callers map "port available"
// (no probe needed) to OwnerUnknown themselves.
func (p *VibeWardenHealthProbe) ProbeOwner(ctx context.Context, host string, port int) ports.PortOwner {
	if host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	probeCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		probeCtx, cancel = context.WithTimeout(ctx, defaultProbeTimeout)
		defer cancel()
	}

	url := fmt.Sprintf("https://%s:%d%s", host, port, healthProbePath)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url, nil)
	if err != nil {
		return ports.OwnerForeign
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return ports.OwnerForeign
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ports.OwnerForeign
	}

	// Primary: stable X-Vibewarden header (introduced in v0.20.x / #1276).
	// Decoupled from the version string so that detection works even when the
	// operator has set health.expose_version: false.
	if resp.Header.Get(healthIdentityHeader) == "1" {
		return ports.OwnerVibeWarden
	}

	// Legacy fallback: body-prefix match for sidecars that predate the
	// X-Vibewarden header. Kept to ensure detection works across upgrades /
	// version skew. Older sidecars always include the version in the body.
	body, err := io.ReadAll(io.LimitReader(resp.Body, probeReadLimit))
	if err != nil {
		return ports.OwnerForeign
	}

	if strings.HasPrefix(string(body), healthSignaturePrefix) {
		return ports.OwnerVibeWarden
	}
	return ports.OwnerForeign
}

// Compile-time assertion that VibeWardenHealthProbe satisfies the port.
var _ ports.PortOwnerProbe = (*VibeWardenHealthProbe)(nil)
