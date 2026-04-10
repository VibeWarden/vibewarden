package egress

import (
	"testing"
)

func TestSanitizeConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  SanitizeConfig
		want bool
	}{
		{
			name: "empty config is zero",
			cfg:  SanitizeConfig{},
			want: true,
		},
		{
			name: "config with headers is not zero",
			cfg:  SanitizeConfig{Headers: []string{"Authorization"}},
			want: false,
		},
		{
			name: "config with query params is not zero",
			cfg:  SanitizeConfig{QueryParams: []string{"api_key"}},
			want: false,
		},
		{
			name: "config with body fields is not zero",
			cfg:  SanitizeConfig{BodyFields: []string{"password"}},
			want: false,
		},
		{
			name: "config with all fields is not zero",
			cfg: SanitizeConfig{
				Headers:     []string{"Cookie"},
				QueryParams: []string{"token"},
				BodyFields:  []string{"ssn"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsZero()
			if got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeConfig_RedactedHeaders(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SanitizeConfig
		check   string
		present bool
	}{
		{
			name:    "nil config returns nil map",
			cfg:     SanitizeConfig{},
			check:   "authorization",
			present: false,
		},
		{
			name:    "header matched case-insensitively — exact case",
			cfg:     SanitizeConfig{Headers: []string{"Authorization"}},
			check:   "authorization",
			present: true,
		},
		{
			name:    "header matched case-insensitively — mixed case input",
			cfg:     SanitizeConfig{Headers: []string{"COOKIE"}},
			check:   "cookie",
			present: true,
		},
		{
			name:    "header not present",
			cfg:     SanitizeConfig{Headers: []string{"X-Custom"}},
			check:   "authorization",
			present: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.cfg.RedactedHeaders()
			_, got := m[tt.check]
			if got != tt.present {
				t.Errorf("RedactedHeaders()[%q] present = %v, want %v", tt.check, got, tt.present)
			}
		})
	}
}
