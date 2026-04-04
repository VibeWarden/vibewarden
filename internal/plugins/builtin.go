package plugins

import (
	"log/slog"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/config"
	authplugin "github.com/vibewarden/vibewarden/internal/plugins/auth"
	bodysizeplugin "github.com/vibewarden/vibewarden/internal/plugins/bodysize"
	corsplugin "github.com/vibewarden/vibewarden/internal/plugins/cors"
	ipfilterplugin "github.com/vibewarden/vibewarden/internal/plugins/ipfilter"
	maintenanceplugin "github.com/vibewarden/vibewarden/internal/plugins/maintenance"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	ratelimitplugin "github.com/vibewarden/vibewarden/internal/plugins/ratelimit"
	sechdrs "github.com/vibewarden/vibewarden/internal/plugins/securityheaders"
	usermgmtplugin "github.com/vibewarden/vibewarden/internal/plugins/usermgmt"
	wafplugin "github.com/vibewarden/vibewarden/internal/plugins/waf"
	webhooksigplugin "github.com/vibewarden/vibewarden/internal/plugins/webhooksig"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// RegisterBuiltinPlugins registers all OSS compiled-in plugins with the registry
// based on the provided configuration. Plugin registration order matches plugin
// priority (low number → runs first in the request chain).
//
// This function is called automatically by internal/app/serve.RunServe and should
// not be called directly unless composing a custom startup sequence.
func RegisterBuiltinPlugins(
	registry *Registry,
	cfg *config.Config,
	eventLogger ports.EventLogger,
	logger *slog.Logger,
) {
	// Maintenance mode — priority 5 (must run before all other middleware so
	// that the sidecar is immediately quiesced during maintenance windows).
	registry.Register(maintenanceplugin.New(maintenanceplugin.Config{
		Enabled: cfg.Maintenance.Enabled,
		Message: cfg.Maintenance.Message,
	}, logger))

	// IP filter — priority 15 (must run before all other middleware)
	registry.Register(ipfilterplugin.New(ipfilterplugin.Config{
		Enabled:           cfg.IPFilter.Enabled,
		Mode:              ipfilterplugin.FilterMode(cfg.IPFilter.Mode),
		Addresses:         cfg.IPFilter.Addresses,
		TrustProxyHeaders: cfg.IPFilter.TrustProxyHeaders,
	}, logger))

	// TLS — priority 10
	registry.Register(buildTLSPlugin(cfg, eventLogger, logger))

	// CORS — priority 10 (before all middleware; OPTIONS preflight must be handled first)
	registry.Register(corsplugin.New(corsplugin.Config{
		Enabled:          cfg.CORS.Enabled,
		AllowedOrigins:   cfg.CORS.AllowedOrigins,
		AllowedMethods:   cfg.CORS.AllowedMethods,
		AllowedHeaders:   cfg.CORS.AllowedHeaders,
		ExposedHeaders:   cfg.CORS.ExposedHeaders,
		AllowCredentials: cfg.CORS.AllowCredentials,
		MaxAge:           cfg.CORS.MaxAge,
	}, logger))

	// Input validation — priority 18 (before WAF at 25; catches oversized inputs
	// before regex scanning begins).
	registry.Register(buildInputValidationPlugin(cfg, logger))

	// WAF — priority 25 (after security headers at 20, before admin auth at 30)
	registry.Register(wafplugin.New(wafplugin.Config{
		ContentTypeValidation: wafplugin.ContentTypeValidationConfig{
			Enabled: cfg.WAF.ContentTypeValidation.Enabled,
			Allowed: cfg.WAF.ContentTypeValidation.Allowed,
		},
		Engine: wafplugin.WAFEngineConfig{
			Enabled: cfg.WAF.Enabled,
			Mode:    wafplugin.Mode(cfg.WAF.Mode),
			Rules: wafplugin.RulesConfig{
				SQLInjection:     cfg.WAF.Rules.SQLInjection,
				XSS:              cfg.WAF.Rules.XSS,
				PathTraversal:    cfg.WAF.Rules.PathTraversal,
				CommandInjection: cfg.WAF.Rules.CommandInjection,
			},
			ExemptPaths: cfg.WAF.ExemptPaths,
		},
	}, logger))

	// Security headers — priority 20
	//
	// resolveCSP picks the raw content_security_policy string when set
	// (backward compat) and falls back to the structured csp builder otherwise.
	registry.Register(sechdrs.New(sechdrs.Config{
		Enabled:                      cfg.SecurityHeaders.Enabled,
		HSTSMaxAge:                   cfg.SecurityHeaders.HSTSMaxAge,
		HSTSIncludeSubDomains:        cfg.SecurityHeaders.HSTSIncludeSubDomains,
		HSTSPreload:                  cfg.SecurityHeaders.HSTSPreload,
		ContentTypeNosniff:           cfg.SecurityHeaders.ContentTypeNosniff,
		FrameOption:                  cfg.SecurityHeaders.FrameOption,
		ContentSecurityPolicy:        resolveCSP(cfg),
		ReferrerPolicy:               cfg.SecurityHeaders.ReferrerPolicy,
		PermissionsPolicy:            cfg.SecurityHeaders.PermissionsPolicy,
		CrossOriginOpenerPolicy:      cfg.SecurityHeaders.CrossOriginOpenerPolicy,
		CrossOriginResourcePolicy:    cfg.SecurityHeaders.CrossOriginResourcePolicy,
		PermittedCrossDomainPolicies: cfg.SecurityHeaders.PermittedCrossDomainPolicies,
		SuppressViaHeader:            cfg.SecurityHeaders.SuppressViaHeader,
	}, cfg.TLS.Enabled, logger))

	// Body size limiting — priority 45
	// Parse the configured size strings into bytes. Errors are already validated
	// by config.Validate(), so we log-and-skip on any unexpected parse failure
	// rather than failing startup.
	globalMaxBytes, err := config.ParseBodySize(cfg.BodySize.Max)
	if err != nil {
		logger.Error("body_size.max parse error — body size limiting disabled",
			slog.String("error", err.Error()),
		)
	} else {
		bodySizeCfg := bodysizeplugin.Config{
			Enabled:  globalMaxBytes > 0 || len(cfg.BodySize.Overrides) > 0,
			MaxBytes: globalMaxBytes,
		}
		for _, ov := range cfg.BodySize.Overrides {
			ovBytes, ovErr := config.ParseBodySize(ov.Max)
			if ovErr != nil {
				logger.Error("body_size override parse error — skipping override",
					slog.String("path", ov.Path),
					slog.String("error", ovErr.Error()),
				)
				continue
			}
			bodySizeCfg.Overrides = append(bodySizeCfg.Overrides, bodysizeplugin.OverrideConfig{
				Path:     ov.Path,
				MaxBytes: ovBytes,
			})
		}
		registry.Register(bodysizeplugin.New(bodySizeCfg, logger))
	}

	// Metrics — priority 30
	registry.Register(metricsplugin.New(metricsplugin.Config{
		Enabled:           cfg.Telemetry.Enabled,
		PathPatterns:      cfg.Telemetry.PathPatterns,
		PrometheusEnabled: cfg.Telemetry.Prometheus.Enabled,
		OTLPEnabled:       cfg.Telemetry.OTLP.Enabled,
		OTLPEndpoint:      cfg.Telemetry.OTLP.Endpoint,
		OTLPHeaders:       cfg.Telemetry.OTLP.Headers,
		OTLPInterval:      cfg.Telemetry.OTLP.Interval,
		OTLPProtocol:      cfg.Telemetry.OTLP.Protocol,
		LogsOTLPEnabled:   cfg.Telemetry.Logs.OTLP,
		TracesEnabled:     cfg.Telemetry.Traces.Enabled,
	}, logger))

	// Webhook signature verification — priority 35 (after admin auth at 30, before rate limiting at 50)
	{
		paths := make([]webhooksigplugin.RuleConfig, 0, len(cfg.Webhooks.SignatureVerification.Paths))
		for _, p := range cfg.Webhooks.SignatureVerification.Paths {
			paths = append(paths, webhooksigplugin.RuleConfig{
				Path:         p.Path,
				Provider:     p.Provider,
				SecretEnvVar: p.SecretEnvVar,
				Header:       p.Header,
			})
		}
		registry.Register(webhooksigplugin.New(webhooksigplugin.Config{
			Enabled: cfg.Webhooks.SignatureVerification.Enabled,
			Paths:   paths,
		}, logger))
	}

	// Rate limiting — priority 50
	//
	// When cfg.RateLimit.Store is "redis", pass nil as the factory so the
	// plugin builds its own Redis (or Redis-with-fallback) factory during Init
	// based on the RedisConfig. When the store is "memory" (or empty), pass the
	// default memory factory directly for backward compatibility.
	var rlFactory ports.RateLimiterFactory
	if cfg.RateLimit.Store != "redis" {
		rlFactory = ratelimitadapter.NewDefaultMemoryFactory()
	}
	registry.Register(ratelimitplugin.New(ratelimitplugin.Config{
		Enabled:           cfg.RateLimit.Enabled,
		Store:             cfg.RateLimit.Store,
		TrustProxyHeaders: cfg.RateLimit.TrustProxyHeaders,
		ExemptPaths:       cfg.RateLimit.ExemptPaths,
		Redis: ratelimitplugin.RedisConfig{
			URL:                 cfg.RateLimit.Redis.URL,
			Address:             cfg.RateLimit.Redis.Address,
			Password:            cfg.RateLimit.Redis.Password,
			DB:                  cfg.RateLimit.Redis.DB,
			PoolSize:            cfg.RateLimit.Redis.PoolSize,
			KeyPrefix:           cfg.RateLimit.Redis.KeyPrefix,
			Fallback:            cfg.RateLimit.Redis.Fallback,
			HealthCheckInterval: cfg.RateLimit.Redis.HealthCheckInterval,
		},
		PerIP: ratelimitplugin.RuleConfig{
			RequestsPerSecond: cfg.RateLimit.PerIP.RequestsPerSecond,
			Burst:             cfg.RateLimit.PerIP.Burst,
		},
		PerUser: ratelimitplugin.RuleConfig{
			RequestsPerSecond: cfg.RateLimit.PerUser.RequestsPerSecond,
			Burst:             cfg.RateLimit.PerUser.Burst,
		},
	}, rlFactory, logger))

	// Auth — priority 40 (registered after rate-limiting for dependency clarity;
	// actual order is controlled by priority, but registry order matches intent)
	//
	// When auth.mode is "jwt" and jwks_url is non-empty, wire the HTTP JWKS
	// fetcher and JWT adapter here. When jwks_url is empty (local dev JWKS mode),
	// the auth plugin's Init handles key generation and adapter creation itself.
	var authIdentityProvider ports.IdentityProvider
	if cfg.Auth.Mode == config.AuthModeJWT {
		jwtCfg := cfg.Auth.JWT
		devJWKSMode := jwtCfg.JWKSURL == "" && jwtCfg.IssuerURL == ""
		if !devJWKSMode {
			jwtFetcher := jwtadapter.NewHTTPJWKSFetcher(
				jwtCfg.JWKSURL, 0, jwtCfg.CacheTTL, logger,
			)
			jwtAdapter, jwtErr := jwtadapter.NewAdapter(jwtadapter.Config{
				JWKSURL:           jwtCfg.JWKSURL,
				IssuerURL:         jwtCfg.IssuerURL,
				Issuer:            jwtCfg.Issuer,
				Audience:          jwtCfg.Audience,
				AllowedAlgorithms: jwtCfg.AllowedAlgorithms,
				ClaimsToHeaders:   jwtCfg.ClaimsToHeaders,
				CacheTTL:          jwtCfg.CacheTTL,
			}, jwtFetcher, logger)
			if jwtErr != nil {
				logger.Error("failed to create JWT adapter", slog.String("error", jwtErr.Error()))
			} else {
				authIdentityProvider = jwtAdapter
			}
		}
	}
	registry.Register(authplugin.New(authplugin.Config{
		Enabled:           cfg.Auth.Enabled,
		Mode:              authplugin.Mode(cfg.Auth.Mode),
		KratosPublicURL:   cfg.Kratos.PublicURL,
		KratosAdminURL:    cfg.Kratos.AdminURL,
		SessionCookieName: cfg.Auth.SessionCookieName,
		LoginURL:          cfg.Auth.LoginURL,
		PublicPaths:       cfg.Auth.PublicPaths,
		IdentitySchema:    cfg.Auth.IdentitySchema,
		JWT: authplugin.JWTPluginConfig{
			JWKSURL:   cfg.Auth.JWT.JWKSURL,
			IssuerURL: cfg.Auth.JWT.IssuerURL,
			Issuer:    cfg.Auth.JWT.Issuer,
			Audience:  cfg.Auth.JWT.Audience,
		},
	}, logger, authIdentityProvider))

	// User management — priority 60
	registry.Register(usermgmtplugin.New(usermgmtplugin.Config{
		Enabled:        cfg.Admin.Enabled,
		AdminToken:     cfg.Admin.Token,
		KratosAdminURL: cfg.Kratos.AdminURL,
		DatabaseURL:    cfg.Database.URL,
	}, eventLogger, logger))

	// Secrets (OpenBao) — priority 70
	registry.Register(buildSecretsPlugin(cfg, eventLogger, logger))

	// Egress proxy — priority 80 (independent listener; registered last)
	registry.Register(buildEgressPlugin(cfg, eventLogger, logger))
}
