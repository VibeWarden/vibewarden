package waf

import "errors"

// InputLocation identifies which part of an HTTP request contained the
// suspicious input that triggered a WAF rule.
type InputLocation string

const (
	// LocationQueryParam indicates the match was found in a URL query parameter.
	LocationQueryParam InputLocation = "query_param"

	// LocationHeader indicates the match was found in an HTTP request header.
	LocationHeader InputLocation = "header"

	// LocationBody indicates the match was found in the request body.
	LocationBody InputLocation = "body"
)

// Detection is an immutable value object that records a single WAF rule match.
// It captures the rule that fired, where in the request the suspicious input was
// found, and the raw matched value (truncated to a safe length for logging).
type Detection struct {
	rule         Rule
	location     InputLocation
	locationKey  string
	matchedValue string
}

// maxMatchedValueLen is the maximum number of bytes retained from a matched
// input value. This prevents unbounded memory growth when logging large payloads.
const maxMatchedValueLen = 256

// NewDetection constructs a Detection value object.
// Returns an error when location is empty or locationKey is empty.
// matchedValue is silently truncated to maxMatchedValueLen bytes.
func NewDetection(rule Rule, location InputLocation, locationKey, matchedValue string) (Detection, error) {
	if location == "" {
		return Detection{}, errors.New("detection location cannot be empty")
	}
	if locationKey == "" {
		return Detection{}, errors.New("detection location key cannot be empty")
	}
	if len(matchedValue) > maxMatchedValueLen {
		matchedValue = matchedValue[:maxMatchedValueLen]
	}
	return Detection{
		rule:         rule,
		location:     location,
		locationKey:  locationKey,
		matchedValue: matchedValue,
	}, nil
}

// Rule returns the WAF rule that produced this detection.
func (d Detection) Rule() Rule { return d.rule }

// Location returns where in the HTTP request the suspicious input was found.
func (d Detection) Location() InputLocation { return d.location }

// LocationKey returns the specific field name within the location
// (e.g. the query parameter name or header name).
func (d Detection) LocationKey() string { return d.locationKey }

// MatchedValue returns the raw input value that triggered the rule match,
// truncated to 256 bytes.
func (d Detection) MatchedValue() string { return d.matchedValue }
