package health

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// maxBodySnippet is the maximum number of bytes read from a non-200 response
// body for inclusion in the ProbeNon200Error.
const maxBodySnippet = 512

// wireHealth is the JSON shape of /_vibewarden/health (ADR-098). It is used
// only for unmarshalling inside HTTPProber.Probe and is not exported.
type wireHealth struct {
	Status     string            `json:"status"`
	Version    string            `json:"version"`
	Site       string            `json:"site"`
	Components map[string]string `json:"components"`
}

// HTTPProber implements ports.HealthProber via Go's stdlib net/http.
//
// Construct with NewLocalhostProber for the dev path (InsecureSkipVerify=true),
// or NewStrictProber for the --env path (full cert chain verification via
// the stdlib default transport).
type HTTPProber struct {
	client *http.Client
}

// NewLocalhostProber returns an HTTPProber whose TLS client skips certificate
// verification. This is appropriate for the dev path where the sidecar
// presents a self-signed certificate on localhost — and where system curl on
// macOS (LibreSSL) cannot complete the handshake.
//
// timeout is the per-request deadline. Pass 3*time.Second for CLI use.
func NewLocalhostProber(timeout time.Duration) *HTTPProber {
	return &HTTPProber{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // dev self-signed cert only; intentional per ADR-102
			},
		},
	}
}

// NewStrictProber returns an HTTPProber that uses the stdlib default TLS
// verification (full cert chain). This is the --env path where the target
// is a production domain with a valid certificate.
//
// timeout is the per-request deadline. Pass 3*time.Second for CLI use.
func NewStrictProber(timeout time.Duration) *HTTPProber {
	return &HTTPProber{
		client: &http.Client{Timeout: timeout},
	}
}

// Probe performs a single HTTPS GET against url and returns the parsed
// HealthDocument. Error semantics follow ports.HealthProber:
//   - ports.ErrDNSFailure    — hostname does not resolve
//   - ports.ErrProbeRefused  — connection refused
//   - ports.ErrProbeMalformed — body cannot be decoded
//   - *ports.ProbeNon200Error — non-2xx HTTP status
func (p *HTTPProber) Probe(ctx context.Context, url string) (ports.HealthDocument, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ports.HealthDocument{}, fmt.Errorf("building probe request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		// Classify DNS failures first: net.DNSError is wrapped inside *url.Error
		// → *net.OpError → *net.DNSError. errors.As walks the full chain.
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return ports.HealthDocument{}, ports.ErrDNSFailure
		}
		if isConnectionRefused(err) {
			return ports.HealthDocument{}, ports.ErrProbeRefused
		}
		if isTLSHandshakeError(err) {
			return ports.HealthDocument{}, fmt.Errorf("%w: %w", ports.ErrTLSHandshake, err)
		}
		return ports.HealthDocument{}, fmt.Errorf("executing probe request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodySnippet))
		return ports.HealthDocument{}, &ports.ProbeNon200Error{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(snippet)),
		}
	}

	var wire wireHealth
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return ports.HealthDocument{}, fmt.Errorf("%w: %s", ports.ErrProbeMalformed, err.Error())
	}

	// Validate that the response looks like the VibeWarden health wire format.
	// An empty status or nil components map indicates a foreign service.
	if wire.Status == "" || wire.Components == nil {
		return ports.HealthDocument{}, fmt.Errorf("%w: missing required fields", ports.ErrProbeMalformed)
	}

	return ports.HealthDocument{
		Status:     wire.Status,
		Version:    wire.Version,
		Site:       wire.Site,
		Components: wire.Components,
	}, nil
}

// isConnectionRefused inspects the error chain from net/http to detect TCP
// connection-refused failures. It checks the error message because Go's
// stdlib does not export a typed sentinel for ECONNREFUSED from net.OpError.
func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connect: connection refused")
}

// isTLSHandshakeError reports whether err contains a known TLS handshake
// failure substring. These substrings indicate either a transient ACME
// issuance in progress or a permanent cert-chain problem. The check is
// case-sensitive and matches the exact strings produced by Go's crypto/tls
// package and the BoringSSL/OpenSSL alert codes that reach the client.
//
// Recognised substrings:
//   - "tls: internal error"
//   - "tls: handshake failure"
//   - "bad certificate"
//   - "tls: protocol version not supported"
func isTLSHandshakeError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "tls: internal error") ||
		strings.Contains(msg, "tls: handshake failure") ||
		strings.Contains(msg, "bad certificate") ||
		strings.Contains(msg, "tls: protocol version not supported")
}

// Compile-time assertion that HTTPProber satisfies ports.HealthProber.
var _ ports.HealthProber = (*HTTPProber)(nil)
