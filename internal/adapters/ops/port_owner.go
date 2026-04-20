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

// healthSignaturePrefix is the JSON body prefix served by the VibeWarden
// sidecar on /_vibewarden/health. It is stable across sidecar versions
// because it is part of the published liveness contract. See ADR-084 and
// internal/adapters/caddy/config.go (healthBody).
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
// Contract (per ADR-084):
//   - port closed or TLS handshake fails → OwnerForeign.
//   - 2xx response whose body starts with `{"status":"ok","version":` → OwnerVibeWarden.
//   - 2xx response with any other body → OwnerForeign.
//   - non-2xx response → OwnerForeign.
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
