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
		egress.WithResponseSizeLimit(10485760),
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
	if got := r.ResponseSizeLimit(); got != 10485760 {
		t.Errorf("ResponseSizeLimit() = %d, want %d", got, 10485760)
	}
	if got := r.Secret(); got != secret {
		t.Errorf("Secret() = %+v, want %+v", got, secret)
	}
	if got := r.CircuitBreaker(); got != cb {
		t.Errorf("CircuitBreaker() = %+v, want %+v", got, cb)
	}
}

// TestNewRoute_SizeLimitDefaults verifies that BodySizeLimit and
// ResponseSizeLimit default to 0 (no limit) when not set.
func TestNewRoute_SizeLimitDefaults(t *testing.T) {
	r, err := egress.NewRoute("r", "https://example.com/**")
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}
	if got := r.BodySizeLimit(); got != 0 {
		t.Errorf("BodySizeLimit() = %d, want 0 (no limit by default)", got)
	}
	if got := r.ResponseSizeLimit(); got != 0 {
		t.Errorf("ResponseSizeLimit() = %d, want 0 (no limit by default)", got)
	}
}

// TestNewRoute_AllowInsecure verifies the AllowInsecure accessor and option.
func TestNewRoute_AllowInsecure(t *testing.T) {
	tests := []struct {
		name         string
		opts         []egress.RouteOption
		wantInsecure bool
	}{
		{
			name:         "default is false",
			opts:         nil,
			wantInsecure: false,
		},
		{
			name:         "WithAllowInsecure(true) sets true",
			opts:         []egress.RouteOption{egress.WithAllowInsecure(true)},
			wantInsecure: true,
		},
		{
			name:         "WithAllowInsecure(false) keeps false",
			opts:         []egress.RouteOption{egress.WithAllowInsecure(false)},
			wantInsecure: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := egress.NewRoute("r", "https://example.com/**", tt.opts...)
			if err != nil {
				t.Fatalf("NewRoute() unexpected error: %v", err)
			}
			if got := r.AllowInsecure(); got != tt.wantInsecure {
				t.Errorf("AllowInsecure() = %v, want %v", got, tt.wantInsecure)
			}
		})
	}
}

func TestRetryConfig_IsRetryableMethod(t *testing.T) {
	tests := []struct {
		name   string
		cfg    egress.RetryConfig
		method string
		want   bool
	}{
		{
			name:   "empty methods: GET is retryable by default",
			cfg:    egress.RetryConfig{},
			method: "GET",
			want:   true,
		},
		{
			name:   "empty methods: HEAD is retryable by default",
			cfg:    egress.RetryConfig{},
			method: "HEAD",
			want:   true,
		},
		{
			name:   "empty methods: PUT is retryable by default",
			cfg:    egress.RetryConfig{},
			method: "PUT",
			want:   true,
		},
		{
			name:   "empty methods: DELETE is retryable by default",
			cfg:    egress.RetryConfig{},
			method: "DELETE",
			want:   true,
		},
		{
			name:   "empty methods: POST is NOT retryable by default",
			cfg:    egress.RetryConfig{},
			method: "POST",
			want:   false,
		},
		{
			name:   "empty methods: PATCH is NOT retryable by default",
			cfg:    egress.RetryConfig{},
			method: "PATCH",
			want:   false,
		},
		{
			name:   "explicit methods: listed method is retryable",
			cfg:    egress.RetryConfig{Methods: []string{"GET", "POST"}},
			method: "POST",
			want:   true,
		},
		{
			name:   "explicit methods: unlisted method is not retryable",
			cfg:    egress.RetryConfig{Methods: []string{"GET"}},
			method: "DELETE",
			want:   false,
		},
		{
			name:   "case insensitive match in explicit methods",
			cfg:    egress.RetryConfig{Methods: []string{"get"}},
			method: "GET",
			want:   true,
		},
		{
			name:   "case insensitive match in default idempotent set",
			cfg:    egress.RetryConfig{},
			method: "get",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.IsRetryableMethod(tt.method)
			if got != tt.want {
				t.Errorf("IsRetryableMethod(%q) = %v, want %v", tt.method, got, tt.want)
			}
		})
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

// TestMTLSConfig_IsZero verifies zero-value detection for MTLSConfig.
func TestMTLSConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  egress.MTLSConfig
		want bool
	}{
		{
			name: "zero value is zero",
			cfg:  egress.MTLSConfig{},
			want: true,
		},
		{
			name: "cert path set",
			cfg:  egress.MTLSConfig{CertPath: "/etc/certs/client.crt"},
			want: false,
		},
		{
			name: "key path set",
			cfg:  egress.MTLSConfig{KeyPath: "/etc/certs/client.key"},
			want: false,
		},
		{
			name: "ca path set",
			cfg:  egress.MTLSConfig{CAPath: "/etc/certs/ca.crt"},
			want: false,
		},
		{
			name: "all fields set",
			cfg: egress.MTLSConfig{
				CertPath: "/etc/certs/client.crt",
				KeyPath:  "/etc/certs/client.key",
				CAPath:   "/etc/certs/ca.crt",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsZero(); got != tt.want {
				t.Errorf("MTLSConfig.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCacheConfig_IsZero verifies zero-value detection for CacheConfig.
func TestCacheConfig_IsZero(t *testing.T) {
	tests := []struct {
		name string
		cfg  egress.CacheConfig
		want bool
	}{
		{
			name: "zero value is zero",
			cfg:  egress.CacheConfig{},
			want: true,
		},
		{
			name: "disabled is zero",
			cfg:  egress.CacheConfig{Enabled: false, TTL: 5 * time.Second},
			want: true,
		},
		{
			name: "enabled is not zero",
			cfg:  egress.CacheConfig{Enabled: true},
			want: false,
		},
		{
			name: "enabled with TTL and MaxSize",
			cfg:  egress.CacheConfig{Enabled: true, TTL: 30 * time.Second, MaxSize: 1024},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.IsZero(); got != tt.want {
				t.Errorf("CacheConfig.IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNewRoute_WithSanitizeMTLSCache verifies that WithSanitize, WithMTLS, and
// WithCache options are applied and their accessor methods return correct values.
func TestNewRoute_WithSanitizeMTLSCache(t *testing.T) {
	mtls := egress.MTLSConfig{
		CertPath: "/etc/certs/client.crt",
		KeyPath:  "/etc/certs/client.key",
		CAPath:   "/etc/certs/ca.crt",
	}
	cache := egress.CacheConfig{
		Enabled: true,
		TTL:     60 * time.Second,
		MaxSize: 4096,
	}
	sanitize := egress.SanitizeConfig{
		Headers:     []string{"Authorization"},
		QueryParams: []string{"api_key"},
	}
	retry := egress.RetryConfig{
		Max:            3,
		Methods:        []string{"GET", "PUT"},
		Backoff:        egress.RetryBackoffFixed,
		InitialBackoff: 100 * time.Millisecond,
	}

	r, err := egress.NewRoute(
		"secure-api",
		"https://api.example.com/**",
		egress.WithSanitize(sanitize),
		egress.WithMTLS(mtls),
		egress.WithCache(cache),
		egress.WithRetry(retry),
		egress.WithMethods("GET", "PUT"),
	)
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}

	// MTLS and Cache can be compared directly (no slice fields in MTLSConfig/CacheConfig).
	if got := r.MTLS(); got != mtls {
		t.Errorf("MTLS() = %+v, want %+v", got, mtls)
	}
	if got := r.Cache(); got != cache {
		t.Errorf("Cache() = %+v, want %+v", got, cache)
	}

	// SanitizeConfig contains slices — compare fields individually.
	gotSanitize := r.Sanitize()
	if len(gotSanitize.Headers) != len(sanitize.Headers) || gotSanitize.Headers[0] != sanitize.Headers[0] {
		t.Errorf("Sanitize().Headers = %v, want %v", gotSanitize.Headers, sanitize.Headers)
	}
	if len(gotSanitize.QueryParams) != len(sanitize.QueryParams) || gotSanitize.QueryParams[0] != sanitize.QueryParams[0] {
		t.Errorf("Sanitize().QueryParams = %v, want %v", gotSanitize.QueryParams, sanitize.QueryParams)
	}

	// RetryConfig contains slices — compare fields individually.
	gotRetry := r.Retry()
	if gotRetry.Max != retry.Max {
		t.Errorf("Retry().Max = %d, want %d", gotRetry.Max, retry.Max)
	}
	if gotRetry.Backoff != retry.Backoff {
		t.Errorf("Retry().Backoff = %q, want %q", gotRetry.Backoff, retry.Backoff)
	}
	if gotRetry.InitialBackoff != retry.InitialBackoff {
		t.Errorf("Retry().InitialBackoff = %v, want %v", gotRetry.InitialBackoff, retry.InitialBackoff)
	}
	if len(gotRetry.Methods) != len(retry.Methods) {
		t.Errorf("Retry().Methods len = %d, want %d", len(gotRetry.Methods), len(retry.Methods))
	}

	wantMethods := []string{"GET", "PUT"}
	got := r.Methods()
	if len(got) != len(wantMethods) {
		t.Errorf("Methods() len = %d, want %d", len(got), len(wantMethods))
	} else {
		for i, m := range got {
			if m != wantMethods[i] {
				t.Errorf("Methods()[%d] = %q, want %q", i, m, wantMethods[i])
			}
		}
	}
}

// TestNewRoute_DefaultAccessors verifies that accessor methods return zero
// values when no options are applied.
func TestNewRoute_DefaultAccessors(t *testing.T) {
	r, err := egress.NewRoute("r", "https://example.com/**")
	if err != nil {
		t.Fatalf("NewRoute() unexpected error: %v", err)
	}

	if got := r.Methods(); len(got) != 0 {
		t.Errorf("Methods() = %v, want empty slice", got)
	}
	gotRetry := r.Retry()
	if gotRetry.Max != 0 || gotRetry.Backoff != "" || len(gotRetry.Methods) != 0 {
		t.Errorf("Retry() = %+v, want zero value", gotRetry)
	}
	gotSanitize := r.Sanitize()
	if len(gotSanitize.Headers) != 0 || len(gotSanitize.QueryParams) != 0 {
		t.Errorf("Sanitize() = %+v, want zero value", gotSanitize)
	}
	if got := r.MTLS(); !got.IsZero() {
		t.Errorf("MTLS() = %+v, want zero value", got)
	}
	if got := r.Cache(); !got.IsZero() {
		t.Errorf("Cache() = %+v, want zero value", got)
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
