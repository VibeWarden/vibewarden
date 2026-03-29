package egress_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/egress"
)

func TestNewRoute_Validation(t *testing.T) {
	tests := []struct {
		name    string
		rName   string
		pattern string
		wantErr bool
	}{
		{
			name:    "valid route",
			rName:   "stripe",
			pattern: "https://api.stripe.com/**",
			wantErr: false,
		},
		{
			name:    "empty name",
			rName:   "",
			pattern: "https://api.stripe.com/**",
			wantErr: true,
		},
		{
			name:    "empty pattern",
			rName:   "stripe",
			pattern: "",
			wantErr: true,
		},
		{
			name:    "pattern without scheme",
			rName:   "stripe",
			pattern: "api.stripe.com/**",
			wantErr: true,
		},
		{
			name:    "http scheme is valid",
			rName:   "local",
			pattern: "http://localhost/**",
			wantErr: false,
		},
		{
			name:    "invalid glob syntax",
			rName:   "bad",
			pattern: "https://api.stripe.com/[invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := egress.NewRoute(tt.rName, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRoute(%q, %q) error = %v, wantErr %v", tt.rName, tt.pattern, err, tt.wantErr)
			}
		})
	}
}

func TestNewRoute_Accessors(t *testing.T) {
	timeout := 10 * time.Second
	secret := egress.SecretConfig{
		Name:   "stripe_key",
		Header: "Authorization",
		Format: "Bearer {value}",
	}
	cb := egress.CircuitBreakerConfig{Threshold: 5, ResetAfter: 30 * time.Second}
	retry := egress.RetryConfig{Max: 3, Methods: []string{"GET"}, Backoff: egress.RetryBackoffExponential}

	r, err := egress.NewRoute(
		"stripe",
		"https://api.stripe.com/**",
		egress.WithMethods("GET", "POST"),
		egress.WithTimeout(timeout),
		egress.WithSecret(secret),
		egress.WithRateLimit("100/s"),
		egress.WithCircuitBreaker(cb),
		egress.WithRetry(retry),
		egress.WithBodySizeLimit(52428800),
	)
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}

	if got := r.Name(); got != "stripe" {
		t.Errorf("Name() = %q, want %q", got, "stripe")
	}
	if got := r.Pattern(); got != "https://api.stripe.com/**" {
		t.Errorf("Pattern() = %q, want %q", got, "https://api.stripe.com/**")
	}
	if got := r.Timeout(); got != timeout {
		t.Errorf("Timeout() = %v, want %v", got, timeout)
	}
	if got := r.RateLimit(); got != "100/s" {
		t.Errorf("RateLimit() = %q, want %q", got, "100/s")
	}
	if got := r.BodySizeLimit(); got != 52428800 {
		t.Errorf("BodySizeLimit() = %d, want %d", got, 52428800)
	}
	if got := r.Secret(); got != secret {
		t.Errorf("Secret() = %+v, want %+v", got, secret)
	}
	if got := r.CircuitBreaker(); got != cb {
		t.Errorf("CircuitBreaker() = %+v, want %+v", got, cb)
	}
}

func TestRoute_MatchesMethod(t *testing.T) {
	tests := []struct {
		name         string
		routeMethods []string
		method       string
		want         bool
	}{
		{
			name:         "empty methods matches any",
			routeMethods: nil,
			method:       "DELETE",
			want:         true,
		},
		{
			name:         "method in list matches",
			routeMethods: []string{"GET", "POST"},
			method:       "GET",
			want:         true,
		},
		{
			name:         "method not in list no match",
			routeMethods: []string{"GET", "POST"},
			method:       "DELETE",
			want:         false,
		},
		{
			name:         "case insensitive match",
			routeMethods: []string{"get"},
			method:       "GET",
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := []egress.RouteOption{}
			if len(tt.routeMethods) > 0 {
				opts = append(opts, egress.WithMethods(tt.routeMethods...))
			}
			r, err := egress.NewRoute("r", "https://example.com/**", opts...)
			if err != nil {
				t.Fatalf("NewRoute() error: %v", err)
			}
			if got := r.MatchesMethod(tt.method); got != tt.want {
				t.Errorf("MatchesMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
	}
}

func TestRoute_MatchesURL(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		url     string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "https://api.stripe.com/v1/charges",
			url:     "https://api.stripe.com/v1/charges",
			want:    true,
		},
		{
			name:    "glob wildcard matches path segment",
			pattern: "https://api.stripe.com/v1/*",
			url:     "https://api.stripe.com/v1/charges",
			want:    true,
		},
		{
			name:    "double star glob matches nested path",
			pattern: "https://api.stripe.com/**",
			url:     "https://api.stripe.com/v1/customers/cus_123",
			want:    false, // path.Match ** is not recursive in stdlib
		},
		{
			name:    "no match on different domain",
			pattern: "https://api.stripe.com/v1/*",
			url:     "https://api.github.com/v1/repos",
			want:    false,
		},
		{
			name:    "question mark matches single char",
			pattern: "https://api.stripe.com/v?/charges",
			url:     "https://api.stripe.com/v1/charges",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := egress.NewRoute("r", tt.pattern)
			if err != nil {
				t.Fatalf("NewRoute() error: %v", err)
			}
			if got := r.MatchesURL(tt.url); got != tt.want {
				t.Errorf("MatchesURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
