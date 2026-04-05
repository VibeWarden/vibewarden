// Package llm defines the domain model for LLM egress security controls.
// This package has zero external dependencies — only the Go standard library.
//
// The prompt injection detector evaluates text content extracted from outbound
// LLM API requests and identifies injection payloads using a compiled set of
// regex and keyword patterns.
package llm

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// defaultPatternSpecs holds the built-in prompt injection detection patterns
// shipped with VibeWarden. Patterns are matched case-insensitively.
//
// The set covers the most common direct injection phrases while staying
// practical — it is not exhaustive. Operators can extend it via ExtraPatterns.
var defaultPatternSpecs = []patternSpec{
	// Instruction override phrases
	{name: "ignore-previous-instructions", pattern: `ignore\s+(previous|prior|all|above)\s+instructions?`},
	{name: "disregard-all-prior", pattern: `disregard\s+(all\s+)?(prior|previous|above)\s+(instructions?|context|rules?|constraints?)`},
	{name: "forget-everything", pattern: `forget\s+(everything|all|your\s+previous|prior)`},
	{name: "override-instructions", pattern: `(override|bypass|ignore|discard)\s+(your\s+)?(system|initial|original)\s+(instructions?|prompt|constraints?|rules?)`},

	// Role / identity manipulation
	{name: "you-are-now", pattern: `you\s+are\s+now\s+\w`},
	{name: "act-as-if", pattern: `act\s+as\s+(if|though|like)\s+you`},
	{name: "pretend-you-are", pattern: `pretend\s+(you\s+are|to\s+be)`},
	{name: "roleplay-as", pattern: `(role\s*play|roleplay)\s+as\s+\w`},
	{name: "your-new-role", pattern: `your\s+new\s+(role|persona|identity|name|purpose)\s+is`},
	{name: "simulate-mode", pattern: `(simulate|enable|activate)\s+(developer|jailbreak|unrestricted|god|dan)\s+mode`},

	// System prompt extraction
	{name: "system-prompt-label", pattern: `system\s*prompt\s*:`},
	{name: "repeat-system-prompt", pattern: `(repeat|reveal|show|print|output|display)\s+(your\s+)?(system\s+prompt|initial\s+instructions?|original\s+prompt)`},
	{name: "what-were-your-instructions", pattern: `what\s+(were|are)\s+your\s+(instructions?|rules?|constraints?|system\s+prompt)`},

	// DAN / jailbreak keywords
	{name: "jailbreak-dan", pattern: `\bDAN\b.{0,30}(mode|enabled|activated|persona)`},
	{name: "do-anything-now", pattern: `do\s+anything\s+now`},
	{name: "no-restrictions", pattern: `(no\s+restrictions?|without\s+restrictions?|remove\s+(all\s+)?restrictions?)`},

	// Unicode/encoding evasion — homoglyph and zero-width characters
	{name: "zero-width-chars", pattern: `[\x{200B}\x{200C}\x{200D}\x{FEFF}\x{00AD}]`},
	{name: "homoglyph-cyrillic", pattern: `[\x{0430}\x{0435}\x{043e}\x{0440}\x{0441}\x{0443}\x{0445}]`},
}

// patternSpec is a compile-time specification for a single detection pattern.
type patternSpec struct {
	name    string
	pattern string
}

// Pattern is an immutable value object pairing a human-readable name with a
// pre-compiled case-insensitive regular expression for efficient matching.
type Pattern struct {
	name string
	re   *regexp.Regexp
}

// NewPattern constructs a Pattern from the given name and regex string.
// Returns an error when name is empty, pattern is empty, or the pattern
// fails to compile.
//
// The pattern is compiled with case-insensitive matching ((?i)) so callers
// should write patterns without embedded flags.
func NewPattern(name, pattern string) (Pattern, error) {
	if name == "" {
		return Pattern{}, errors.New("pattern name cannot be empty")
	}
	if pattern == "" {
		return Pattern{}, errors.New("pattern expression cannot be empty")
	}
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return Pattern{}, err
	}
	return Pattern{name: name, re: re}, nil
}

// Name returns the human-readable identifier for this pattern.
func (p Pattern) Name() string { return p.name }

// Matches reports whether the pattern matches the given text.
func (p Pattern) Matches(text string) bool {
	return p.re.MatchString(text)
}

// Detector is an immutable value object that holds a compiled set of prompt
// injection detection patterns and exposes a Detect method for scanning text.
//
// Use NewDetector to construct a Detector from a custom pattern set, or
// DefaultDetector to obtain a pre-loaded instance with all built-in patterns.
type Detector struct {
	patterns []Pattern
}

// NewDetector constructs a Detector from the provided patterns.
// Returns an error when patterns is empty.
func NewDetector(patterns []Pattern) (Detector, error) {
	if len(patterns) == 0 {
		return Detector{}, errors.New("detector must contain at least one pattern")
	}
	cp := make([]Pattern, len(patterns))
	copy(cp, patterns)
	return Detector{patterns: cp}, nil
}

// DefaultDetector returns a Detector pre-loaded with all built-in prompt
// injection detection patterns.
//
// It panics only if the built-in patterns are invalid — a programming error
// that is caught at startup.
func DefaultDetector() Detector {
	d, err := NewDetector(BuiltinPatterns())
	if err != nil {
		panic("llm: failed to build default detector: " + err.Error())
	}
	return d
}

// Patterns returns a copy of the patterns held by this Detector.
func (d Detector) Patterns() []Pattern {
	cp := make([]Pattern, len(d.patterns))
	copy(cp, d.patterns)
	return cp
}

// Detect scans text for prompt injection payloads.
//
// It returns (true, patternName) on the first match, where patternName is the
// Name() of the Pattern that fired.
// It returns (false, "") when no pattern matches.
//
// The text is normalised by collapsing repeated whitespace before matching, so
// payloads padded with extra spaces are still caught.
func (d Detector) Detect(text string) (detected bool, patternName string) {
	normalised := normaliseWhitespace(text)
	for _, p := range d.patterns {
		if p.Matches(normalised) {
			return true, p.Name()
		}
	}
	return false, ""
}

// normaliseWhitespace replaces runs of whitespace (spaces, tabs, newlines,
// carriage returns) with a single space. This collapses payload fragments
// that use whitespace padding to evade keyword matching.
func normaliseWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// BuiltinPatterns returns the compiled set of built-in prompt injection
// detection patterns. It panics if any built-in pattern fails to compile —
// this is a programming error that must be caught at startup.
func BuiltinPatterns() []Pattern {
	patterns := make([]Pattern, 0, len(defaultPatternSpecs))
	for _, spec := range defaultPatternSpecs {
		p, err := NewPattern(spec.name, spec.pattern)
		if err != nil {
			panic("llm: invalid built-in pattern " + spec.name + ": " + err.Error())
		}
		patterns = append(patterns, p)
	}
	return patterns
}

// NewDetectorWithExtra constructs a Detector that combines the built-in
// patterns with the additional raw regex strings supplied by the caller.
//
// extraPatterns is a slice of raw regex strings. Each extra pattern is
// named "extra-<n>" where n is its zero-based index in the slice.
// Returns an error when any extra pattern fails to compile.
func NewDetectorWithExtra(extraPatterns []string) (Detector, error) {
	patterns := BuiltinPatterns()
	for i, raw := range extraPatterns {
		name := fmt.Sprintf("extra-%d", i)
		p, err := NewPattern(name, raw)
		if err != nil {
			return Detector{}, fmt.Errorf("extra pattern %d %q: %w", i, raw, err)
		}
		patterns = append(patterns, p)
	}
	return NewDetector(patterns)
}
