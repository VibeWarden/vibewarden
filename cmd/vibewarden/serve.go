package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	caddyadapter "github.com/vibewarden/vibewarden/internal/adapters/caddy"
	logadapter "github.com/vibewarden/vibewarden/internal/adapters/log"
	ratelimitadapter "github.com/vibewarden/vibewarden/internal/adapters/ratelimit"
	"github.com/vibewarden/vibewarden/internal/app/proxy"
	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/plugins"
	authplugin "github.com/vibewarden/vibewarden/internal/plugins/auth"
	bodysizeplugin "github.com/vibewarden/vibewarden/internal/plugins/bodysize"
	corsplugin "github.com/vibewarden/vibewarden/internal/plugins/cors"
	ipfilterplugin "github.com/vibewarden/vibewarden/internal/plugins/ipfilter"
	metricsplugin "github.com/vibewarden/vibewarden/internal/plugins/metrics"
	ratelimitplugin "github.com/vibewarden/vibewarden/internal/plugins/ratelimit"
	secretsplugin "github.com/vibewarden/vibewarden/internal/plugins/secrets"
	sechdrs "github.com/vibewarden/vibewarden/internal/plugins/securityheaders"
	tlsplugin "github.com/vibewarden/vibewarden/internal/plugins/tls"
	usermgmtplugin "github.com/vibewarden/vibewarden/internal/plugins/usermgmt"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// newServeCmd creates the serve subcommand.
func newServeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the VibeWarden reverse proxy",
		Long: `Start the VibeWarden security sidecar reverse proxy.

Reads configuration from vibewarden.yaml (or the path specified with --config).
Listens for SIGINT/SIGTERM and performs a graceful shutdown.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to vibewarden.yaml (default: ./vibewarden.yaml)")

	return cmd
}

// runServe loads config, builds the plugin registry, wires Caddy via plugin
// contributors, and runs until a shutdown signal is received.
func runServe(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := buildLogger(cfg.Log)

	logger.Info("VibeWarden starting",
		slog.String("version", version),
		slog.String("listen", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)),
		slog.String("upstream", fmt.Sprintf("%s:%d", cfg.Upstream.Host, cfg.Upstream.Port)),
	)

	// Initialize the EventLogger — writes structured JSON events to stdout.
	eventLogger := logadapter.NewSlogEventLogger(os.Stdout)

	// Build the plugin registry and register all compiled-in plugins.
	registry := plugins.NewRegistry(logger)
	registerPlugins(registry, cfg, eventLogger, logger)

	// Set up OS signal handling before Init/Start so that a slow Init can
	// still be interrupted.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received shutdown signal", slog.String("signal", sig.String()))
		cancel()
	}()

	// Initialise all plugins.
	if err := registry.InitAll(ctx); err != nil {
		return fmt.Errorf("initialising plugins: %w", err)
	}

	// Start all plugins (background servers, etc.).
	if err := registry.StartAll(ctx); err != nil {
		return fmt.Errorf("starting plugins: %w", err)
	}

	// Ensure StopAll runs on return (normal or error path).
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		if stopErr := registry.StopAll(stopCtx); stopErr != nil {
			logger.Error("stopping plugins", slog.String("error", stopErr.Error()))
		}
	}()

	// Build ProxyConfig — base fields that the Caddy adapter uses directly.
	// Plugin-specific wiring (security headers, rate limiting, auth, admin,
	// metrics) is now driven by each plugin's CaddyContributor implementation.
	// The legacy top-level fields are still populated for the existing
	// BuildCaddyConfig path; a follow-up issue will migrate that fully to
	// the contributor model.
	proxyCfg := buildProxyConfig(cfg, registry)

	// Create Caddy adapter and proxy service.
	adapter := caddyadapter.NewAdapter(proxyCfg, logger, eventLogger)
	svc := proxy.NewService(adapter, logger)

	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("proxy service: %w", err)
	}

	return nil
}

// registerPlugins creates each compiled-in plugin from cfg and registers it
// with the registry. Registration order matches plugin priority (low → high).
func registerPlugins(
	registry *plugins.Registry,
	cfg *config.Config,
	eventLogger ports.EventLogger,
	logger *slog.Logger,
) {
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
		Enabled:      cfg.Metrics.Enabled,
		PathPatterns: cfg.Metrics.PathPatterns,
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
	registry.Register(authplugin.New(authplugin.Config{
		Enabled:           cfg.Auth.Enabled,
		KratosPublicURL:   cfg.Kratos.PublicURL,
		KratosAdminURL:    cfg.Kratos.AdminURL,
		SessionCookieName: cfg.Auth.SessionCookieName,
		LoginURL:          cfg.Auth.LoginURL,
		PublicPaths:       cfg.Auth.PublicPaths,
		IdentitySchema:    cfg.Auth.IdentitySchema,
	}, logger, nil))

	// User management — priority 60
	registry.Register(usermgmtplugin.New(usermgmtplugin.Config{
		Enabled:        cfg.Admin.Enabled,
		AdminToken:     cfg.Admin.Token,
		KratosAdminURL: cfg.Kratos.AdminURL,
		DatabaseURL:    cfg.Database.URL,
	}, eventLogger, logger))

	// Secrets (OpenBao) — priority 70
	registry.Register(buildSecretsPlugin(cfg, eventLogger, logger))
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
					Enabled:      cfg.Metrics.Enabled,
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

// buildLogger creates an slog.Logger from the log configuration.
func buildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}
