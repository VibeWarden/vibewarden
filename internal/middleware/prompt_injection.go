package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/domain/llm"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// headerEgressURL is the request header that carries the target URL in
	// transparent egress proxy mode. It must match the value used in the egress
	// adapter so the middleware can resolve the effective target URL.
	headerEgressURL = "X-Egress-URL"

	// namedRoutePrefix is the URL path prefix used in named-route egress mode.
	namedRoutePrefix = "/_egress/"

	// maxPromptBodyBytes is the maximum number of bytes read from a request body
	// for prompt injection scanning. Reading the entire body of a large request
	// is unnecessary — injection payloads appear near the start of the prompt.
	maxPromptBodyBytes = 64 * 1024 // 64 KB
)

// PromptInjectionAction controls what happens when a prompt injection is detected.
type PromptInjectionAction string

const (
	// PromptInjectionActionBlock rejects the request with 400 Bad Request.
	PromptInjectionActionBlock PromptInjectionAction = "block"

	// PromptInjectionActionDetect logs the event and forwards the request unchanged.
	PromptInjectionActionDetect PromptInjectionAction = "detect"
)

// PromptInjectionRouteConfig holds the prompt injection detection configuration
// for a single egress route.
type PromptInjectionRouteConfig struct {
	// RouteName is the unique egress route name.
	RouteName string

	// RoutePattern is the URL glob pattern for the route (e.g. "https://api.openai.com/**").
	RoutePattern string

	// Enabled toggles scanning for this route. When false the route is skipped.
	Enabled bool

	// ContentPaths is the list of JSON pointer-like path expressions used to
	// extract text fields from the request body before scanning.
	// Example: [".messages[].content", ".prompt"]
	// When empty, the entire raw body is scanned as a string.
	ContentPaths []string

	// Detector is the pre-built prompt injection detector for this route.
	// It combines the built-in patterns with any extra patterns configured for
	// the route.
	Detector llm.Detector

	// Action controls the response when an injection is detected.
	// Defaults to PromptInjectionActionBlock when zero.
	Action PromptInjectionAction
}

// PromptInjectionMiddleware returns an HTTP middleware that scans outbound LLM
// API request bodies for prompt injection payloads.
//
// The middleware is intended to wrap the egress proxy HTTP handler. It:
//  1. Resolves the target URL from the X-Egress-URL header or the /_egress/ path prefix.
//  2. Matches the URL against the configured route patterns.
//  3. On a match where Enabled is true, reads up to maxPromptBodyBytes from the
//     request body, restores the body so downstream handlers can re-read it, and
//     extracts text using the configured ContentPaths (or the whole body when
//     ContentPaths is empty).
//  4. Runs the route's Detector against each extracted text fragment.
//  5. On detection:
//     - PromptInjectionActionBlock: writes a 400 Bad Request JSON error and returns.
//     - PromptInjectionActionDetect: logs the event and passes the request through.
//
// If eventLogger is non-nil, an llm.prompt_injection_blocked or
// llm.prompt_injection_detected event is emitted for every detection.
// The logger must not be nil; pass slog.Default() if no custom logger is needed.
func PromptInjectionMiddleware(
	routes []PromptInjectionRouteConfig,
	logger *slog.Logger,
	eventLogger ports.EventLogger,
) func(http.Handler) http.Handler {
	// Filter to only enabled routes to avoid repeated checks at request time.
	enabled := make([]PromptInjectionRouteConfig, 0, len(routes))
	for _, r := range routes {
		if r.Enabled {
			// Normalise action: empty defaults to block.
			if r.Action == "" {
				r.Action = PromptInjectionActionBlock
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

			// Only scan requests that have a body and a JSON content type.
			// Requests without a body cannot carry a prompt.
			if r.Body == nil {
				next.ServeHTTP(w, r)
				return
			}

			targetURL := resolveTargetURL(r)
			if targetURL == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Find the first matching route.
			var matched *PromptInjectionRouteConfig
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

			// Read up to maxPromptBodyBytes and restore the body.
			chunk := make([]byte, maxPromptBodyBytes)
			n, err := io.ReadFull(r.Body, chunk)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				// Fail open on read errors — log and pass through.
				logger.WarnContext(r.Context(), "prompt_injection: failed to read request body",
					slog.String("route", matched.RouteName),
					slog.String("error", err.Error()),
				)
				next.ServeHTTP(w, r)
				return
			}
			body := chunk[:n]

			// Restore the body so downstream handlers can re-read it.
			r.Body = io.NopCloser(io.MultiReader(
				bytes.NewReader(body),
				r.Body, // remainder beyond maxPromptBodyBytes
			))

			// Extract text fragments to scan.
			fragments := extractFragments(body, matched.ContentPaths)

			action := matched.Action
			if action == "" {
				action = PromptInjectionActionBlock
			}

			for _, frag := range fragments {
				detected, patternName := matched.Detector.Detect(frag.text)
				if !detected {
					continue
				}

				traceID := CorrelationID(r.Context())
				params := events.LLMPromptInjectionParams{
					Route:       matched.RouteName,
					Method:      r.Method,
					URL:         targetURL,
					Pattern:     patternName,
					ContentPath: frag.path,
					Action:      string(action),
					TraceID:     traceID,
				}

				if action == PromptInjectionActionBlock {
					logger.WarnContext(r.Context(), "prompt_injection: request blocked",
						slog.String("route", matched.RouteName),
						slog.String("pattern", patternName),
						slog.String("content_path", frag.path),
						slog.String("method", r.Method),
						slog.String("url", targetURL),
					)
					if eventLogger != nil {
						_ = eventLogger.Log(r.Context(), events.NewLLMPromptInjectionBlocked(params))
					}
					WriteErrorResponse(w, r, http.StatusBadRequest, "prompt_injection_blocked",
						fmt.Sprintf("request blocked: prompt injection detected by pattern %q in %s",
							patternName, frag.path))
					return
				}

				// Detect mode: log and continue.
				logger.WarnContext(r.Context(), "prompt_injection: injection detected (detect mode — request allowed)",
					slog.String("route", matched.RouteName),
					slog.String("pattern", patternName),
					slog.String("content_path", frag.path),
					slog.String("method", r.Method),
					slog.String("url", targetURL),
				)
				if eventLogger != nil {
					_ = eventLogger.Log(r.Context(), events.NewLLMPromptInjectionDetected(params))
				}
				// Do not break — continue scanning all fragments in detect mode.
			}

			next.ServeHTTP(w, r)
		})
	}
}

// fragment holds a text value extracted from the request body along with the
// JSON path expression that yielded it.
type fragment struct {
	path string
	text string
}

// extractFragments extracts text fragments from body according to the configured
// content paths. When contentPaths is empty the entire body is returned as a
// single fragment with path "body".
//
// contentPaths use a simplified dot-path syntax:
//   - ".field" — top-level JSON field
//   - ".field.nested" — nested field
//   - ".array[].field" — all elements of a JSON array at the given field
//
// Non-JSON bodies or JSON bodies where a path does not match are skipped
// gracefully; no error is returned.
func extractFragments(body []byte, contentPaths []string) []fragment {
	if len(contentPaths) == 0 {
		return []fragment{{path: "body", text: string(body)}}
	}

	// Parse the body as JSON once.
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		// Not JSON — scan the entire body as a single fragment.
		return []fragment{{path: "body", text: string(body)}}
	}

	var frags []fragment
	for _, cp := range contentPaths {
		values := extractByPath(root, cp)
		for _, v := range values {
			frags = append(frags, fragment{path: cp, text: v})
		}
	}
	// If no content paths matched anything, fall back to the full body.
	if len(frags) == 0 {
		return []fragment{{path: "body", text: string(body)}}
	}
	return frags
}

// extractByPath extracts string values from a decoded JSON value (any) using
// the simplified dot-path syntax. Returns an empty slice when the path does
// not match.
//
// Supported tokens:
//   - ".field" / "field" — map key lookup
//   - "[*]" or "[]"      — array wildcard (all elements)
func extractByPath(v any, p string) []string {
	// Normalise: strip leading dot.
	p = strings.TrimPrefix(p, ".")

	if p == "" {
		if s, ok := v.(string); ok {
			return []string{s}
		}
		// Serialise non-string leaf values to string for scanning.
		if b, err := json.Marshal(v); err == nil {
			return []string{string(b)}
		}
		return nil
	}

	// Split the first token.
	field, rest := splitFirstToken(p)

	switch node := v.(type) {
	case map[string]any:
		// Check for array wildcard on the rest segment first:
		// e.g. ".messages[].content" — field="messages", rest="[].content"
		child, ok := node[field]
		if !ok {
			return nil
		}
		if rest == "" {
			return extractByPath(child, "")
		}
		// Check if rest starts with array wildcard.
		if strings.HasPrefix(rest, "[].") || rest == "[]" || strings.HasPrefix(rest, "[*].") || rest == "[*]" {
			// Strip the array token.
			afterArray := strings.TrimPrefix(rest, "[]")
			afterArray = strings.TrimPrefix(afterArray, "[*]")
			afterArray = strings.TrimPrefix(afterArray, ".")
			return extractByPath(child, afterArray)
		}
		return extractByPath(child, rest)

	case []any:
		// Walking into an array without an explicit wildcard token — apply the
		// remaining path to every element.
		var out []string
		for _, elem := range node {
			out = append(out, extractByPath(elem, p)...)
		}
		return out
	}

	return nil
}

// splitFirstToken splits p at the first separator (".", "[", or end-of-string)
// and returns the first token and the remainder. The leading "." or "[" is kept
// on the remainder where applicable.
func splitFirstToken(p string) (field, rest string) {
	for i, r := range p {
		if r == '.' && i > 0 {
			return p[:i], p[i+1:]
		}
		if r == '[' && i > 0 {
			return p[:i], p[i:]
		}
	}
	return p, ""
}

// resolveTargetURL extracts the effective target URL from the request.
// It checks the X-Egress-URL header (transparent mode) first, then falls
// back to the /_egress/ named-route prefix if present.
func resolveTargetURL(r *http.Request) string {
	if u := r.Header.Get(headerEgressURL); u != "" {
		return u
	}
	if strings.HasPrefix(r.URL.Path, namedRoutePrefix) {
		// Named-route mode: path is "/_egress/{route-name}/rest/of/path".
		// We cannot fully resolve the URL without the route pattern, so we return
		// the path as a proxy for matching purposes. Route patterns that use
		// named-route mode should match against "/_egress/<name>*" or similar.
		return r.URL.Path
	}
	return ""
}

// matchesGlob reports whether targetURL matches the given glob pattern.
//
// Matching rules (evaluated in order):
//  1. If pattern ends with "/**", the URL must start with the prefix before "/**".
//     Example: "https://api.openai.com/**" matches "https://api.openai.com/v1/chat/completions".
//  2. Otherwise, path.Match is used for standard single-segment wildcard patterns.
//
// Returns false when the pattern is malformed or the URL does not match.
func matchesGlob(pattern, targetURL string) bool {
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return strings.HasPrefix(targetURL, prefix+"/") || targetURL == prefix
	}
	matched, err := path.Match(pattern, targetURL)
	if err != nil {
		return false
	}
	return matched
}

// BuildPromptInjectionRoutes converts a slice of per-route egress prompt
// injection config entries (from vibewarden.yaml) into the resolved
// PromptInjectionRouteConfig values expected by PromptInjectionMiddleware.
//
// Routes where PromptInjection.Enabled is false are omitted from the result.
// Returns an error when any extra pattern in a route fails to compile.
func BuildPromptInjectionRoutes(routes []PromptInjectionRouteInput) ([]PromptInjectionRouteConfig, error) {
	out := make([]PromptInjectionRouteConfig, 0, len(routes))
	for _, r := range routes {
		if !r.PromptInjectionEnabled {
			continue
		}
		detector, err := llm.NewDetectorWithExtra(r.ExtraPatterns)
		if err != nil {
			return nil, fmt.Errorf("route %q prompt injection extra patterns: %w", r.Name, err)
		}
		action := PromptInjectionAction(r.Action)
		if action == "" {
			action = PromptInjectionActionBlock
		}
		out = append(out, PromptInjectionRouteConfig{
			RouteName:    r.Name,
			RoutePattern: r.Pattern,
			Enabled:      true,
			ContentPaths: r.ContentPaths,
			Detector:     detector,
			Action:       action,
		})
	}
	return out, nil
}

// PromptInjectionRouteInput carries the per-route configuration fields needed
// by BuildPromptInjectionRoutes to construct PromptInjectionRouteConfig values.
type PromptInjectionRouteInput struct {
	// Name is the unique egress route name.
	Name string

	// Pattern is the URL glob pattern for the route.
	Pattern string

	// PromptInjectionEnabled mirrors EgressRouteConfig.PromptInjection.Enabled.
	PromptInjectionEnabled bool

	// ContentPaths mirrors EgressRouteConfig.PromptInjection.ContentPaths.
	ContentPaths []string

	// ExtraPatterns mirrors EgressRouteConfig.PromptInjection.ExtraPatterns.
	ExtraPatterns []string

	// Action mirrors EgressRouteConfig.PromptInjection.Action ("block" or "detect").
	Action string
}
