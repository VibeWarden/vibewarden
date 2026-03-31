package main

import (
	"log/slog"
	"time"

	jwtadapter "github.com/vibewarden/vibewarden/internal/adapters/jwt"
	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	authplugin "github.com/vibewarden/vibewarden/internal/plugins/auth"
	bodysizeplugin "github.com/vibewarden/vibewarden/internal/plugins/bodysize"
	corsplugin "github.com/vibewarden/vibewarden/internal/plugins/cors"
	ipfilterplugin "github.com/vibewarden/vibewarden/internal/plugins/ipfilter"
	maintenanceplugin "github.com/vibewarden/vibewarden/internal/plugins/maintenance"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	ratelimitplugin "github.com/vibewarden/vibewarden/internal/plugins/ratelimit"
	secretsplugin "github.com/vibewarden/vibewarden/internal/plugins/secrets"
	sechdrs "github.com/vibewarden/vibewarden/internal/plugins/securityheaders"
	tlsplugin "github.com/vibewarden/vibewarden/internal/plugins/tls"
	usermgmtplugin "github.com/vibewarden/vibewarden/internal/plugins/usermgmt"
	wafplugin "github.com/vibewarden/vibewarden/internal/plugins/waf"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// registerPlugins creates each compiled-in plugin from cfg and registers it
// with the registry. Registration order matches plugin priority (low → high).
func registerPlugins(
	registry *plugins.Registry,
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
	registry.Register(tlsplugin.New(ports.TLSConfig{
		Enabled:     cfg.TLS.Enabled,
		Provider:    ports.TLSProvider(cfg.TLS.Provider),
		Domain:      cfg.TLS.Domain,
		CertPath:    cfg.TLS.CertPath,
		KeyPath:     cfg.TLS.KeyPath,
		StoragePath: cfg.TLS.StoragePath,
	}, logger))

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

	// WAF — priority 25 (after security headers at 20, before admin auth at 30)
	registry.Register(wafplugin.New(wafplugin.Config{
		ContentTypeValidation: wafplugin.ContentTypeValidationConfig{
			Enabled: cfg.WAF.ContentTypeValidation.Enabled,
			Allowed: cfg.WAF.ContentTypeValidation.Allowed,
		},
	}, logger))

	// Security headers — priority 20
	registry.Register(sechdrs.New(sechdrs.Config{
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

	// Rate limiting — priority 50
	registry.Register(ratelimitplugin.New(ratelimitplugin.Config{
		Enabled:           cfg.RateLimit.Enabled,
		Store:             "memory",
		TrustProxyHeaders: cfg.RateLimit.TrustProxyHeaders,
		ExemptPaths:       cfg.RateLimit.ExemptPaths,
		PerIP: ratelimitplugin.RuleConfig{
			RequestsPerSecond: cfg.RateLimit.PerIP.RequestsPerSecond,
			Burst:             cfg.RateLimit.PerIP.Burst,
		},
		PerUser: ratelimitplugin.RuleConfig{
			RequestsPerSecond: cfg.RateLimit.PerUser.RequestsPerSecond,
			Burst:             cfg.RateLimit.PerUser.Burst,
		},
	}, ratelimitadapter.NewDefaultMemoryFactory(), logger))

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

// buildSecretsPlugin constructs the secrets plugin from config, parsing
// duration strings into time.Duration values. Falls back to plugin defaults
// on parse errors (config.Validate does not validate duration strings here
// since they are optional and have sensible defaults).
func buildSecretsPlugin(cfg *config.Config, eventLogger ports.EventLogger, logger *slog.Logger) *secretsplugin.Plugin {
	secretsCfg := secretsplugin.Config{
		Enabled:  cfg.Secrets.Enabled,
		Provider: cfg.Secrets.Provider,
		OpenBao: secretsplugin.OpenBaoConfig{
			Address:   cfg.Secrets.OpenBao.Address,
			MountPath: cfg.Secrets.OpenBao.MountPath,
			Auth: secretsplugin.OpenBaoAuthConfig{
				Method:   cfg.Secrets.OpenBao.Auth.Method,
				Token:    cfg.Secrets.OpenBao.Auth.Token,
				RoleID:   cfg.Secrets.OpenBao.Auth.RoleID,
				SecretID: cfg.Secrets.OpenBao.Auth.SecretID,
			},
		},
		Dynamic: secretsplugin.DynamicConfig{
			Postgres: secretsplugin.DynamicPostgresConfig{
				Enabled: cfg.Secrets.Dynamic.Postgres.Enabled,
			},
		},
		Inject: secretsplugin.InjectConfig{
			EnvFile: cfg.Secrets.Inject.EnvFile,
		},
		Health: secretsplugin.HealthConfig{
			WeakPatterns: cfg.Secrets.Health.WeakPatterns,
		},
	}

	// Parse optional duration strings.
	if cfg.Secrets.CacheTTL != "" {
		if d, err := time.ParseDuration(cfg.Secrets.CacheTTL); err != nil {
			logger.Warn("secrets.cache_ttl parse error — using default", slog.String("error", err.Error()))
		} else {
			secretsCfg.CacheTTL = d
		}
	}
	if cfg.Secrets.Health.CheckInterval != "" {
		if d, err := time.ParseDuration(cfg.Secrets.Health.CheckInterval); err != nil {
			logger.Warn("secrets.health.check_interval parse error — using default", slog.String("error", err.Error()))
		} else {
			secretsCfg.Health.CheckInterval = d
		}
	}
	if cfg.Secrets.Health.MaxStaticAge != "" {
		if d, err := time.ParseDuration(cfg.Secrets.Health.MaxStaticAge); err != nil {
			logger.Warn("secrets.health.max_static_age parse error — using default", slog.String("error", err.Error()))
		} else {
			secretsCfg.Health.MaxStaticAge = d
		}
	}

	// Map header injections.
	for _, inj := range cfg.Secrets.Inject.Headers {
		secretsCfg.Inject.Headers = append(secretsCfg.Inject.Headers, secretsplugin.HeaderInjection{
			SecretPath: inj.SecretPath,
			SecretKey:  inj.SecretKey,
			Header:     inj.Header,
		})
	}

	// Map env injections.
	for _, inj := range cfg.Secrets.Inject.Env {
		secretsCfg.Inject.Env = append(secretsCfg.Inject.Env, secretsplugin.EnvInjection{
			SecretPath: inj.SecretPath,
			SecretKey:  inj.SecretKey,
			EnvVar:     inj.EnvVar,
		})
	}

	// Map dynamic postgres roles.
	for _, role := range cfg.Secrets.Dynamic.Postgres.Roles {
		secretsCfg.Dynamic.Postgres.Roles = append(secretsCfg.Dynamic.Postgres.Roles, secretsplugin.DynamicRole{
			Name:           role.Name,
			EnvVarUser:     role.EnvVarUser,
			EnvVarPassword: role.EnvVarPassword,
		})
	}

	return secretsplugin.New(secretsCfg, eventLogger, logger)
}
