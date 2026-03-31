package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	egressplugin "github.com/vibewarden/vibewarden/internal/plugins/egress"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// buildProxyConfig constructs the ports.ProxyConfig that the Caddy adapter
// uses to build its JSON configuration. Plugin-specific fields are read from
// the running plugins where possible (e.g. metrics internal address after
// Start).
func buildProxyConfig(cfg *config.Config, registry *plugins.Registry) *ports.ProxyConfig {
	// Collect internal addresses from started InternalServerPlugin instances.
	var metricsCfg ports.MetricsProxyConfig
	var adminCfg ports.AdminProxyConfig

	for _, p := range registry.Plugins() {
		if isp, ok := p.(ports.InternalServerPlugin); ok {
			switch p.Name() {
			case "metrics":
				metricsCfg = ports.MetricsProxyConfig{
					Enabled:      cfg.Telemetry.Enabled && cfg.Telemetry.Prometheus.Enabled,
					InternalAddr: isp.InternalAddr(),
				}
			case "user-management":
				adminCfg = ports.AdminProxyConfig{
					Enabled:      cfg.Admin.Enabled,
					InternalAddr: isp.InternalAddr(),
				}
			}
		}
	}

	return &ports.ProxyConfig{
		ListenAddr:   fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		UpstreamAddr: fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port),
		Version:      version,
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
		Metrics: metricsCfg,
		AdminAuth: ports.AdminAuthConfig{
			Enabled: cfg.Admin.Enabled,
			Token:   cfg.Admin.Token,
		},
		Admin:    adminCfg,
		BodySize: buildBodySizePortsConfig(cfg),
		IPFilter: ports.IPFilterConfig{
			Enabled:           cfg.IPFilter.Enabled,
			Mode:              cfg.IPFilter.Mode,
			Addresses:         cfg.IPFilter.Addresses,
			TrustProxyHeaders: cfg.IPFilter.TrustProxyHeaders,
		},
		Resilience: buildResiliencePortsConfig(cfg),
		Compression: ports.CompressionConfig{
			Enabled:    cfg.Compression.Enabled,
			Algorithms: cfg.Compression.Algorithms,
		},
	}
}

// buildBodySizePortsConfig constructs a ports.BodySizeConfig from the app config,
// parsing human-readable size strings into bytes. Unparseable overrides are skipped.
func buildBodySizePortsConfig(cfg *config.Config) ports.BodySizeConfig {
	if cfg.BodySize.Max == "" {
		return ports.BodySizeConfig{}
	}

	maxBytes, err := config.ParseBodySize(cfg.BodySize.Max)
	if err != nil {
		// Already validated in config.Validate(). Defensive fallback: no limit.
		return ports.BodySizeConfig{}
	}

	bodySizeCfg := ports.BodySizeConfig{
		Enabled:  maxBytes > 0 || len(cfg.BodySize.Overrides) > 0,
		MaxBytes: maxBytes,
	}

	for _, ov := range cfg.BodySize.Overrides {
		ovBytes, ovErr := config.ParseBodySize(ov.Max)
		if ovErr != nil {
			continue
		}
		bodySizeCfg.Overrides = append(bodySizeCfg.Overrides, ports.BodySizeOverride{
			Path:     ov.Path,
			MaxBytes: ovBytes,
		})
	}

	return bodySizeCfg
}

// buildResiliencePortsConfig parses the resilience duration strings and returns
// a ports.ResilienceConfig. Unparseable timeout values are replaced with the
// 30-second default. Unparseable circuit breaker timeout values fall back to 60s.
// Unparseable retry backoff values fall back to their defaults.
func buildResiliencePortsConfig(cfg *config.Config) ports.ResilienceConfig {
	result := ports.ResilienceConfig{}

	// Parse request timeout.
	raw := cfg.Resilience.Timeout
	if raw != "" && raw != "0" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			slog.Default().Warn("resilience.timeout parse error — using default 30s",
				slog.String("error", err.Error()),
				slog.String("value", raw),
			)
			result.Timeout = 30 * time.Second
		} else {
			result.Timeout = d
		}
	}

	// Parse circuit breaker config.
	cbCfg := cfg.Resilience.CircuitBreaker
	if cbCfg.Enabled {
		threshold := cbCfg.Threshold
		if threshold <= 0 {
			threshold = 5
		}

		cbTimeout := 60 * time.Second
		if cbCfg.Timeout != "" && cbCfg.Timeout != "0" {
			d, err := time.ParseDuration(cbCfg.Timeout)
			if err != nil {
				slog.Default().Warn("resilience.circuit_breaker.timeout parse error — using default 60s",
					slog.String("error", err.Error()),
					slog.String("value", cbCfg.Timeout),
				)
			} else {
				cbTimeout = d
			}
		}

		result.CircuitBreaker = ports.CircuitBreakerConfig{
			Enabled:   true,
			Threshold: threshold,
			Timeout:   cbTimeout,
		}
	}

	// Parse retry config.
	retryCfg := cfg.Resilience.Retry
	if retryCfg.Enabled {
		maxAttempts := retryCfg.MaxAttempts
		if maxAttempts < 2 {
			maxAttempts = 3
		}

		initialBackoff := 100 * time.Millisecond
		if retryCfg.InitialBackoff != "" && retryCfg.InitialBackoff != "0" {
			d, err := time.ParseDuration(retryCfg.InitialBackoff)
			if err != nil {
				slog.Default().Warn("resilience.retry.backoff parse error — using default 100ms",
					slog.String("error", err.Error()),
					slog.String("value", retryCfg.InitialBackoff),
				)
			} else {
				initialBackoff = d
			}
		}

		maxBackoff := 10 * time.Second
		if retryCfg.MaxBackoff != "" && retryCfg.MaxBackoff != "0" {
			d, err := time.ParseDuration(retryCfg.MaxBackoff)
			if err != nil {
				slog.Default().Warn("resilience.retry.max_backoff parse error — using default 10s",
					slog.String("error", err.Error()),
					slog.String("value", retryCfg.MaxBackoff),
				)
			} else {
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

// buildEgressPlugin constructs the egress proxy plugin from config, parsing
// duration strings into time.Duration values. Falls back to plugin defaults
// on parse errors (config.Validate does not validate egress duration strings
// since they are optional and have sensible defaults).
func buildEgressPlugin(cfg *config.Config, eventLogger ports.EventLogger, logger *slog.Logger) *egressplugin.Plugin {
	ec := cfg.Egress

	defaultTimeout, err := time.ParseDuration(ec.DefaultTimeout)
	if err != nil && ec.DefaultTimeout != "" {
		logger.Warn("egress.default_timeout parse error — using default 30s",
			slog.String("error", err.Error()),
			slog.String("value", ec.DefaultTimeout),
		)
	}

	defaultBodySizeBytes, err := config.ParseBodySize(ec.DefaultBodySizeLimit)
	if err != nil && ec.DefaultBodySizeLimit != "" {
		logger.Warn("egress.default_body_size_limit parse error — disabling global body size limit",
			slog.String("error", err.Error()),
		)
		defaultBodySizeBytes = 0
	}

	defaultResponseSizeBytes, err := config.ParseBodySize(ec.DefaultResponseSizeLimit)
	if err != nil && ec.DefaultResponseSizeLimit != "" {
		logger.Warn("egress.default_response_size_limit parse error — disabling global response size limit",
			slog.String("error", err.Error()),
		)
		defaultResponseSizeBytes = 0
	}

	pluginCfg := egressplugin.Config{
		Enabled:                  ec.Enabled,
		Listen:                   ec.Listen,
		DefaultPolicy:            ec.DefaultPolicy,
		AllowInsecure:            ec.AllowInsecure,
		DefaultTimeout:           defaultTimeout,
		DefaultBodySizeLimit:     defaultBodySizeBytes,
		DefaultResponseSizeLimit: defaultResponseSizeBytes,
		BlockPrivate:             ec.DNS.BlockPrivate,
		AllowedPrivate:           ec.DNS.AllowedPrivate,
	}

	// Map route configs.
	for _, rc := range ec.Routes {
		routeTimeout, routeErr := time.ParseDuration(rc.Timeout)
		if routeErr != nil && rc.Timeout != "" {
			logger.Warn("egress route timeout parse error — using global default",
				slog.String("route", rc.Name),
				slog.String("error", routeErr.Error()),
			)
		}

		cbResetAfter, cbErr := time.ParseDuration(rc.CircuitBreaker.ResetAfter)
		if cbErr != nil && rc.CircuitBreaker.ResetAfter != "" {
			logger.Warn("egress route circuit_breaker.reset_after parse error — disabling circuit breaker for route",
				slog.String("route", rc.Name),
				slog.String("error", cbErr.Error()),
			)
		}

		bodySizeBytes, _ := config.ParseBodySize(rc.BodySizeLimit)
		responseSizeBytes, _ := config.ParseBodySize(rc.ResponseSizeLimit)

		pluginCfg.Routes = append(pluginCfg.Routes, egressplugin.RouteConfig{
			Name:              rc.Name,
			Pattern:           rc.Pattern,
			Methods:           rc.Methods,
			Timeout:           routeTimeout,
			Secret:            rc.Secret,
			SecretHeader:      rc.SecretHeader,
			SecretFormat:      rc.SecretFormat,
			RateLimit:         rc.RateLimit,
			BodySizeLimit:     bodySizeBytes,
			ResponseSizeLimit: responseSizeBytes,
			AllowInsecure:     rc.AllowInsecure,
			CircuitBreaker: egressplugin.CircuitBreakerConfig{
				Threshold:  rc.CircuitBreaker.Threshold,
				ResetAfter: cbResetAfter,
			},
			Retries: egressplugin.RetryConfig{
				Max:     rc.Retries.Max,
				Methods: rc.Retries.Methods,
				Backoff: rc.Retries.Backoff,
			},
			ValidateResponse: egressplugin.ResponseValidationConfig{
				StatusCodes:  rc.ValidateResponse.StatusCodes,
				ContentTypes: rc.ValidateResponse.ContentTypes,
			},
		})
	}

	return egressplugin.New(pluginCfg, eventLogger, logger)
}

// buildEventLogger constructs the event logger used by the caddy adapter and
// other consumers. When the metrics plugin has an OTel log handler available,
// the logger fans out to both stdout JSON and OTel via a MultiHandler.
// Falls back to stdout-only when log export is disabled or unavailable.
func buildEventLogger(registry *plugins.Registry, logger *slog.Logger) ports.EventLogger {
	for _, p := range registry.Plugins() {
		if mp, ok := p.(*metricsplugin.Plugin); ok {
			if h := mp.LogHandler(); h != nil {
				logger.Info("event logger: OTel log export enabled")
				return logadapter.NewSlogEventLogger(os.Stdout, h)
			}
			break
		}
	}
	return logadapter.NewSlogEventLogger(os.Stdout)
}
