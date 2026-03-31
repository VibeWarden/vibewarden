package middleware

import (
	"fmt"
	"net/http"
	"path"
)

// InputValidationConfig holds configuration for the InputValidation middleware.
// Limits are applied globally; per-path overrides can narrow or widen them for
// specific URL paths.
type InputValidationConfig struct {
	// Enabled toggles the middleware. When false the handler is a no-op.
	Enabled bool

	// MaxURLLength is the maximum allowed length of the raw request URI
	// (path + query string). Default: 2048. Zero disables this check.
	MaxURLLength int

	// MaxQueryStringLength is the maximum allowed length of the query string
	// component of the URL, not including the leading "?". Default: 2048.
	// Zero disables this check.
	MaxQueryStringLength int

	// MaxHeaderCount is the maximum number of request headers allowed.
	// Default: 100. Zero disables this check.
	MaxHeaderCount int

	// MaxHeaderSize is the maximum allowed byte length of any single header
	// value. Default: 8192. Zero disables this check.
	MaxHeaderSize int

	// PathOverrides allows per-path configuration. The first entry whose Path
	// glob pattern (path.Match syntax) matches the request URL path wins. If no
	// override matches, the top-level limits apply.
	PathOverrides []InputValidationPathOverride
}

// InputValidationPathOverride defines per-path limit overrides.
// Only non-zero fields override the global values.
type InputValidationPathOverride struct {
	// Path is a glob pattern (path.Match syntax) matched against the request
	// URL path (e.g. "/api/upload", "/static/*").
	Path string

	// MaxURLLength overrides the global limit. Zero means use the global value.
	MaxURLLength int

	// MaxQueryStringLength overrides the global limit. Zero means use the
	// global value.
	MaxQueryStringLength int

	// MaxHeaderCount overrides the global limit. Zero means use the global
	// value.
	MaxHeaderCount int

	// MaxHeaderSize overrides the global limit. Zero means use the global
	// value.
	MaxHeaderSize int
}

// inputValidationLimits holds the effective per-request limits after override
// resolution.
type inputValidationLimits struct {
	maxURLLength         int
	maxQueryStringLength int
	maxHeaderCount       int
	maxHeaderSize        int
}

// InputValidation returns an HTTP middleware that enforces request input size
// limits. It is designed to run before the WAF so that oversized inputs are
// rejected early, before regex scanning begins.
//
// Behaviour:
//   - When disabled (cfg.Enabled == false) the middleware is a transparent
//     pass-through.
//   - The /_vibewarden/* prefix is always exempt from validation.
//   - Limit resolution: for each request the first PathOverride whose glob
//     pattern matches the request path wins; any zero-valued field in the
//     override falls back to the corresponding global limit.
//   - Violations produce 400 Bad Request with a structured JSON body using
//     error code "input_validation_failed".
//
// Checks are applied in this order:
//  1. URL length (len(r.RequestURI))
//  2. Query string length (len(r.URL.RawQuery))
//  3. Header count (len(r.Header))
//  4. Per-header value size (any single header value exceeding MaxHeaderSize)
func InputValidation(cfg InputValidationConfig) func(http.Handler) http.Handler {
	if !cfg.Enabled {
		return func(next http.Handler) http.Handler { return next }
	}

	// Pre-validate all override patterns at construction time so the error is
	// surfaced immediately rather than silently on the first matching request.
	type validatedOverride struct {
		pattern string
		limits  InputValidationPathOverride
	}

	overrides := make([]validatedOverride, 0, len(cfg.PathOverrides))
	for _, ov := range cfg.PathOverrides {
		if _, err := path.Match(ov.Path, ""); err != nil {
			// Invalid pattern: skip. The misconfiguration is visible at startup
			// because Init() validates the config before calling this function.
			continue
		}
		overrides = append(overrides, validatedOverride{pattern: ov.Path, limits: ov})
	}

	// Exempt matcher covers /_vibewarden/* only.
	exemptMatcher, _ := NewExemptPathMatcher(nil) //nolint:errcheck // nil is always valid

	resolve := func(requestPath string) inputValidationLimits {
		l := inputValidationLimits{
			maxURLLength:         cfg.MaxURLLength,
			maxQueryStringLength: cfg.MaxQueryStringLength,
			maxHeaderCount:       cfg.MaxHeaderCount,
			maxHeaderSize:        cfg.MaxHeaderSize,
		}
		for _, ov := range overrides {
			matched, err := path.Match(ov.pattern, requestPath)
			if err != nil || !matched {
				continue
			}
			if ov.limits.MaxURLLength != 0 {
				l.maxURLLength = ov.limits.MaxURLLength
			}
			if ov.limits.MaxQueryStringLength != 0 {
				l.maxQueryStringLength = ov.limits.MaxQueryStringLength
			}
			if ov.limits.MaxHeaderCount != 0 {
				l.maxHeaderCount = ov.limits.MaxHeaderCount
			}
			if ov.limits.MaxHeaderSize != 0 {
				l.maxHeaderSize = ov.limits.MaxHeaderSize
			}
			break
		}
		return l
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Always exempt /_vibewarden/* paths.
			if exemptMatcher.Matches(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			limits := resolve(r.URL.Path)

			// 1. URL length check.
			if limits.maxURLLength > 0 && len(r.RequestURI) > limits.maxURLLength {
				WriteErrorResponse(w, r, http.StatusBadRequest, "input_validation_failed",
					fmt.Sprintf("request URI length %d exceeds maximum %d",
						len(r.RequestURI), limits.maxURLLength))
				return
			}

			// 2. Query string length check.
			if limits.maxQueryStringLength > 0 && len(r.URL.RawQuery) > limits.maxQueryStringLength {
				WriteErrorResponse(w, r, http.StatusBadRequest, "input_validation_failed",
					fmt.Sprintf("query string length %d exceeds maximum %d",
						len(r.URL.RawQuery), limits.maxQueryStringLength))
				return
			}

			// 3. Header count check.
			if limits.maxHeaderCount > 0 && len(r.Header) > limits.maxHeaderCount {
				WriteErrorResponse(w, r, http.StatusBadRequest, "input_validation_failed",
					fmt.Sprintf("header count %d exceeds maximum %d",
						len(r.Header), limits.maxHeaderCount))
				return
			}

			// 4. Per-header value size check.
			if limits.maxHeaderSize > 0 {
				for name, values := range r.Header {
					for _, v := range values {
						if len(v) > limits.maxHeaderSize {
							WriteErrorResponse(w, r, http.StatusBadRequest, "input_validation_failed",
								fmt.Sprintf("header %q value length %d exceeds maximum %d",
									name, len(v), limits.maxHeaderSize))
							return
						}
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
