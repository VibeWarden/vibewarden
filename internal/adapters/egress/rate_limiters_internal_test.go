package egress

import (
	"testing"
)

// TestParseRateLimit_TableDriven exercises the parseRateLimit function with
// valid and invalid inputs.
func TestParseRateLimit_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantRPS float64
		wantErr bool
	}{
		{
			name:    "per second",
			expr:    "60/s",
			wantRPS: 60,
		},
		{
			name:    "per minute",
			expr:    "120/m",
			wantRPS: 2, // 120/60
		},
		{
			name:    "per hour",
			expr:    "3600/h",
			wantRPS: 1, // 3600/3600
		},
		{
			name:    "single per minute",
			expr:    "1/m",
			wantRPS: 1.0 / 60.0,
		},
		{
			name:    "spaces around parts",
			expr:    " 10 / s ",
			wantRPS: 10,
		},
		{
			name:    "uppercase unit",
			expr:    "5/S",
			wantRPS: 5,
		},
		{
			name:    "empty expression",
			expr:    "",
			wantErr: true,
		},
		{
			name:    "missing unit",
			expr:    "100",
			wantErr: true,
		},
		{
			name:    "invalid count",
			expr:    "abc/s",
			wantErr: true,
		},
		{
			name:    "zero count",
			expr:    "0/s",
			wantErr: true,
		},
		{
			name:    "negative count",
			expr:    "-5/s",
			wantErr: true,
		},
		{
			name:    "unknown unit",
			expr:    "100/d",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRateLimit(tt.expr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRateLimit(%q) error = %v, wantErr %v", tt.expr, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				const epsilon = 1e-9
				diff := got - tt.wantRPS
				if diff < -epsilon || diff > epsilon {
					t.Errorf("parseRateLimit(%q) = %f, want %f", tt.expr, got, tt.wantRPS)
				}
			}
		})
	}
}

// TestRetryAfterHeader_Values verifies that retryAfterHeader always returns a
// string representing a positive integer >= 1.
func TestRetryAfterHeader_Values(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
		want    string
	}{
		{"zero", 0, "1"},
		{"negative", -5, "1"},
		{"one", 1, "1"},
		{"fractional", 1.3, "2"},
		{"exact two", 2.0, "2"},
		{"large", 300.7, "301"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := retryAfterHeader(tt.seconds)
			if got != tt.want {
				t.Errorf("retryAfterHeader(%f) = %q, want %q", tt.seconds, got, tt.want)
			}
		})
	}
}
