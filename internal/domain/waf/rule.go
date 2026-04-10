// Package waf defines the domain model for the Web Application Firewall rule engine.
// This package has zero external dependencies — only the Go standard library.
//
// The WAF rule engine evaluates HTTP request inputs against a compiled set of
// detection patterns and produces a list of Detections describing any matches.
// All types are value objects — immutable after construction.
package waf

import (
	"errors"
	"fmt"
	"regexp"
)

// Severity classifies the danger level of a matched WAF rule.
// Higher severity events warrant more aggressive responses (e.g. block vs. log).
type Severity string

const (
	// SeverityLow indicates a low-risk pattern match, suitable for logging only.
	SeverityLow Severity = "low"

	// SeverityMedium indicates a medium-risk pattern match that should be reviewed.
	SeverityMedium Severity = "medium"

	// SeverityHigh indicates a high-risk pattern match that should be blocked.
	SeverityHigh Severity = "high"

	// SeverityCritical indicates a critical-risk pattern match (e.g. known exploit
	// payloads) that must be blocked immediately.
	SeverityCritical Severity = "critical"
)

// Category groups rules by the class of attack they detect.
type Category string

const (
	// CategorySQLInjection covers SQL injection attack patterns.
	CategorySQLInjection Category = "sqli"

	// CategoryXSS covers cross-site scripting attack patterns.
	CategoryXSS Category = "xss"

	// CategoryPathTraversal covers directory traversal and path escape patterns.
	CategoryPathTraversal Category = "path_traversal"

	// CategoryCommandInjection covers OS command injection patterns.
	CategoryCommandInjection Category = "cmd_injection"
)

// Rule is an immutable value object representing a single WAF detection rule.
// It pairs a human-readable name and metadata with a pre-compiled regular
// expression for efficient repeated matching.
//
// Rules are equal when their names are equal — the name is the natural key.
type Rule struct {
	name     string
	pattern  *regexp.Regexp
	severity Severity
	category Category
}

// NewRule constructs a Rule from the given parameters.
// Returns an error when name is empty, pattern is empty, severity is empty,
// or category is empty.
//
// The pattern is compiled with case-insensitive matching ((?i)) so callers
// should write patterns without embedded flags.
func NewRule(name, pattern string, severity Severity, category Category) (Rule, error) {
	if name == "" {
		return Rule{}, errors.New("rule name cannot be empty")
	}
	if pattern == "" {
		return Rule{}, errors.New("rule pattern cannot be empty")
	}
	if severity == "" {
		return Rule{}, errors.New("rule severity cannot be empty")
	}
	if category == "" {
		return Rule{}, errors.New("rule category cannot be empty")
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return Rule{}, fmt.Errorf("compiling rule pattern %q: %w", pattern, err)
	}
	return Rule{
		name:     name,
		pattern:  re,
		severity: severity,
		category: category,
	}, nil
}

// Name returns the human-readable identifier for this rule.
func (r Rule) Name() string { return r.name }

// Severity returns the danger classification of this rule.
func (r Rule) Severity() Severity { return r.severity }

// Category returns the attack category this rule detects.
func (r Rule) Category() Category { return r.category }

// MatchString reports whether the rule's pattern matches input.
func (r Rule) MatchString(input string) bool {
	return r.pattern.MatchString(input)
}
