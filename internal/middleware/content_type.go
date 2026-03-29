package middleware

import (
	"mime"
	"net/http"
)

// bodyMethods is the set of HTTP methods that carry a request body and
// therefore require a Content-Type header.
var bodyMethods = map[string]bool{
	http.MethodPost:  true,
	http.MethodPut:   true,
	http.MethodPatch: true,
}

// ContentTypeValidationConfig holds the configuration for the
// ContentTypeValidation middleware.
type ContentTypeValidationConfig struct {
	// Enabled toggles Content-Type validation.
	Enabled bool

	// Allowed is the list of permitted media types (e.g. "application/json").
	// Parameters (e.g. charset) are stripped before comparison, so
	// "application/json; charset=utf-8" matches "application/json".
	Allowed []string
}

// ContentTypeValidation returns an HTTP middleware that enforces the
// Content-Type header on body-bearing requests (POST, PUT, PATCH).
//
// Behaviour:
//   - Requests using a no-body method (GET, HEAD, DELETE, OPTIONS, …) always
//     pass through without inspection.
//   - A body-bearing request with no Content-Type header is rejected with
//     415 Unsupported Media Type.
//   - A body-bearing request whose Content-Type is not in cfg.Allowed is
//     rejected with 415 Unsupported Media Type.
//   - When disabled (cfg.Enabled == false) the middleware is a no-op.
//
// The 415 response body is the structured JSON produced by WriteErrorResponse
// so callers can correlate it to the corresponding log line via trace_id /
// request_id.
func ContentTypeValidation(cfg ContentTypeValidationConfig) func(next http.Handler) http.Handler {
	// Build a fast lookup set from the allowed list.
	allowed := make(map[string]bool, len(cfg.Allowed))
	for _, ct := range cfg.Allowed {
		allowed[ct] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled || !bodyMethods[r.Method] {
				next.ServeHTTP(w, r)
				return
			}

			ct := r.Header.Get("Content-Type")
			if ct == "" {
				WriteErrorResponse(
					w, r,
					http.StatusUnsupportedMediaType,
					"unsupported_media_type",
					"Content-Type header is required for "+r.Method+" requests",
				)
				return
			}

			// Strip parameters (e.g. "; charset=utf-8") before comparing.
			mediaType, _, err := mime.ParseMediaType(ct)
			if err != nil || !allowed[mediaType] {
				WriteErrorResponse(
					w, r,
					http.StatusUnsupportedMediaType,
					"unsupported_media_type",
					"Content-Type "+ct+" is not allowed",
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
