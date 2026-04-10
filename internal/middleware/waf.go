package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vibewarden/vibewarden/internal/domain/audit"
	"github.com/vibewarden/vibewarden/internal/domain/waf"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// WAFMode controls the WAF response strategy.
type WAFMode string

const (
	// WAFModeBlock rejects requests that match a WAF rule with 403 Forbidden.
	WAFModeBlock WAFMode = "block"

	// WAFModeDetect logs detections and passes requests through unchanged.
	WAFModeDetect WAFMode = "detect"
)

// WAFConfig holds the complete configuration for WAFMiddleware.
type WAFConfig struct {
	// Mode controls whether detections block or only log. Default: WAFModeDetect.
	Mode WAFMode

	// EnabledCategories maps a waf.Category to a toggle. Categories absent from
	// the map (or mapped to true) are enabled.
	EnabledCategories map[waf.Category]bool

	// ExemptPaths is the list of URL path glob patterns that bypass WAF scanning.
	// The /_vibewarden/* prefix is always exempt.
	ExemptPaths []string
}

// WAFMiddleware returns an HTTP middleware that scans every incoming request
// against the provided RuleSet.
//
// Behaviour:
//   - Paths that match an exempt pattern (including /_vibewarden/*) are passed
//     through without scanning.
//   - Enabled categories are determined by cfg.EnabledCategories; a missing key
//     defaults to enabled.
//   - In WAFModeBlock: the first matching detection produces a 403 Forbidden
//     JSON response (error code "waf_blocked", detail includes rule name).
//   - In WAFModeDetect: all detections are logged and the request passes through.
//   - If auditLogger is non-nil, an audit.AuditEvent is emitted for every
//     detection/block using audit.EventTypeWAFDetection or
//     audit.EventTypeWAFBlocked.
//   - If mc is non-nil, vibewarden_waf_detections_total{rule,mode} is incremented
//     for every detection.
//
// The logger must not be nil; pass slog.Default() if no custom logger is needed.
func WAFMiddleware(
	rs waf.RuleSet,
	cfg WAFConfig,
	logger *slog.Logger,
	mc ports.MetricsCollector,
	auditLogger ports.AuditEventLogger,
) func(http.Handler) http.Handler {
	mode := cfg.Mode
	if mode != WAFModeBlock && mode != WAFModeDetect {
		mode = WAFModeDetect
	}

	matcher, err := NewExemptPathMatcher(cfg.ExemptPaths)
	if err != nil {
		logger.Error("waf middleware: invalid exempt path patterns, falling back to empty list",
			slog.String("error", err.Error()),
		)
		matcher, _ = NewExemptPathMatcher(nil) //nolint:errcheck // nil patterns are always valid
	}

	modeStr := string(mode)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass exempt paths.
			if matcher.Matches(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			detections, err := rs.ScanRequest(r)
			if err != nil {
				// Scanning failed (unlikely — only body read errors reach here).
				// Fail open: log and pass through.
				logger.Error("waf middleware: scanning request", slog.String("error", err.Error()))
				next.ServeHTTP(w, r)
				return
			}

			for _, d := range detections {
				// Skip categories that the operator has disabled.
				if enabled, ok := cfg.EnabledCategories[d.Rule().Category()]; ok && !enabled {
					continue
				}

				// Increment metric.
				if mc != nil {
					mc.IncWAFDetection(d.Rule().Name(), modeStr)
				}

				if mode == WAFModeBlock {
					// Emit audit event before writing the response.
					emitWAFAuditEvent(r, auditLogger, audit.EventTypeWAFBlocked, d, modeStr)

					logger.Warn("waf: request blocked",
						slog.String("rule", d.Rule().Name()),
						slog.String("category", string(d.Rule().Category())),
						slog.String("severity", string(d.Rule().Severity())),
						slog.String("location", string(d.Location())),
						slog.String("location_key", d.LocationKey()),
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
					)

					WriteErrorResponse(w, r, http.StatusForbidden, "waf_blocked",
						fmt.Sprintf("request blocked by WAF rule %q: %s", d.Rule().Name(), wafDetail(d)))
					return
				}

				// Detect mode: log and continue.
				emitWAFAuditEvent(r, auditLogger, audit.EventTypeWAFDetection, d, modeStr)

				logger.Warn("waf: attack detected (detect mode — request allowed)",
					slog.String("rule", d.Rule().Name()),
					slog.String("category", string(d.Rule().Category())),
					slog.String("severity", string(d.Rule().Severity())),
					slog.String("location", string(d.Location())),
					slog.String("location_key", d.LocationKey()),
					slog.String("path", r.URL.Path),
					slog.String("method", r.Method),
				)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// wafDetail returns a short human-readable description of a detection for use
// in the error message and audit details.
func wafDetail(d waf.Detection) string {
	return fmt.Sprintf("matched %s in %s %q",
		d.Rule().Category(), d.Location(), d.LocationKey())
}

// emitWAFAuditEvent records a WAF audit event if auditLogger is non-nil.
// Best-effort: errors are silently discarded so request processing is never
// interrupted by audit logging failures.
func emitWAFAuditEvent(
	r *http.Request,
	auditLogger ports.AuditEventLogger,
	eventType audit.EventType,
	d waf.Detection,
	mode string,
) {
	if auditLogger == nil {
		return
	}

	outcome := audit.OutcomeFailure
	if eventType == audit.EventTypeWAFDetection {
		// In detect mode the request proceeds — outcome is informational.
		outcome = audit.OutcomeSuccess
	}

	ev, err := audit.NewAuditEvent(
		eventType,
		audit.Actor{IP: extractClientIPOrEmpty(r)},
		audit.Target{Path: r.URL.Path},
		outcome,
		CorrelationID(r.Context()),
		map[string]any{
			"method":        r.Method,
			"rule":          d.Rule().Name(),
			"category":      string(d.Rule().Category()),
			"severity":      string(d.Rule().Severity()),
			"location":      string(d.Location()),
			"location_key":  d.LocationKey(),
			"matched_value": d.MatchedValue(),
			"mode":          mode,
		},
	)
	if err != nil {
		return
	}
	// Best-effort.
	_ = auditLogger.Log(r.Context(), ev)
}

// extractClientIPOrEmpty returns the client IP from RemoteAddr, stripping the
// port. Returns an empty string on parse failure. This helper avoids importing
// ExtractClientIP's full proxy-trust logic for the audit actor field, which is
// used for logging only.
func extractClientIPOrEmpty(r *http.Request) string {
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
