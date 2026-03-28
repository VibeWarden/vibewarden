package waf

import (
	"strings"
	"testing"
)

func TestNewRule_Validation(t *testing.T) {
	tests := []struct {
		name     string
		ruleName string
		pattern  string
		severity Severity
		category Category
		wantErr  bool
	}{
		{
			name:     "valid rule",
			ruleName: "test-rule",
			pattern:  `union[\s]+select`,
			severity: SeverityHigh,
			category: CategorySQLInjection,
			wantErr:  false,
		},
		{
			name:     "empty name",
			ruleName: "",
			pattern:  `union[\s]+select`,
			severity: SeverityHigh,
			category: CategorySQLInjection,
			wantErr:  true,
		},
		{
			name:     "empty pattern",
			ruleName: "test-rule",
			pattern:  "",
			severity: SeverityHigh,
			category: CategorySQLInjection,
			wantErr:  true,
		},
		{
			name:     "empty severity",
			ruleName: "test-rule",
			pattern:  `union[\s]+select`,
			severity: "",
			category: CategorySQLInjection,
			wantErr:  true,
		},
		{
			name:     "empty category",
			ruleName: "test-rule",
			pattern:  `union[\s]+select`,
			severity: SeverityHigh,
			category: "",
			wantErr:  true,
		},
		{
			name:     "invalid regex",
			ruleName: "test-rule",
			pattern:  `[invalid`,
			severity: SeverityHigh,
			category: CategorySQLInjection,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewRule(tt.ruleName, tt.pattern, tt.severity, tt.category)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if r.Name() != tt.ruleName {
					t.Errorf("Name() = %q, want %q", r.Name(), tt.ruleName)
				}
				if r.Severity() != tt.severity {
					t.Errorf("Severity() = %q, want %q", r.Severity(), tt.severity)
				}
				if r.Category() != tt.category {
					t.Errorf("Category() = %q, want %q", r.Category(), tt.category)
				}
			}
		})
	}
}

func TestRule_MatchString_CaseInsensitive(t *testing.T) {
	r, err := NewRule("test", `union[\s]+select`, SeverityHigh, CategorySQLInjection)
	if err != nil {
		t.Fatalf("NewRule() unexpected error: %v", err)
	}

	tests := []struct {
		input string
		want  bool
	}{
		{"UNION SELECT", true},
		{"union select", true},
		{"Union Select", true},
		{"hello world", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := r.MatchString(tt.input)
			if got != tt.want {
				t.Errorf("MatchString(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestRule_PatternCompiled(t *testing.T) {
	// Verify that the (?i) flag is prepended automatically.
	r, err := NewRule("ci-test", "hello", SeverityLow, CategoryXSS)
	if err != nil {
		t.Fatalf("NewRule() unexpected error: %v", err)
	}
	if !r.MatchString("HELLO") {
		t.Error("expected case-insensitive match for HELLO with pattern hello")
	}
	// Verify the pattern string is correct.
	if !strings.Contains(r.pattern.String(), "(?i)") {
		t.Errorf("compiled pattern %q should contain (?i)", r.pattern.String())
	}
}
