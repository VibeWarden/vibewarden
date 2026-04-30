package ports

import (
	"context"
	"errors"
	"fmt"
)

// HealthProber issues an HTTPS GET against a sidecar's /_vibewarden/health
// endpoint and returns the parsed wire-format response.
//
// Implementations choose how strictly to verify the TLS cert chain — the
// localhost adapter sets InsecureSkipVerify=true; the production adapter uses
// the stdlib default (full chain verification). Callers express the difference
// by selecting the adapter, not via flags on the port.
type HealthProber interface {
	// Probe performs a single HTTPS GET against url and returns the parsed
	// body. Returns ErrDNSFailure when the hostname cannot be resolved.
	// Returns ErrProbeRefused when the connection is refused (stack not
	// running). Returns ErrProbeMalformed when the body cannot be parsed as
	// the expected wire format. Returns a *ProbeNon200Error (which wraps
	// ErrProbeNon200) when the server returns a non-2xx status.
	Probe(ctx context.Context, url string) (HealthDocument, error)
}

// HealthDocument is the parsed shape of /_vibewarden/health (ADR-098).
// It mirrors the JSON wire format emitted by the HealthHandler in the caddy adapter.
type HealthDocument struct {
	// Status is the aggregate status: "ok" or "degraded".
	Status string
	// Version is the sidecar version, e.g. "0.18.4".
	Version string
	// Site is the multisite scope; empty for single-site deployments.
	Site string
	// Components maps component names to their status strings.
	// Known keys: "sidecar" ("ok"), "upstream" ("ok"|"failing"|"unknown").
	Components map[string]string
}

// Sentinel errors for HealthProber implementations.
var (
	// ErrProbeRefused is returned when the connection to the health endpoint
	// is refused, indicating the stack is not running.
	ErrProbeRefused = errors.New("connection refused — stack is not running")

	// ErrProbeMalformed is returned when the response body cannot be parsed
	// as the expected /_vibewarden/health wire format.
	ErrProbeMalformed = errors.New("malformed health response body")

	// ErrProbeNon200 is the sentinel wrapped by ProbeNon200Error. Callers
	// may use errors.Is to detect any non-2xx response regardless of the
	// status code.
	ErrProbeNon200 = errors.New("non-2xx response from health endpoint")

	// ErrDNSFailure is returned when the health endpoint hostname cannot be
	// resolved. This is distinct from ErrProbeRefused: the TCP stack was never
	// reached because DNS lookup failed first. In production this typically
	// means the tls.domain entry in vibewarden.<env>.yaml has no matching A/AAAA
	// record; for localhost it indicates a broken /etc/hosts.
	ErrDNSFailure = errors.New("DNS resolution failed")
)

// ProbeNon200Error wraps ErrProbeNon200 with the HTTP status code and a
// bounded snippet of the response body so the CLI layer can render a useful
// message without re-issuing the request.
type ProbeNon200Error struct {
	// StatusCode is the HTTP status code returned by the server.
	StatusCode int
	// Body is a truncated snippet of the response body (up to ~512 bytes).
	Body string
}

// Error implements the error interface.
func (e *ProbeNon200Error) Error() string {
	return fmt.Sprintf("non-2xx response (%d): %s", e.StatusCode, e.Body)
}

// Unwrap returns ErrProbeNon200 so callers can use errors.Is(err, ErrProbeNon200).
func (e *ProbeNon200Error) Unwrap() error { return ErrProbeNon200 }
