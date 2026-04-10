// Package eject implements the "vibew eject" use case: reading vibewarden.yaml
// and producing the equivalent raw proxy configuration so that operators can
// graduate past VibeWarden and run Caddy (or another proxy) directly.
package eject

import (
	"fmt"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Format identifies the target proxy format for the generated configuration.
type Format string

const (
	// FormatCaddy produces Caddy's native JSON configuration.
	FormatCaddy Format = "caddy"
)

// ErrUnsupportedFormat is returned when the requested format is not supported.
type ErrUnsupportedFormat struct {
	Format Format
}

func (e ErrUnsupportedFormat) Error() string {
	return fmt.Sprintf("unsupported eject format %q; supported: caddy", e.Format)
}

// ConfigBuilder builds a proxy configuration map from a ProxyConfig.
// The caddy adapter implements this interface.
type ConfigBuilder interface {
	// Build returns a proxy-format-specific configuration map.
	Build(cfg *ports.ProxyConfig) (map[string]any, error)
}

// Service orchestrates the eject use case.
// It translates a vibewarden.yaml Config into a proxy-native configuration
// that can be used to run the proxy directly without VibeWarden.
type Service struct {
	builder ConfigBuilder
}

// NewService creates a new eject Service using the given ConfigBuilder.
func NewService(builder ConfigBuilder) *Service {
	return &Service{builder: builder}
}

// Eject translates the loaded VibeWarden config into a proxy-native
// configuration map. The returned map can be serialised to JSON and fed
// directly to the target proxy (e.g. Caddy's /load API or a config file).
//
// Note: internal-only addresses (metrics, admin, readiness) are omitted from
// the generated config because those internal HTTP servers are managed by
// VibeWarden itself and are not meaningful outside of it.
func (s *Service) Eject(cfg *config.Config) (map[string]any, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	proxyCfg := buildProxyConfig(cfg)
	result, err := s.builder.Build(proxyCfg)
	if err != nil {
		return nil, fmt.Errorf("building proxy config: %w", err)
	}
	return result, nil
}

// buildProxyConfig converts a VibeWarden config.Config into a ports.ProxyConfig
// suitable for the eject use case. Internal-service addresses (metrics, admin,
// readiness) are intentionally left empty because those services are managed by
// VibeWarden and not meaningful in a standalone proxy deployment.
func buildProxyConfig(cfg *config.Config) *ports.ProxyConfig {
	return &ports.ProxyConfig{
		ListenAddr:   fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		UpstreamAddr: fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port),
		Version:      "ejected",
		TLS: ports.TLSConfig{
			Enabled:     cfg.TLS.Enabled,
			Provider:    ports.TLSProvider(cfg.TLS.Provider),
			Domain:      cfg.TLS.Domain,
			CertPath:    cfg.TLS.CertPath,
			KeyPath:     cfg.TLS.KeyPath,
			StoragePath: cfg.TLS.StoragePath,
		},
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:                      cfg.SecurityHeaders.Enabled,
			HSTSMaxAge:                   cfg.SecurityHeaders.HSTSMaxAge,
			HSTSIncludeSubDomains:        cfg.SecurityHeaders.HSTSIncludeSubDomains,
			HSTSPreload:                  cfg.SecurityHeaders.HSTSPreload,
			ContentTypeNosniff:           cfg.SecurityHeaders.ContentTypeNosniff,
			FrameOption:                  cfg.SecurityHeaders.FrameOption,
			ContentSecurityPolicy:        cfg.SecurityHeaders.ContentSecurityPolicy,
			ReferrerPolicy:               cfg.SecurityHeaders.ReferrerPolicy,
			PermissionsPolicy:            cfg.SecurityHeaders.PermissionsPolicy,
			CrossOriginOpenerPolicy:      cfg.SecurityHeaders.CrossOriginOpenerPolicy,
			CrossOriginResourcePolicy:    cfg.SecurityHeaders.CrossOriginResourcePolicy,
			PermittedCrossDomainPolicies: cfg.SecurityHeaders.PermittedCrossDomainPolicies,
			SuppressViaHeader:            cfg.SecurityHeaders.SuppressViaHeader,
		},
		Auth: ports.AuthConfig{
			Enabled:             cfg.Auth.Enabled,
			KratosPublicURL:     cfg.Kratos.PublicURL,
			KratosAdminURL:      cfg.Kratos.AdminURL,
			PublicPaths:         cfg.Auth.PublicPaths,
			SessionCookieName:   cfg.Auth.SessionCookieName,
			LoginURL:            cfg.Auth.LoginURL,
			OnKratosUnavailable: ports.KratosUnavailableBehavior(cfg.Auth.OnKratosUnavailable),
		},
		RateLimit: ports.RateLimitConfig{
			Enabled:           cfg.RateLimit.Enabled,
			TrustProxyHeaders: cfg.RateLimit.TrustProxyHeaders,
			ExemptPaths:       cfg.RateLimit.ExemptPaths,
			PerIP: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerIP.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerIP.Burst,
			},
			PerUser: ports.RateLimitRule{
				RequestsPerSecond: cfg.RateLimit.PerUser.RequestsPerSecond,
				Burst:             cfg.RateLimit.PerUser.Burst,
			},
		},
		AdminAuth: ports.AdminAuthConfig{
			Enabled: cfg.Admin.Enabled,
			Token:   cfg.Admin.Token,
		},
		BodySize: buildBodySizeConfig(cfg),
		IPFilter: ports.IPFilterConfig{
			Enabled:           cfg.IPFilter.Enabled,
			Mode:              cfg.IPFilter.Mode,
			Addresses:         cfg.IPFilter.Addresses,
			TrustProxyHeaders: cfg.IPFilter.TrustProxyHeaders,
		},
		Resilience: buildResilienceConfig(cfg),
		Compression: ports.CompressionConfig{
			Enabled:    cfg.Compression.Enabled,
			Algorithms: cfg.Compression.Algorithms,
		},
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: len(cfg.ResponseHeaders.Set) > 0 ||
				len(cfg.ResponseHeaders.Add) > 0 ||
				len(cfg.ResponseHeaders.Remove) > 0,
			Set:    cfg.ResponseHeaders.Set,
			Add:    cfg.ResponseHeaders.Add,
			Remove: cfg.ResponseHeaders.Remove,
		},
		// Metrics, Admin (internal), and Readiness are intentionally omitted:
		// these internal HTTP servers are managed by VibeWarden and have no
		// equivalent in a standalone Caddy deployment.
	}
}

// buildBodySizeConfig converts the app config body size settings into a
// ports.BodySizeConfig, parsing human-readable size strings into bytes.
// Unparseable values are silently skipped (the config has already been validated).
func buildBodySizeConfig(cfg *config.Config) ports.BodySizeConfig {
	if cfg.BodySize.Max == "" {
		return ports.BodySizeConfig{}
	}

	maxBytes, err := config.ParseBodySize(cfg.BodySize.Max)
	if err != nil {
		return ports.BodySizeConfig{}
	}

	result := ports.BodySizeConfig{
		Enabled:  maxBytes > 0 || len(cfg.BodySize.Overrides) > 0,
		MaxBytes: maxBytes,
	}

	for _, ov := range cfg.BodySize.Overrides {
		ovBytes, ovErr := config.ParseBodySize(ov.Max)
		if ovErr != nil {
			continue
		}
		result.Overrides = append(result.Overrides, ports.BodySizeOverride{
			Path:     ov.Path,
			MaxBytes: ovBytes,
		})
	}

	return result
}

// buildResilienceConfig parses the resilience duration strings from the app
// config and returns a ports.ResilienceConfig. Unparseable values fall back
// to the same defaults used by the serve command.
func buildResilienceConfig(cfg *config.Config) ports.ResilienceConfig {
	result := ports.ResilienceConfig{}

	raw := cfg.Resilience.Timeout
	if raw != "" && raw != "0" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			result.Timeout = 30 * time.Second
		} else {
			result.Timeout = d
		}
	}

	cbCfg := cfg.Resilience.CircuitBreaker
	if cbCfg.Enabled {
		threshold := cbCfg.Threshold
		if threshold <= 0 {
			threshold = 5
		}

		cbTimeout := 60 * time.Second
		if cbCfg.Timeout != "" && cbCfg.Timeout != "0" {
			d, err := time.ParseDuration(cbCfg.Timeout)
			if err == nil {
				cbTimeout = d
			}
		}

		result.CircuitBreaker = ports.CircuitBreakerConfig{
			Enabled:   true,
			Threshold: threshold,
			Timeout:   cbTimeout,
		}
	}

	retryCfg := cfg.Resilience.Retry
	if retryCfg.Enabled {
		maxAttempts := retryCfg.MaxAttempts
		if maxAttempts < 2 {
			maxAttempts = 3
		}

		initialBackoff := 100 * time.Millisecond
		if retryCfg.InitialBackoff != "" && retryCfg.InitialBackoff != "0" {
			if d, err := time.ParseDuration(retryCfg.InitialBackoff); err == nil {
				initialBackoff = d
			}
		}

		maxBackoff := 10 * time.Second
		if retryCfg.MaxBackoff != "" && retryCfg.MaxBackoff != "0" {
			if d, err := time.ParseDuration(retryCfg.MaxBackoff); err == nil {
				maxBackoff = d
			}
		}

		retryOn := retryCfg.RetryOn
		if len(retryOn) == 0 {
			retryOn = []int{502, 503, 504}
		}

		result.Retry = ports.RetryConfig{
			Enabled:        true,
			MaxAttempts:    maxAttempts,
			InitialBackoff: initialBackoff,
			MaxBackoff:     maxBackoff,
			RetryOn:        retryOn,
		}
	}

	return result
}
