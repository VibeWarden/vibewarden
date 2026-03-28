package waf

import (
	"errors"
	"io"
	"net/http"
)

// maxBodyBytes is the maximum number of bytes read from a request body during
// WAF inspection. Reading the entire body of a large upload is unnecessary and
// wasteful — the vast majority of injection payloads appear in the first 8 KB.
const maxBodyBytes = 8 * 1024 // 8 KB

// RuleSet is an aggregate that holds an ordered collection of WAF rules and
// exposes methods to evaluate inputs against them.
//
// RuleSet is immutable after construction — callers cannot add or remove rules
// once it is built. Use NewRuleSet or DefaultRuleSet to obtain an instance.
type RuleSet struct {
	rules []Rule
}

// NewRuleSet constructs a RuleSet from the provided rules.
// Returns an error when rules is empty.
func NewRuleSet(rules []Rule) (RuleSet, error) {
	if len(rules) == 0 {
		return RuleSet{}, errors.New("rule set must contain at least one rule")
	}
	cp := make([]Rule, len(rules))
	copy(cp, rules)
	return RuleSet{rules: cp}, nil
}

// DefaultRuleSet returns a RuleSet pre-loaded with all built-in detection rules.
// It panics only if the built-in patterns are invalid — a programming error
// caught at startup.
func DefaultRuleSet() RuleSet {
	rs, err := NewRuleSet(BuiltinRules())
	if err != nil {
		panic("waf: failed to build default rule set: " + err.Error())
	}
	return rs
}

// Rules returns a copy of the rules in this RuleSet.
func (rs RuleSet) Rules() []Rule {
	cp := make([]Rule, len(rs.rules))
	copy(cp, rs.rules)
	return cp
}

// Evaluate tests input against every rule in the set and returns all matches.
// The returned slice is nil when no rules fire.
func (rs RuleSet) Evaluate(location InputLocation, locationKey, input string) []Detection {
	var detections []Detection
	for _, rule := range rs.rules {
		if rule.MatchString(input) {
			d, err := NewDetection(rule, location, locationKey, input)
			if err != nil {
				// NewDetection only errors on empty location/key, which cannot
				// happen here since callers pass non-empty values through
				// ScanRequest. Skip defensively.
				continue
			}
			detections = append(detections, d)
		}
	}
	return detections
}

// headersToInspect is the set of HTTP headers scanned for injection payloads.
// Adding every header is prohibitively expensive; these are the most commonly
// abused in WAF bypass attempts.
var headersToInspect = []string{
	"Cookie",
	"Referer",
	"User-Agent",
}

// ScanRequest inspects the HTTP request and returns all WAF detections.
// It checks:
//   - All URL query parameters
//   - Cookie, Referer, and User-Agent headers
//   - The first 8 KB of the request body (body is replaced with a new reader
//     after reading so downstream handlers are unaffected)
//
// ScanRequest returns a nil slice when no rules fire.
func (rs RuleSet) ScanRequest(r *http.Request) ([]Detection, error) {
	var all []Detection

	// --- query parameters ---
	for key, values := range r.URL.Query() {
		for _, v := range values {
			all = append(all, rs.Evaluate(LocationQueryParam, key, v)...)
		}
	}

	// --- selected headers ---
	for _, name := range headersToInspect {
		if val := r.Header.Get(name); val != "" {
			all = append(all, rs.Evaluate(LocationHeader, name, val)...)
		}
	}

	// --- body (first 8 KB) ---
	if r.Body != nil {
		chunk := make([]byte, maxBodyBytes)
		n, err := io.ReadFull(r.Body, chunk)
		// io.ReadFull returns io.ErrUnexpectedEOF when the body is shorter than
		// the buffer — that is the normal case and is not an error.
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}
		_ = r.Body.Close()

		if n > 0 {
			bodySnippet := string(chunk[:n])
			// Restore the body so downstream handlers can read it. We already
			// consumed up to 8 KB; the rest (if any) is still in the original
			// reader. To keep things simple we restore only the consumed portion.
			r.Body = io.NopCloser(io.MultiReader(
				&stringReader{s: bodySnippet},
				r.Body, // remainder (may be empty)
			))
			all = append(all, rs.Evaluate(LocationBody, "body", bodySnippet)...)
		}
	}

	return all, nil
}

// stringReader wraps a string as an io.Reader so we can restore the request body
// without importing the strings package at the top level.
type stringReader struct{ s string }

func (r *stringReader) Read(p []byte) (int, error) {
	if len(r.s) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.s)
	r.s = r.s[n:]
	return n, nil
}
