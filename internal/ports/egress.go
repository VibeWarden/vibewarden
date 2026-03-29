package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

// EgressProxy is the outbound port for the egress proxy plugin.
// Implementations intercept outbound HTTP requests, apply per-route security
// and resilience settings, and forward the request to the external service.
type EgressProxy interface {
	// HandleRequest processes an outbound HTTP request.
	// It looks up the matching route, applies security settings (secret injection,
	// rate limiting, circuit breaking), forwards the request, and returns the
	// response. Returns an error when the request is denied by policy, the circuit
	// is open, or the upstream returns a non-recoverable error.
	HandleRequest(ctx context.Context, req egress.EgressRequest) (egress.EgressResponse, error)
}

// RouteResolver is the outbound port for resolving an egress request to its
// configured route. Implementations consult the loaded configuration to find
// the first route whose pattern matches the request URL and whose methods
// include the request method.
type RouteResolver interface {
	// Resolve attempts to match the given request against the configured routes.
	// It returns an EgressRequest paired with the matched Route (Matched == true)
	// or an unmatched result (Matched == false) when no route is found.
	// Returns an error only on internal failures (e.g. malformed configuration).
	Resolve(ctx context.Context, req egress.EgressRequest) (egress.RouteMatch, error)
}
