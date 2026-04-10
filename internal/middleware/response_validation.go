package middleware

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/llm"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// LLMResponseValidationAction controls what happens when an LLM response fails
// schema validation.
type LLMResponseValidationAction string

const (
	// LLMResponseValidationActionBlock rejects the response and returns 502 Bad
	// Gateway to the application.
	LLMResponseValidationActionBlock LLMResponseValidationAction = "block"

	// LLMResponseValidationActionWarn passes the response through unchanged but
	// logs a warning and emits an llm.response_invalid event.
	LLMResponseValidationActionWarn LLMResponseValidationAction = "warn"
)

// maxResponseBodyBytes is the maximum number of bytes read from an upstream
// response body for schema validation. Reading the entire body of very large
// responses is unnecessary for structural validation.
const maxResponseBodyBytes = 1 * 1024 * 1024 // 1 MB

// LLMResponseValidationRouteConfig holds the LLM response schema validation
// configuration for a single egress route.
type LLMResponseValidationRouteConfig struct {
	// RouteName is the unique egress route name.
	RouteName string

	// RoutePattern is the URL glob pattern for the route (e.g. "https://api.openai.com/**").
	RoutePattern string

	// Enabled toggles schema validation for this route. When false the route is skipped.
	Enabled bool

	// Validator is the pre-built response validator for this route.
	Validator llm.ResponseValidator

	// Action controls the response when validation fails.
	// Defaults to LLMResponseValidationActionBlock when zero.
	Action LLMResponseValidationAction
}

// LLMResponseValidationMiddleware returns an HTTP middleware that validates
// upstream LLM API response bodies against a configured JSON Schema.
//
// The middleware is intended to wrap the egress proxy HTTP handler. It:
//  1. Resolves the target URL from the X-Egress-URL header or the /_egress/ path prefix.
//  2. Matches the URL against the configured route patterns.
//  3. On a match where Enabled is true, intercepts the upstream response via a
//     buffering ResponseWriter, reads the response body (up to maxResponseBodyBytes),
//     and checks the Content-Type. Only application/json responses are validated.
//  4. Runs the route's Validator against the captured body.
//  5. On validation failure:
//     - LLMResponseValidationActionBlock: discards the upstream response, returns
//     502 Bad Gateway with a JSON error body.
//     - LLMResponseValidationActionWarn: logs a warning, emits the
//     llm.response_invalid event, and passes the upstream response through.
//
// If eventLogger is non-nil, an llm.response_invalid event is emitted on every
// validation failure.
// The logger must not be nil; pass slog.Default() if no custom logger is needed.
func LLMResponseValidationMiddleware(
	routes []LLMResponseValidationRouteConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
) func(http.Handler) http.Handler {
	// Filter to only enabled routes to avoid repeated checks at request time.
	enabled := make([]LLMResponseValidationRouteConfig, 0, len(routes))
	for _, r := range routes {
		if r.Enabled {
			if r.Action == "" {
				r.Action = LLMResponseValidationActionBlock
			}
			enabled = append(enabled, r)
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(enabled) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			targetURL := resolveTargetURL(r)
			if targetURL == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Find the first matching route.
			var matched *LLMResponseValidationRouteConfig
			for i := range enabled {
				if matchesGlob(enabled[i].RoutePattern, targetURL) {
					matched = &enabled[i]
					break
				}
			}
			if matched == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Intercept the response via a buffering writer.
			buf := &bufferingResponseWriter{
				header: make(http.Header),
			}
			next.ServeHTTP(buf, r)

			// Only validate application/json responses.
			ct := buf.header.Get("Content-Type")
			if !isJSONContentType(ct) {
				// Not JSON — replay the response as-is.
				copyBufferedResponse(w, buf)
				return
			}

			// Read up to maxResponseBodyBytes for validation.
			body := buf.body.Bytes()
			if len(body) > maxResponseBodyBytes {
				body = body[:maxResponseBodyBytes]
			}

			violations, valErr := matched.Validator.Validate(body)

			traceID := CorrelationID(r.Context())

			if valErr != nil {
				// Body could not be parsed as JSON — treat as a validation failure.
				logger.WarnContext(r.Context(), "llm.response_invalid: failed to parse response body",
					slog.String("route", matched.RouteName),
					slog.String("url", targetURL),
					slog.String("error", valErr.Error()),
				)
				if matched.Action == LLMResponseValidationActionBlock {
					emitLLMResponseInvalid(r, eventLogger, matched, targetURL, buf.status, ct, traceID,
						[]string{fmt.Sprintf("response body is not valid JSON: %s", valErr)})
					WriteErrorResponse(w, r, http.StatusBadGateway, "llm_response_invalid",
						"upstream response failed schema validation: body is not valid JSON")
					return
				}
				emitLLMResponseInvalid(r, eventLogger, matched, targetURL, buf.status, ct, traceID,
					[]string{fmt.Sprintf("response body is not valid JSON: %s", valErr)})
				copyBufferedResponse(w, buf)
				return
			}

			if len(violations) > 0 {
				logger.WarnContext(r.Context(), "llm.response_invalid: response failed schema validation",
					slog.String("route", matched.RouteName),
					slog.String("url", targetURL),
					slog.Int("violations", len(violations)),
				)
				emitLLMResponseInvalid(r, eventLogger, matched, targetURL, buf.status, ct, traceID, violations)

				if matched.Action == LLMResponseValidationActionBlock {
					WriteErrorResponse(w, r, http.StatusBadGateway, "llm_response_invalid",
						fmt.Sprintf("upstream response failed schema validation: %d violation(s)", len(violations)))
					return
				}

				// Warn mode: log and pass through.
				copyBufferedResponse(w, buf)
				return
			}

			// Validation passed — forward the response.
			copyBufferedResponse(w, buf)
		})
	}
}

// bufferingResponseWriter captures the upstream response status, headers, and
// body so the middleware can inspect it before forwarding to the original writer.
type bufferingResponseWriter struct {
	header  http.Header
	body    bytes.Buffer
	status  int
	written bool
}

// Header returns the response header map that the handler can set.
func (b *bufferingResponseWriter) Header() http.Header {
	return b.header
}

// WriteHeader captures the HTTP status code.
func (b *bufferingResponseWriter) WriteHeader(status int) {
	if !b.written {
		b.status = status
		b.written = true
	}
}

// Write captures response body bytes.
func (b *bufferingResponseWriter) Write(p []byte) (int, error) {
	if !b.written {
		b.status = http.StatusOK
		b.written = true
	}
	return b.body.Write(p)
}

// copyBufferedResponse replays the buffered response headers, status code, and
// body to the original ResponseWriter.
func copyBufferedResponse(w http.ResponseWriter, buf *bufferingResponseWriter) {
	dst := w.Header()
	for k, vs := range buf.header {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
	if buf.status != 0 {
		w.WriteHeader(buf.status)
	}
	_, _ = io.Copy(w, &buf.body)
}

// isJSONContentType reports whether the Content-Type header value indicates a
// JSON body. It strips parameters (charset etc.) before comparison.
func isJSONContentType(ct string) bool {
	mt := strings.ToLower(strings.TrimSpace(ct))
	if idx := strings.IndexByte(mt, ';'); idx >= 0 {
		mt = strings.TrimSpace(mt[:idx])
	}
	return mt == "application/json"
}

// emitLLMResponseInvalid emits an llm.response_invalid structured event when an
// event logger is configured.
func emitLLMResponseInvalid(
	r *http.Request,
	eventLogger ports.EventLogger,
	matched *LLMResponseValidationRouteConfig,
	targetURL string,
	statusCode int,
	contentType string,
	traceID string,
	violations []string,
) {
	if eventLogger == nil {
		return
	}
	ev := events.NewLLMResponseInvalid(events.LLMResponseInvalidParams{
		Route:       matched.RouteName,
		Method:      r.Method,
		URL:         targetURL,
		StatusCode:  statusCode,
		ContentType: contentType,
		Action:      string(matched.Action),
		Violations:  violations,
		TraceID:     traceID,
	})
	_ = eventLogger.Log(r.Context(), ev)
}

// LLMResponseValidationRouteInput carries the per-route configuration fields
// needed by BuildLLMResponseValidationRoutes to construct
// LLMResponseValidationRouteConfig values.
type LLMResponseValidationRouteInput struct {
	// Name is the unique egress route name.
	Name string

	// Pattern is the URL glob pattern for the route.
	Pattern string

	// Enabled mirrors EgressRouteConfig.ResponseValidation.Enabled.
	Enabled bool

	// Schema is the JSON Schema document (as a map) to validate against.
	Schema map[string]any

	// Action mirrors EgressRouteConfig.ResponseValidation.Action ("block" or "warn").
	Action string
}

// BuildLLMResponseValidationRoutes converts a slice of per-route LLM response
// validation config entries (from vibewarden.yaml) into the resolved
// LLMResponseValidationRouteConfig values expected by LLMResponseValidationMiddleware.
//
// Routes where Enabled is false are omitted from the result.
// Returns an error when any route schema fails to compile.
func BuildLLMResponseValidationRoutes(routes []LLMResponseValidationRouteInput) ([]LLMResponseValidationRouteConfig, error) {
	out := make([]LLMResponseValidationRouteConfig, 0, len(routes))
	for _, r := range routes {
		if !r.Enabled {
			continue
		}
		sd, err := llm.NewSchemaDefinition(r.Schema)
		if err != nil {
			return nil, fmt.Errorf("route %q response_validation schema: %w", r.Name, err)
		}
		v, err := llm.NewResponseValidator(sd)
		if err != nil {
			return nil, fmt.Errorf("route %q response_validation validator: %w", r.Name, err)
		}
		action := LLMResponseValidationAction(r.Action)
		if action == "" {
			action = LLMResponseValidationActionBlock
		}
		out = append(out, LLMResponseValidationRouteConfig{
			RouteName:    r.Name,
			RoutePattern: r.Pattern,
			Enabled:      true,
			Validator:    v,
			Action:       action,
		})
	}
	return out, nil
}
