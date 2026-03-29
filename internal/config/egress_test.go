package config_test

import (
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

// TestValidate_EgressDisabled verifies that no egress validation runs when egress
// is disabled, even if routes have invalid fields.
func TestValidate_EgressDisabled(t *testing.T) {
	cfg := config.Config{
		Egress: config.EgressConfig{
			Enabled: false,
			Routes: []config.EgressRouteConfig{
				{Name: "", Pattern: ""},
			},
		},
	}
	// Should not error because egress is disabled.
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() with egress disabled returned unexpected error: %v", err)
	}
}

// TestValidate_EgressDefaultPolicy verifies valid and invalid default_policy values.
func TestValidate_EgressDefaultPolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  string
		wantErr bool
	}{
		{"deny is valid", "deny", false},
		{"allow is valid", "allow", false},
		{"empty is valid (uses default)", "", false},
		{"unknown is invalid", "block", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled:       true,
					DefaultPolicy: tt.policy,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressDefaultTimeout verifies valid and invalid default_timeout values.
func TestValidate_EgressDefaultTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid 30s", "30s", false},
		{"valid 1m", "1m", false},
		{"empty is valid (uses default)", "", false},
		{"invalid string", "notaduration", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled:        true,
					DefaultTimeout: tt.timeout,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressRouteName verifies that route names are required and unique.
func TestValidate_EgressRouteName(t *testing.T) {
	tests := []struct {
		name    string
		routes  []config.EgressRouteConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "unique names are valid",
			routes: []config.EgressRouteConfig{
				{Name: "stripe", Pattern: "https://api.stripe.com/**"},
				{Name: "github", Pattern: "https://api.github.com/**"},
			},
			wantErr: false,
		},
		{
			name: "empty name is invalid",
			routes: []config.EgressRouteConfig{
				{Name: "", Pattern: "https://api.stripe.com/**"},
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "duplicate names are invalid",
			routes: []config.EgressRouteConfig{
				{Name: "stripe", Pattern: "https://api.stripe.com/**"},
				{Name: "stripe", Pattern: "https://api2.stripe.com/**"},
			},
			wantErr: true,
			errMsg:  "duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes:  tt.routes,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_EgressRoutePattern verifies that route patterns are required and valid.
func TestValidate_EgressRoutePattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"https pattern is valid", "https://api.stripe.com/**", false},
		{"http pattern is valid", "http://localhost/**", false},
		{"empty pattern is invalid", "", true},
		{"pattern without scheme is invalid", "api.stripe.com/**", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: tt.pattern},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() with pattern %q: error = %v, wantErr %v", tt.pattern, err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressRouteTimeout verifies per-route timeout validation.
func TestValidate_EgressRouteTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{"valid timeout", "10s", false},
		{"empty timeout is valid", "", false},
		{"invalid timeout", "bad", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: "https://api.example.com/**", Timeout: tt.timeout},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressCircuitBreaker verifies circuit breaker field validation.
func TestValidate_EgressCircuitBreaker(t *testing.T) {
	tests := []struct {
		name    string
		cb      config.EgressCircuitBreakerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "zero CB config is valid (not configured)",
			cb:      config.EgressCircuitBreakerConfig{},
			wantErr: false,
		},
		{
			name:    "valid CB config",
			cb:      config.EgressCircuitBreakerConfig{Threshold: 5, ResetAfter: "30s"},
			wantErr: false,
		},
		{
			name:    "threshold set without reset_after",
			cb:      config.EgressCircuitBreakerConfig{Threshold: 5, ResetAfter: ""},
			wantErr: true,
			errMsg:  "reset_after is required",
		},
		{
			name:    "threshold must be positive",
			cb:      config.EgressCircuitBreakerConfig{Threshold: -1, ResetAfter: "30s"},
			wantErr: true,
			errMsg:  "threshold must be > 0",
		},
		{
			name:    "invalid reset_after duration",
			cb:      config.EgressCircuitBreakerConfig{Threshold: 3, ResetAfter: "nope"},
			wantErr: true,
			errMsg:  "reset_after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: "https://api.example.com/**", CircuitBreaker: tt.cb},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_EgressRetries verifies retry field validation.
func TestValidate_EgressRetries(t *testing.T) {
	tests := []struct {
		name    string
		retries config.EgressRetryConfig
		wantErr bool
	}{
		{
			name:    "zero retries config is valid (not configured)",
			retries: config.EgressRetryConfig{},
			wantErr: false,
		},
		{
			name:    "valid retries config",
			retries: config.EgressRetryConfig{Max: 3, Backoff: "exponential"},
			wantErr: false,
		},
		{
			name:    "fixed backoff is valid",
			retries: config.EgressRetryConfig{Max: 2, Backoff: "fixed"},
			wantErr: false,
		},
		{
			name:    "empty backoff is valid",
			retries: config.EgressRetryConfig{Max: 3, Backoff: ""},
			wantErr: false,
		},
		{
			name:    "invalid backoff",
			retries: config.EgressRetryConfig{Max: 3, Backoff: "random"},
			wantErr: true,
		},
		{
			name:    "negative max is invalid",
			retries: config.EgressRetryConfig{Max: -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: "https://api.example.com/**", Retries: tt.retries},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressDNS verifies validation of egress.dns settings.
func TestValidate_EgressDNS(t *testing.T) {
	tests := []struct {
		name           string
		blockPrivate   bool
		allowedPrivate []string
		wantErr        bool
		errMsg         string
	}{
		{
			name:         "block_private true with no allowed_private is valid",
			blockPrivate: true,
			wantErr:      false,
		},
		{
			name:         "block_private false is valid",
			blockPrivate: false,
			wantErr:      false,
		},
		{
			name:           "valid allowed_private CIDRs",
			blockPrivate:   true,
			allowedPrivate: []string{"10.0.0.0/8", "192.168.1.0/24"},
			wantErr:        false,
		},
		{
			name:           "invalid CIDR in allowed_private",
			blockPrivate:   true,
			allowedPrivate: []string{"not-a-cidr"},
			wantErr:        true,
			errMsg:         "allowed_private",
		},
		{
			name:           "second entry is invalid CIDR",
			blockPrivate:   true,
			allowedPrivate: []string{"10.0.0.0/8", "bad"},
			wantErr:        true,
			errMsg:         "allowed_private[1]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					DNS: config.EgressDNSConfig{
						BlockPrivate:   tt.blockPrivate,
						AllowedPrivate: tt.allowedPrivate,
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errMsg)
				}
			}
		})
	}
}

// TestValidate_EgressBodySizeLimit verifies body size limit parsing.
func TestValidate_EgressBodySizeLimit(t *testing.T) {
	tests := []struct {
		name          string
		bodySizeLimit string
		wantErr       bool
	}{
		{"empty is valid", "", false},
		{"50MB is valid", "50MB", false},
		{"1GB is valid", "1GB", false},
		{"invalid unit", "50XB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: "https://api.example.com/**", BodySizeLimit: tt.bodySizeLimit},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressResponseSizeLimit verifies per-route response size limit parsing.
func TestValidate_EgressResponseSizeLimit(t *testing.T) {
	tests := []struct {
		name              string
		responseSizeLimit string
		wantErr           bool
	}{
		{"empty is valid", "", false},
		{"50MB is valid", "50MB", false},
		{"1GB is valid", "1GB", false},
		{"invalid unit", "50XB", true},
		{"no numeric value", "MB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{Name: "r", Pattern: "https://api.example.com/**", ResponseSizeLimit: tt.responseSizeLimit},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressDefaultBodySizeLimit verifies global default body size limit parsing.
func TestValidate_EgressDefaultBodySizeLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   string
		wantErr bool
	}{
		{"empty is valid (no limit)", "", false},
		{"10MB is valid", "10MB", false},
		{"invalid unit", "10XB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled:              true,
					DefaultBodySizeLimit: tt.limit,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressDefaultResponseSizeLimit verifies global default response size limit parsing.
func TestValidate_EgressDefaultResponseSizeLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   string
		wantErr bool
	}{
		{"empty is valid (no limit)", "", false},
		{"100MB is valid", "100MB", false},
		{"invalid unit", "100ZB", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled:                  true,
					DefaultResponseSizeLimit: tt.limit,
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestValidate_EgressSecretInjection verifies that partial secret config is rejected.
func TestValidate_EgressSecretInjection(t *testing.T) {
	tests := []struct {
		name         string
		secret       string
		secretHeader string
		secretFormat string
		wantErr      bool
	}{
		{
			name:    "no secret fields is valid",
			wantErr: false,
		},
		{
			name:         "all secret fields is valid",
			secret:       "stripe_key",
			secretHeader: "Authorization",
			secretFormat: "Bearer {value}",
			wantErr:      false,
		},
		{
			name:         "header set without secret name",
			secretHeader: "Authorization",
			wantErr:      true,
		},
		{
			name:         "format set without secret name",
			secretFormat: "Bearer {value}",
			wantErr:      true,
		},
		{
			name:         "header set without secret_header",
			secret:       "stripe_key",
			secretFormat: "Bearer {value}",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{
				Egress: config.EgressConfig{
					Enabled: true,
					Routes: []config.EgressRouteConfig{
						{
							Name:         "r",
							Pattern:      "https://api.example.com/**",
							Secret:       tt.secret,
							SecretHeader: tt.secretHeader,
							SecretFormat: tt.secretFormat,
						},
					},
				},
			}
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
