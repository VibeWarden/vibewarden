package plugins

import (
	"log/slog"
	"time"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/domain/csp"
	egressplugin "github.com/vibewarden/vibewarden/internal/plugins/egress"
	inputvalidationplugin "github.com/vibewarden/vibewarden/internal/plugins/inputvalidation"
	secretsplugin "github.com/vibewarden/vibewarden/internal/plugins/secrets"
	tlsplugin "github.com/vibewarden/vibewarden/internal/plugins/tls"
	"github.com/vibewarden/vibewarden/internal/ports"
)

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

// buildTLSPlugin constructs the TLS plugin from cfg, parsing the optional
// cert_monitoring duration strings into time.Duration values. Falls back to
// plugin defaults on parse errors.
func buildTLSPlugin(cfg *config.Config, eventLogger ports.EventLogger, logger *slog.Logger) *tlsplugin.Plugin {
	monCfg := ports.TLSCertMonitoringConfig{
		Enabled: cfg.TLS.CertMonitoring.Enabled,
	}

	if cfg.TLS.CertMonitoring.CheckInterval != "" {
		if d, err := time.ParseDuration(cfg.TLS.CertMonitoring.CheckInterval); err != nil {
			logger.Warn("tls.cert_monitoring.check_interval parse error — using default",
				slog.String("error", err.Error()))
		} else {
			monCfg.CheckInterval = d
		}
	}
	if cfg.TLS.CertMonitoring.WarningThreshold != "" {
		if d, err := time.ParseDuration(cfg.TLS.CertMonitoring.WarningThreshold); err != nil {
			logger.Warn("tls.cert_monitoring.warning_threshold parse error — using default",
				slog.String("error", err.Error()))
		} else {
			monCfg.WarningThreshold = d
		}
	}
	if cfg.TLS.CertMonitoring.CriticalThreshold != "" {
		if d, err := time.ParseDuration(cfg.TLS.CertMonitoring.CriticalThreshold); err != nil {
			logger.Warn("tls.cert_monitoring.critical_threshold parse error — using default",
				slog.String("error", err.Error()))
		} else {
			monCfg.CriticalThreshold = d
		}
	}

	return tlsplugin.New(ports.TLSConfig{
		Enabled:        cfg.TLS.Enabled,
		Provider:       ports.TLSProvider(cfg.TLS.Provider),
		Domain:         cfg.TLS.Domain,
		CertPath:       cfg.TLS.CertPath,
		KeyPath:        cfg.TLS.KeyPath,
		StoragePath:    cfg.TLS.StoragePath,
		CertMonitoring: monCfg,
	}, eventLogger, logger)
}

// buildInputValidationPlugin constructs the input validation plugin from cfg.
func buildInputValidationPlugin(cfg *config.Config, logger *slog.Logger) *inputvalidationplugin.Plugin {
	iv := cfg.InputValidation

	pluginCfg := inputvalidationplugin.Config{
		Enabled:              iv.Enabled,
		MaxURLLength:         iv.MaxURLLength,
		MaxQueryStringLength: iv.MaxQueryStringLength,
		MaxHeaderCount:       iv.MaxHeaderCount,
		MaxHeaderSize:        iv.MaxHeaderSize,
	}

	for _, ov := range iv.PathOverrides {
		pluginCfg.PathOverrides = append(pluginCfg.PathOverrides, inputvalidationplugin.PathOverrideConfig{
			Path:                 ov.Path,
			MaxURLLength:         ov.MaxURLLength,
			MaxQueryStringLength: ov.MaxQueryStringLength,
			MaxHeaderCount:       ov.MaxHeaderCount,
			MaxHeaderSize:        ov.MaxHeaderSize,
		})
	}

	return inputvalidationplugin.New(pluginCfg, logger)
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

		cacheTTL, cacheTTLErr := time.ParseDuration(rc.Cache.TTL)
		if cacheTTLErr != nil && rc.Cache.TTL != "" {
			logger.Warn("egress route cache.ttl parse error — disabling TTL for route",
				slog.String("route", rc.Name),
				slog.String("error", cacheTTLErr.Error()),
			)
		}

		cacheMaxSizeBytes, _ := config.ParseBodySize(rc.Cache.MaxSize)

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
			Headers: egressplugin.HeadersConfig{
				Add:            rc.Headers.Add,
				RemoveRequest:  rc.Headers.RemoveRequest,
				RemoveResponse: rc.Headers.RemoveResponse,
			},
			Cache: egressplugin.CacheConfig{
				Enabled: rc.Cache.Enabled,
				TTL:     cacheTTL,
				MaxSize: cacheMaxSizeBytes,
			},
			Sanitize: egressplugin.SanitizeConfig{
				Headers:     rc.Sanitize.Headers,
				QueryParams: rc.Sanitize.QueryParams,
				BodyFields:  rc.Sanitize.BodyFields,
			},
			MTLS: egressplugin.MTLSConfig{
				CertPath: rc.MTLS.CertPath,
				KeyPath:  rc.MTLS.KeyPath,
				CAPath:   rc.MTLS.CAPath,
			},
		})
	}

	return egressplugin.New(pluginCfg, eventLogger, logger)
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
