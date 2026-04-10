package middleware

import (
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const (
	// xRequestIDHeader is the canonical header name for request/correlation IDs.
	// It is the de-facto standard used by NGINX, AWS ALB, and most API gateways.
	xRequestIDHeader = "X-Request-ID"
)

// RequestIDMiddleware returns HTTP middleware that ensures every request and
// response carries an X-Request-ID header.
//
// Resolution order (first match wins):
//  1. When tracing is enabled and the request context already carries a valid
//     OTel span (i.e. TracingMiddleware ran before this one), the trace ID is
//     used as the request ID so that both headers refer to the same correlation
//     token.
//  2. When the incoming request contains a non-empty X-Request-ID header, that
//     value is echoed back. This lets upstream load balancers or clients inject
//     their own correlation IDs.
//  3. A fresh ID is generated via generateRequestID() (format: "req_<12 chars>").
//
// In all cases the resolved ID is:
//   - stored in the request context via ContextWithRequestID so that downstream
//     handlers and other middleware can retrieve it with CorrelationID.
//   - written to the X-Request-ID response header before calling next.
//
// The middleware trims surrounding whitespace from incoming header values and
// ignores them when empty after trimming.
func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := resolveRequestID(r)

			// Store in context for downstream use.
			ctx := ContextWithRequestID(r.Context(), id)

			// Echo on the response so clients and log aggregators can correlate.
			w.Header().Set(xRequestIDHeader, id)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// resolveRequestID determines the request ID for the current request.
// It follows the resolution order described on RequestIDMiddleware.
func resolveRequestID(r *http.Request) string {
	ctx := r.Context()

	// 1. Prefer the OTel trace ID when a valid span is already in context.
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		return sc.TraceID().String()
	}

	// 2. Echo a client-supplied X-Request-ID when present and non-empty.
	if incoming := strings.TrimSpace(r.Header.Get(xRequestIDHeader)); incoming != "" {
		return incoming
	}

	// 3. Generate a fresh ID.
	return generateRequestID()
}
