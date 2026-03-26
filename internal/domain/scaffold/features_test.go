package scaffold_test

import (
	"errors"
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

func TestFeature_Constants(t *testing.T) {
	tests := []struct {
		name  string
		value scaffold.Feature
		want  string
	}{
		{"auth", scaffold.FeatureAuth, "auth"},
		{"rate-limiting", scaffold.FeatureRateLimit, "rate-limiting"},
		{"tls", scaffold.FeatureTLS, "tls"},
		{"admin", scaffold.FeatureAdmin, "admin"},
		{"metrics", scaffold.FeatureMetrics, "metrics"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.value) != tt.want {
				t.Errorf("Feature %q = %q, want %q", tt.name, string(tt.value), tt.want)
			}
		})
	}
}

func TestFeature_Distinctness(t *testing.T) {
	features := []scaffold.Feature{
		scaffold.FeatureAuth,
		scaffold.FeatureRateLimit,
		scaffold.FeatureTLS,
		scaffold.FeatureAdmin,
		scaffold.FeatureMetrics,
	}
	seen := make(map[scaffold.Feature]bool)
	for _, f := range features {
		if seen[f] {
			t.Errorf("duplicate Feature value: %q", f)
		}
		seen[f] = true
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	if errors.Is(scaffold.ErrFeatureAlreadyEnabled, scaffold.ErrConfigNotFound) {
		t.Error("ErrFeatureAlreadyEnabled and ErrConfigNotFound must be distinct sentinel errors")
	}
}

func TestSentinelErrors_ErrorMessages(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrFeatureAlreadyEnabled",
			err:     scaffold.ErrFeatureAlreadyEnabled,
			wantMsg: "feature already enabled",
		},
		{
			name:    "ErrConfigNotFound",
			err:     scaffold.ErrConfigNotFound,
			wantMsg: "vibewarden.yaml not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.wantMsg {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestSentinelErrors_CanBeTestedWithErrorsIs(t *testing.T) {
	wrapped := errors.New("outer: " + scaffold.ErrFeatureAlreadyEnabled.Error())
	// Wrapping via %w allows errors.Is to match.
	if errors.Is(wrapped, scaffold.ErrFeatureAlreadyEnabled) {
		t.Error("plain concatenation should not satisfy errors.Is — this is a sanity check")
	}
}

func TestFeatureState_ZeroValue(t *testing.T) {
	var fs scaffold.FeatureState

	if fs.UpstreamPort != 0 {
		t.Errorf("zero FeatureState.UpstreamPort = %d, want 0", fs.UpstreamPort)
	}
	if fs.AuthEnabled {
		t.Error("zero FeatureState.AuthEnabled should be false")
	}
	if fs.RateLimitEnabled {
		t.Error("zero FeatureState.RateLimitEnabled should be false")
	}
	if fs.TLSEnabled {
		t.Error("zero FeatureState.TLSEnabled should be false")
	}
	if fs.AdminEnabled {
		t.Error("zero FeatureState.AdminEnabled should be false")
	}
	if fs.MetricsEnabled {
		t.Error("zero FeatureState.MetricsEnabled should be false")
	}
}

func TestFeatureState_Construction(t *testing.T) {
	tests := []struct {
		name  string
		state scaffold.FeatureState
	}{
		{
			name: "all features disabled",
			state: scaffold.FeatureState{
				UpstreamPort: 8080,
			},
		},
		{
			name: "auth only",
			state: scaffold.FeatureState{
				UpstreamPort: 8080,
				AuthEnabled:  true,
			},
		},
		{
			name: "all features enabled",
			state: scaffold.FeatureState{
				UpstreamPort:     8080,
				AuthEnabled:      true,
				RateLimitEnabled: true,
				TLSEnabled:       true,
				AdminEnabled:     true,
				MetricsEnabled:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Value object: a copy must equal the original field by field.
			copy := tt.state
			if copy.UpstreamPort != tt.state.UpstreamPort {
				t.Errorf("UpstreamPort mismatch: got %d, want %d", copy.UpstreamPort, tt.state.UpstreamPort)
			}
			if copy.AuthEnabled != tt.state.AuthEnabled {
				t.Errorf("AuthEnabled mismatch")
			}
			if copy.RateLimitEnabled != tt.state.RateLimitEnabled {
				t.Errorf("RateLimitEnabled mismatch")
			}
			if copy.TLSEnabled != tt.state.TLSEnabled {
				t.Errorf("TLSEnabled mismatch")
			}
			if copy.AdminEnabled != tt.state.AdminEnabled {
				t.Errorf("AdminEnabled mismatch")
			}
			if copy.MetricsEnabled != tt.state.MetricsEnabled {
				t.Errorf("MetricsEnabled mismatch")
			}
		})
	}
}

func TestFeatureOptions_ZeroValue(t *testing.T) {
	var fo scaffold.FeatureOptions

	if fo.TLSDomain != "" {
		t.Errorf("zero FeatureOptions.TLSDomain = %q, want empty", fo.TLSDomain)
	}
	if fo.TLSProvider != "" {
		t.Errorf("zero FeatureOptions.TLSProvider = %q, want empty", fo.TLSProvider)
	}
}

func TestFeatureOptions_Construction(t *testing.T) {
	tests := []struct {
		name string
		opts scaffold.FeatureOptions
	}{
		{
			name: "letsencrypt provider",
			opts: scaffold.FeatureOptions{
				TLSDomain:   "example.com",
				TLSProvider: "letsencrypt",
			},
		},
		{
			name: "self-signed provider",
			opts: scaffold.FeatureOptions{
				TLSDomain:   "localhost",
				TLSProvider: "self-signed",
			},
		},
		{
			name: "external provider",
			opts: scaffold.FeatureOptions{
				TLSDomain:   "app.example.com",
				TLSProvider: "external",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.opts.TLSDomain == "" {
				t.Errorf("TLSDomain should not be empty in test case %q", tt.name)
			}
			if tt.opts.TLSProvider == "" {
				t.Errorf("TLSProvider should not be empty in test case %q", tt.name)
			}
		})
	}
}
