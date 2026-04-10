package waf

import (
	"strings"
	"testing"
)

func TestNewDetection_Validation(t *testing.T) {
	rule, _ := NewRule("test-rule", `union`, SeverityHigh, CategorySQLInjection)

	tests := []struct {
		name        string
		location    InputLocation
		locationKey string
		value       string
		wantErr     bool
	}{
		{
			name:        "valid detection",
			location:    LocationQueryParam,
			locationKey: "q",
			value:       "UNION SELECT 1",
			wantErr:     false,
		},
		{
			name:        "empty location",
			location:    "",
			locationKey: "q",
			value:       "UNION SELECT 1",
			wantErr:     true,
		},
		{
			name:        "empty location key",
			location:    LocationQueryParam,
			locationKey: "",
			value:       "UNION SELECT 1",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDetection(rule, tt.location, tt.locationKey, tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewDetection() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if d.Rule().Name() != rule.Name() {
					t.Errorf("Rule().Name() = %q, want %q", d.Rule().Name(), rule.Name())
				}
				if d.Location() != tt.location {
					t.Errorf("Location() = %q, want %q", d.Location(), tt.location)
				}
				if d.LocationKey() != tt.locationKey {
					t.Errorf("LocationKey() = %q, want %q", d.LocationKey(), tt.locationKey)
				}
			}
		})
	}
}

func TestNewDetection_MatchedValueTruncation(t *testing.T) {
	rule, _ := NewRule("test-rule", `x`, SeverityLow, CategoryXSS)

	// Build a value longer than maxMatchedValueLen (256).
	longValue := strings.Repeat("x", 512)

	d, err := NewDetection(rule, LocationBody, "body", longValue)
	if err != nil {
		t.Fatalf("NewDetection() unexpected error: %v", err)
	}
	if len(d.MatchedValue()) != maxMatchedValueLen {
		t.Errorf("MatchedValue() length = %d, want %d", len(d.MatchedValue()), maxMatchedValueLen)
	}
}

func TestNewDetection_ShortValueNotTruncated(t *testing.T) {
	rule, _ := NewRule("test-rule", `x`, SeverityLow, CategoryXSS)
	value := "short"

	d, err := NewDetection(rule, LocationHeader, "User-Agent", value)
	if err != nil {
		t.Fatalf("NewDetection() unexpected error: %v", err)
	}
	if d.MatchedValue() != value {
		t.Errorf("MatchedValue() = %q, want %q", d.MatchedValue(), value)
	}
}
