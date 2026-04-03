package serve

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/csp"
	"github.com/vibewarden/vibewarden/internal/plugins"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	tlsplugin "github.com/vibewarden/vibewarden/internal/plugins/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// buildProxyConfig constructs the ports.ProxyConfig that the Caddy adapter
// uses to build its JSON configuration. Plugin-specific fields are read from
// the running plugins where possible (e.g. metrics internal address after Start).
//
// version is the binary version string injected at build time.
func buildProxyConfig(cfg *config.Config, registry *plugins.Registry, version string) *ports.ProxyConfig {
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

	// Collect routes and handlers contributed by CaddyContributor plugins.
	// Both slices are sorted by ascending Priority before being stored so that
	// BuildCaddyConfig can insert them in a deterministic order.
	var extraRoutes []ports.CaddyRoute
	var extraHandlers []ports.CaddyHandler

	for _, contrib := range registry.CaddyContributors() {
		extraRoutes = append(extraRoutes, contrib.ContributeCaddyRoutes()...)
		extraHandlers = append(extraHandlers, contrib.ContributeCaddyHandlers()...)
	}

	sort.Slice(extraRoutes, func(i, j int) bool {
		return extraRoutes[i].Priority < extraRoutes[j].Priority
	})
	sort.Slice(extraHandlers, func(i, j int) bool {
		return extraHandlers[i].Priority < extraHandlers[j].Priority
	})

	return &ports.ProxyConfig{
		ListenAddr:     fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		UpstreamAddr:   fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port),
		Version:        version,
		ServerTimeouts: buildServerTimeoutsConfig(cfg),
		TLS: ports.TLSConfig{
			Enabled:     cfg.TLS.Enabled,
			Provider:    ports.TLSProvider(cfg.TLS.Provider),
			Domain:      cfg.TLS.Domain,
			CertPath:    cfg.TLS.CertPath,
			KeyPath:     cfg.TLS.KeyPath,
			StoragePath: cfg.TLS.StoragePath,
		},
		SecurityHeaders: ports.SecurityHeadersConfig{
			Enabled:               cfg.SecurityHeaders.Enabled,
			HSTSMaxAge:            cfg.SecurityHeaders.HSTSMaxAge,
			HSTSIncludeSubDomains: cfg.SecurityHeaders.HSTSIncludeSubDomains,
			HSTSPreload:           cfg.SecurityHeaders.HSTSPreload,
			ContentTypeNosniff:    cfg.SecurityHeaders.ContentTypeNosniff,
			FrameOption:           cfg.SecurityHeaders.FrameOption,
			// Resolve CSP: raw string takes precedence; fall back to structured builder.
			ContentSecurityPolicy:        resolveCSP(cfg),
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
		ResponseHeaders: ports.ResponseHeadersConfig{
			Enabled: len(cfg.ResponseHeaders.Set) > 0 ||
				len(cfg.ResponseHeaders.Add) > 0 ||
				len(cfg.ResponseHeaders.Remove) > 0,
			Set:    cfg.ResponseHeaders.Set,
			Add:    cfg.ResponseHeaders.Add,
			Remove: cfg.ResponseHeaders.Remove,
		},
		ExtraRoutes:   extraRoutes,
		ExtraHandlers: extraHandlers,
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

// buildServerTimeoutsConfig parses the server-level timeout duration strings and
// returns a ports.ServerTimeoutsConfig. Unparseable values fall back to the
// documented defaults (read: 30s, write: 60s, idle: 120s). A value of "0" or ""
// disables that particular timeout (no limit).
func buildServerTimeoutsConfig(cfg *config.Config) ports.ServerTimeoutsConfig {
	result := ports.ServerTimeoutsConfig{}

	parseTimeout := func(raw, name, defaultVal string) time.Duration {
		if raw == "0" || raw == "" {
			// Caller explicitly disabled the timeout.
			return 0
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			slog.Default().Warn(fmt.Sprintf("server.%s parse error — using default %s", name, defaultVal),
				slog.String("error", err.Error()),
				slog.String("value", raw),
			)
			d, _ = time.ParseDuration(defaultVal)
		}
		return d
	}

	// Apply defaults when the field is empty (not explicitly set to "0").
	readRaw := cfg.Server.ReadTimeout
	if readRaw == "" {
		readRaw = "30s"
	}
	result.ReadTimeout = parseTimeout(readRaw, "read_timeout", "30s")

	writeRaw := cfg.Server.WriteTimeout
	if writeRaw == "" {
		writeRaw = "60s"
	}
	result.WriteTimeout = parseTimeout(writeRaw, "write_timeout", "60s")

	idleRaw := cfg.Server.IdleTimeout
	if idleRaw == "" {
		idleRaw = "120s"
	}
	result.IdleTimeout = parseTimeout(idleRaw, "idle_timeout", "120s")

	return result
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

// wireTLSMetricsCollector injects the MetricsCollector from the metrics plugin
// into the TLS plugin's certificate expiry monitor. It must be called after
// InitAll (so the metrics provider is ready) and before StartAll (so the
// monitor goroutine receives the collector before it first runs).
func wireTLSMetricsCollector(registry *plugins.Registry) {
	var collector ports.MetricsCollector
	var tlsPlugin *tlsplugin.Plugin

	for _, p := range registry.Plugins() {
		switch v := p.(type) {
		case *metricsplugin.Plugin:
			collector = v.Collector()
		case *tlsplugin.Plugin:
			tlsPlugin = v
		}
	}

	if tlsPlugin != nil && collector != nil {
		tlsPlugin.SetMetricsCollector(collector)
	}
}

// resolveCSP returns the Content-Security-Policy header value to use.
// The raw content_security_policy string takes precedence for backward
// compatibility. When it is empty, the structured csp block is passed to
// csp.Build and the generated string is returned instead.
func resolveCSP(cfg *config.Config) string {
	if cfg.SecurityHeaders.ContentSecurityPolicy != "" {
		return cfg.SecurityHeaders.ContentSecurityPolicy
	}
	return csp.Build(csp.Config{
		DefaultSrc:     cfg.SecurityHeaders.CSP.DefaultSrc,
		ScriptSrc:      cfg.SecurityHeaders.CSP.ScriptSrc,
		StyleSrc:       cfg.SecurityHeaders.CSP.StyleSrc,
		ImgSrc:         cfg.SecurityHeaders.CSP.ImgSrc,
		ConnectSrc:     cfg.SecurityHeaders.CSP.ConnectSrc,
		FontSrc:        cfg.SecurityHeaders.CSP.FontSrc,
		FrameSrc:       cfg.SecurityHeaders.CSP.FrameSrc,
		MediaSrc:       cfg.SecurityHeaders.CSP.MediaSrc,
		ObjectSrc:      cfg.SecurityHeaders.CSP.ObjectSrc,
		ManifestSrc:    cfg.SecurityHeaders.CSP.ManifestSrc,
		WorkerSrc:      cfg.SecurityHeaders.CSP.WorkerSrc,
		ChildSrc:       cfg.SecurityHeaders.CSP.ChildSrc,
		FormAction:     cfg.SecurityHeaders.CSP.FormAction,
		FrameAncestors: cfg.SecurityHeaders.CSP.FrameAncestors,
		BaseURI:        cfg.SecurityHeaders.CSP.BaseURI,
	})
}
