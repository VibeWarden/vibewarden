package egress

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/domain/events"
)

const (
	// redacted is the placeholder value written in place of sensitive data.
	redacted = "[REDACTED]"

	// contentTypeJSON is the MIME type prefix that enables body field redaction.
	contentTypeJSON = "application/json"
)

// SanitizeResult reports how many fields were redacted in each category.
type SanitizeResult struct {
	// RedactedHeaders is the count of header values replaced with "[REDACTED]"
	// in the log-safe copy.
	RedactedHeaders int

	// StrippedQueryParams is the count of query parameters removed from the URL.
	StrippedQueryParams int

	// RedactedBodyFields is the count of JSON body field values replaced with
	// "[REDACTED]".
	RedactedBodyFields int
}

// Total returns the sum of all redacted field counts.
func (r SanitizeResult) Total() int {
	return r.RedactedHeaders + r.StrippedQueryParams + r.RedactedBodyFields
}

// sanitizeRequest applies the per-route PII redaction rules to req and returns:
//   - the modified request (URL query params and body are mutated in-place),
//   - a log-safe header map where sensitive values are replaced with "[REDACTED]",
//   - a SanitizeResult reporting the counts of each redacted category.
//
// Header values in the actual forwarded request are preserved unchanged;
// only the returned logHeaders copy carries the redacted values.
//
// Body redaction is only applied when the request Content-Type is
// application/json. Non-JSON bodies are forwarded unchanged.
func sanitizeRequest(
	ctx context.Context,
	req domainegress.EgressRequest,
	cfg domainegress.SanitizeConfig,
) (domainegress.EgressRequest, http.Header, SanitizeResult, error) {
	var result SanitizeResult

	// --- Query param stripping ---
	if len(cfg.QueryParams) > 0 {
		stripped, n, err := stripQueryParams(req.URL, cfg.QueryParams)
		if err != nil {
			return req, req.Header, result, err
		}
		req.URL = stripped
		result.StrippedQueryParams = n
	}

	// --- Header log-redaction ---
	// logHeaders is the copy used in structured events; the forwarded request
	// still gets the original header values.
	logHeaders := req.Header.Clone()
	if len(cfg.Headers) > 0 {
		redactedSet := cfg.RedactedHeaders()
		for name := range logHeaders {
			if _, sensitive := redactedSet[strings.ToLower(name)]; sensitive {
				logHeaders[name] = []string{redacted}
				result.RedactedHeaders++
			}
		}
	}

	// --- Body field redaction ---
	if len(cfg.BodyFields) > 0 && isJSONContentType(req.Header.Get("Content-Type")) {
		body, ok := req.BodyRef.(io.Reader)
		if ok && body != nil {
			raw, err := io.ReadAll(body)
			if err != nil {
				return req, logHeaders, result, err
			}
			redactedBody, n := redactJSONFields(raw, cfg.BodyFields)
			req.BodyRef = io.NopCloser(bytes.NewReader(redactedBody))
			result.RedactedBodyFields = n
		}
	}

	_ = ctx // context reserved for future async operations (e.g. audit logging)
	return req, logHeaders, result, nil
}

// stripQueryParams removes each named parameter from rawURL and returns the
// modified URL string together with the count of parameters actually removed.
func stripQueryParams(rawURL string, params []string) (string, int, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, 0, nil // malformed URL — leave unchanged, proxy will handle it
	}

	q := u.Query()
	count := 0
	for _, p := range params {
		if _, exists := q[p]; exists {
			q.Del(p)
			count++
		}
	}
	if count == 0 {
		return rawURL, 0, nil
	}
	u.RawQuery = q.Encode()
	return u.String(), count, nil
}

// isJSONContentType reports whether the Content-Type header value indicates
// an application/json body (ignoring any charset parameter).
func isJSONContentType(ct string) bool {
	return strings.HasPrefix(strings.TrimSpace(ct), contentTypeJSON)
}

// redactJSONFields replaces the value of each named JSON field in body with
// "[REDACTED]". It uses a simple regex-based substitution that handles both
// string values (e.g. "password":"secret") and handles nested occurrences.
// Returns the modified body and the count of fields that were actually replaced.
//
// The regex matches:
//
//	"<field>"\s*:\s*"<any value without closing quote>"
//
// and replaces only the value portion. This handles top-level and nested
// string fields but does not support non-string values (numbers, booleans)
// as PII is almost exclusively transmitted as strings.
func redactJSONFields(body []byte, fields []string) ([]byte, int) {
	total := 0
	result := body
	for _, field := range fields {
		// Escape the field name for use in a regex literal.
		escapedField := regexp.QuoteMeta(field)
		// Match: "fieldname"\s*:\s*"value"
		// Capture group 1: everything up to and including the colon + whitespace.
		// We replace the string value (between the quotes after the colon) with [REDACTED].
		pattern := `("` + escapedField + `"\s*:\s*)"[^"]*"`
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Skip invalid patterns silently — the field name itself is the issue.
			continue
		}
		var matched bool
		result = re.ReplaceAllFunc(result, func(b []byte) []byte {
			matched = true
			// re.ReplaceAllLiteral would replace the whole match; we want to keep
			// the key portion (group 1). ReplaceAllFunc gives us the full match, so
			// we re-run FindSubmatch to extract the prefix.
			sub := re.FindSubmatch(b)
			if sub == nil {
				return b
			}
			return append(sub[1], `"`+redacted+`"`...)
		})
		if matched {
			total++
		}
	}
	return result, total
}

// emitSanitized logs an egress.sanitized structured event when at least one
// field was redacted. It is a no-op when the EventLogger is nil or when
// result.Total() == 0.
func (p *Proxy) emitSanitized(
	ctx context.Context,
	routeName string,
	req domainegress.EgressRequest,
	result SanitizeResult,
) {
	if p.cfg.EventLogger == nil || result.Total() == 0 {
		return
	}
	ev := events.NewEgressSanitized(events.EgressSanitizedParams{
		Route:               routeName,
		Method:              req.Method,
		URL:                 req.URL,
		RedactedHeaders:     result.RedactedHeaders,
		StrippedQueryParams: result.StrippedQueryParams,
		RedactedBodyFields:  result.RedactedBodyFields,
		TraceID:             traceIDFromContext(ctx),
	})
	_ = p.cfg.EventLogger.Log(ctx, ev)
}
