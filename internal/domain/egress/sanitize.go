package egress

import "strings"

// SanitizeConfig holds the per-route PII redaction rules applied to outbound
// requests before they are forwarded and before sensitive values are logged.
//
// Header values listed in Headers are replaced with "[REDACTED]" in log output
// but are preserved unchanged in the actual forwarded request.
// Query parameters listed in QueryParams are stripped from the request URL
// before forwarding.
// JSON body fields listed in BodyFields are replaced with "[REDACTED]" in the
// request body before forwarding. Redaction is only applied when the request
// Content-Type is application/json.
type SanitizeConfig struct {
	// Headers is the list of request header names whose values are redacted in
	// structured log events (e.g. "Authorization", "Cookie").
	// Header names are matched case-insensitively.
	// The header value is preserved in the actual forwarded request.
	Headers []string

	// QueryParams is the list of query parameter names to strip from the
	// request URL before forwarding (e.g. "api_key", "token").
	// Parameter names are matched case-sensitively.
	QueryParams []string

	// BodyFields is the list of JSON field names to redact in the request body
	// before forwarding (e.g. "password", "ssn", "card_number").
	// Field names are matched case-sensitively against top-level and nested
	// JSON keys using a simple regex-based substitution.
	// Redaction is only applied when Content-Type is application/json.
	BodyFields []string
}

// IsZero reports whether the SanitizeConfig has no rules configured.
func (s SanitizeConfig) IsZero() bool {
	return len(s.Headers) == 0 && len(s.QueryParams) == 0 && len(s.BodyFields) == 0
}

// RedactedHeaders returns the set of header names that should be redacted
// in log output, normalised to their canonical HTTP header form.
func (s SanitizeConfig) RedactedHeaders() map[string]struct{} {
	if len(s.Headers) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(s.Headers))
	for _, h := range s.Headers {
		out[strings.ToLower(h)] = struct{}{}
	}
	return out
}
