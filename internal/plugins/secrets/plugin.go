package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vibewarden/vibewarden/internal/adapters/openbao"
	"github.com/vibewarden/vibewarden/internal/domain/events"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	defaultCacheTTL      = 5 * time.Minute
	defaultCheckInterval = 6 * time.Hour
	defaultMaxStaticAge  = 90 * 24 * time.Hour
	defaultMountPath     = "secret"
	// renewThreshold is the fraction of TTL remaining that triggers lease renewal.
	renewThreshold = 0.25
)

// Plugin is the VibeWarden secret management plugin.
// It implements ports.Plugin and ports.CaddyContributor.
//
// On startup it:
//  1. Connects to OpenBao and authenticates.
//  2. Fetches all configured static secrets into an in-memory cache.
//  3. Requests dynamic credentials (if configured) and writes them to an env file.
//  4. Starts background goroutines for cache refresh, credential rotation,
//     and secret health checks.
//
// On every proxied request the plugin injects secret-backed HTTP headers via
// the CaddyContributor interface.
type Plugin struct {
	cfg       Config
	store     *openbao.Adapter
	logger    *slog.Logger
	eventLog  ports.EventLogger
	healthy   bool
	healthMsg string

	// cache holds the latest fetched static secrets, keyed by "path/key".
	cache   map[string]string
	cacheMu sync.RWMutex

	// dynCreds holds the current dynamic credentials, keyed by role name.
	dynCreds   map[string]*openbao.DynamicCredentials
	dynCredsMu sync.RWMutex

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a new secrets Plugin.
// eventLog may be nil; when non-nil, secret rotation and health events are
// emitted through it.
func New(cfg Config, eventLog ports.EventLogger, logger *slog.Logger) *Plugin {
	applyDefaults(&cfg)
	return &Plugin{
		cfg:       cfg,
		logger:    logger,
		eventLog:  eventLog,
		cache:     make(map[string]string),
		dynCreds:  make(map[string]*openbao.DynamicCredentials),
		stopCh:    make(chan struct{}),
		healthMsg: "not initialised",
	}
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg *Config) {
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = defaultCacheTTL
	}
	if cfg.Health.CheckInterval == 0 {
		cfg.Health.CheckInterval = defaultCheckInterval
	}
	if cfg.Health.MaxStaticAge == 0 {
		cfg.Health.MaxStaticAge = defaultMaxStaticAge
	}
	if len(cfg.Health.WeakPatterns) == 0 {
		cfg.Health.WeakPatterns = []string{"password", "changeme", "secret", "123456", "admin", "letmein"}
	}
	if cfg.OpenBao.MountPath == "" {
		cfg.OpenBao.MountPath = defaultMountPath
	}
	if cfg.Provider == "" {
		cfg.Provider = "openbao"
	}
}

// Name returns the canonical plugin identifier "secrets".
func (p *Plugin) Name() string { return "secrets" }

// Init connects to OpenBao, authenticates, and pre-fetches static secrets.
// Returns an error if the plugin is enabled and the connection fails.
func (p *Plugin) Init(ctx context.Context) error {
	if !p.cfg.Enabled {
		p.healthy = true
		p.healthMsg = "secrets plugin disabled"
		return nil
	}

	if p.cfg.Provider != "openbao" {
		return fmt.Errorf("secrets plugin: unsupported provider %q (only \"openbao\" is supported)", p.cfg.Provider)
	}

	if p.cfg.OpenBao.Address == "" {
		return fmt.Errorf("secrets plugin: openbao.address is required when secrets plugin is enabled")
	}

	// Build the OpenBao adapter.
	p.store = openbao.New(openbao.Config{
		Address: p.cfg.OpenBao.Address,
		Auth: openbao.AuthConfig{
			Method:   openbao.AuthMethod(p.cfg.OpenBao.Auth.Method),
			Token:    p.cfg.OpenBao.Auth.Token,
			RoleID:   p.cfg.OpenBao.Auth.RoleID,
			SecretID: p.cfg.OpenBao.Auth.SecretID,
		},
		MountPath: p.cfg.OpenBao.MountPath,
	}, p.logger)

	// Authenticate to OpenBao.
	if err := p.store.Authenticate(ctx); err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("authentication failed: %s", err.Error())
		return fmt.Errorf("secrets plugin init: authenticate: %w", err)
	}

	// Verify connectivity.
	if err := p.store.Health(ctx); err != nil {
		p.healthy = false
		p.healthMsg = fmt.Sprintf("openbao unhealthy: %s", err.Error())
		return fmt.Errorf("secrets plugin init: health check: %w", err)
	}

	// Pre-fetch all configured static secrets into cache.
	if err := p.refreshCache(ctx); err != nil {
		// Non-fatal — warn and continue; the background loop will retry.
		p.logger.WarnContext(ctx, "secrets plugin: initial cache refresh failed",
			slog.String("error", err.Error()),
		)
	}

	// Request dynamic credentials if configured.
	if p.cfg.Dynamic.Postgres.Enabled {
		if err := p.refreshDynamicCredentials(ctx); err != nil {
			p.logger.WarnContext(ctx, "secrets plugin: initial dynamic credential fetch failed",
				slog.String("error", err.Error()),
			)
		}
	}

	// Write env file if configured.
	if p.cfg.Inject.EnvFile != "" {
		if err := p.writeEnvFile(); err != nil {
			p.logger.WarnContext(ctx, "secrets plugin: failed to write env file",
				slog.String("path", p.cfg.Inject.EnvFile),
				slog.String("error", err.Error()),
			)
		}
	}

	p.healthy = true
	p.healthMsg = "connected to OpenBao"
	p.logger.InfoContext(ctx, "secrets plugin initialised",
		slog.String("address", p.cfg.OpenBao.Address),
		slog.Int("static_injections", len(p.cfg.Inject.Headers)+len(p.cfg.Inject.Env)),
		slog.Bool("dynamic_postgres", p.cfg.Dynamic.Postgres.Enabled),
	)
	return nil
}

// Start launches background goroutines for cache refresh, credential rotation,
// and secret health checks.
func (p *Plugin) Start(ctx context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}

	// Background cache refresh.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runCacheRefreshLoop(ctx)
	}()

	// Background credential rotation (only when dynamic postgres is enabled).
	if p.cfg.Dynamic.Postgres.Enabled {
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runCredentialRotationLoop(ctx)
		}()
	}

	// Background health checks.
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runHealthCheckLoop(ctx)
	}()

	return nil
}

// Stop signals background goroutines to stop and waits for them to finish.
func (p *Plugin) Stop(_ context.Context) error {
	if !p.cfg.Enabled {
		return nil
	}
	close(p.stopCh)
	p.wg.Wait()
	p.healthy = false
	p.healthMsg = "stopped"
	return nil
}

// Health returns the current health status of the secrets plugin.
func (p *Plugin) Health() ports.HealthStatus {
	return ports.HealthStatus{
		Healthy: p.healthy,
		Message: p.healthMsg,
	}
}

// ContributeCaddyRoutes returns nil — the secrets plugin does not add named routes.
func (p *Plugin) ContributeCaddyRoutes() []ports.CaddyRoute { return nil }

// ContributeCaddyHandlers returns a Caddy headers handler that injects
// secret-backed headers into every proxied request.
// Returns nil when no header injections are configured or the plugin is disabled.
func (p *Plugin) ContributeCaddyHandlers() []ports.CaddyHandler {
	if !p.cfg.Enabled || len(p.cfg.Inject.Headers) == 0 {
		return nil
	}

	return []ports.CaddyHandler{
		{
			Handler:  p.buildRequestHeadersHandler(),
			Priority: 35,
		},
	}
}

// buildRequestHeadersHandler creates a Caddy headers handler that injects
// the current cached secret values as request headers.
func (p *Plugin) buildRequestHeadersHandler() map[string]any {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	set := map[string][]string{}
	for _, inj := range p.cfg.Inject.Headers {
		cacheKey := cacheKeyFor(inj.SecretPath, inj.SecretKey)
		if val, ok := p.cache[cacheKey]; ok && val != "" {
			set[inj.Header] = []string{val}
		}
	}

	return map[string]any{
		"handler": "headers",
		"request": map[string]any{
			"set": set,
		},
	}
}

// GetCachedSecret returns the cached value for the given path/key combination.
// Returns ("", false) when the entry is not in the cache.
// This method is safe for concurrent use.
func (p *Plugin) GetCachedSecret(path, key string) (string, bool) {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	val, ok := p.cache[cacheKeyFor(path, key)]
	return val, ok
}

// refreshCache fetches all secrets referenced by inject.headers and inject.env
// and stores them in the in-memory cache. Non-fatal: missing secrets are logged
// but do not block the refresh.
func (p *Plugin) refreshCache(ctx context.Context) error {
	// Collect all unique paths to fetch.
	paths := make(map[string]struct{})
	for _, inj := range p.cfg.Inject.Headers {
		paths[inj.SecretPath] = struct{}{}
	}
	for _, inj := range p.cfg.Inject.Env {
		paths[inj.SecretPath] = struct{}{}
	}

	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()

	var firstErr error
	for path := range paths {
		data, err := p.store.Get(ctx, path)
		if err != nil {
			p.logger.WarnContext(ctx, "secrets plugin: failed to fetch secret",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for k, v := range data {
			p.cache[cacheKeyFor(path, k)] = v
		}
	}
	return firstErr
}

// refreshDynamicCredentials requests fresh credentials for all configured roles.
func (p *Plugin) refreshDynamicCredentials(ctx context.Context) error {
	for _, role := range p.cfg.Dynamic.Postgres.Roles {
		creds, err := p.store.RequestDynamicCredentials(ctx, role.Name)
		if err != nil {
			p.logger.WarnContext(ctx, "secrets plugin: failed to fetch dynamic credentials",
				slog.String("role", role.Name),
				slog.String("error", err.Error()),
			)
			p.emitEvent(ctx, events.EventTypeSecretRotationFailed, fmt.Sprintf(
				"failed to fetch dynamic credentials for role %q: %s", role.Name, err.Error(),
			), map[string]any{
				"role":  role.Name,
				"error": err.Error(),
			})
			continue
		}

		p.dynCredsMu.Lock()
		p.dynCreds[role.Name] = creds
		p.dynCredsMu.Unlock()

		// Inject into env cache for env file writing.
		p.cacheMu.Lock()
		if role.EnvVarUser != "" {
			p.cache[cacheKeyFor("_dynamic/"+role.Name, "user")] = creds.Username
		}
		if role.EnvVarPassword != "" {
			p.cache[cacheKeyFor("_dynamic/"+role.Name, "password")] = creds.Password
		}
		p.cacheMu.Unlock()

		p.logger.InfoContext(ctx, "secrets plugin: dynamic credentials obtained",
			slog.String("role", role.Name),
			slog.String("username", creds.Username),
			slog.Time("expires_at", creds.ExpiresAt()),
		)
	}
	return nil
}

// writeEnvFile writes all env-injected secrets and dynamic credentials to the
// configured env file path. Secret values are written without logging them.
func (p *Plugin) writeEnvFile() error {
	if p.cfg.Inject.EnvFile == "" {
		return nil
	}

	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(p.cfg.Inject.EnvFile), 0o700); err != nil {
		return fmt.Errorf("creating env file directory: %w", err)
	}

	var sb strings.Builder

	// Static env injections.
	p.cacheMu.RLock()
	for _, inj := range p.cfg.Inject.Env {
		cacheKey := cacheKeyFor(inj.SecretPath, inj.SecretKey)
		if val, ok := p.cache[cacheKey]; ok {
			sb.WriteString(inj.EnvVar)
			sb.WriteString("=")
			sb.WriteString(val)
			sb.WriteString("\n")
		}
	}

	// Dynamic Postgres credential injections.
	for _, role := range p.cfg.Dynamic.Postgres.Roles {
		userKey := cacheKeyFor("_dynamic/"+role.Name, "user")
		passKey := cacheKeyFor("_dynamic/"+role.Name, "password")
		if role.EnvVarUser != "" {
			if val, ok := p.cache[userKey]; ok {
				sb.WriteString(role.EnvVarUser)
				sb.WriteString("=")
				sb.WriteString(val)
				sb.WriteString("\n")
			}
		}
		if role.EnvVarPassword != "" {
			if val, ok := p.cache[passKey]; ok {
				sb.WriteString(role.EnvVarPassword)
				sb.WriteString("=")
				sb.WriteString(val)
				sb.WriteString("\n")
			}
		}
	}
	p.cacheMu.RUnlock()

	// Write atomically via a temp file.
	tmpFile := p.cfg.Inject.EnvFile + ".tmp"
	if err := os.WriteFile(tmpFile, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}
	if err := os.Rename(tmpFile, p.cfg.Inject.EnvFile); err != nil {
		return fmt.Errorf("renaming env file: %w", err)
	}
	return nil
}

// runCacheRefreshLoop periodically re-fetches static secrets into the cache.
func (p *Plugin) runCacheRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.CacheTTL)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.refreshCache(ctx); err != nil {
				p.logger.WarnContext(ctx, "secrets plugin: cache refresh failed (serving stale)",
					slog.String("error", err.Error()),
				)
			} else if p.cfg.Inject.EnvFile != "" {
				if err := p.writeEnvFile(); err != nil {
					p.logger.WarnContext(ctx, "secrets plugin: env file refresh failed",
						slog.String("error", err.Error()),
					)
				}
			}
		}
	}
}

// runCredentialRotationLoop periodically checks dynamic credential TTLs and
// renews them before they expire (at 75% of TTL elapsed).
func (p *Plugin) runCredentialRotationLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.rotateDynamicCredentialsIfNeeded(ctx)
		}
	}
}

// rotateDynamicCredentialsIfNeeded checks each dynamic credential and renews
// any that are within 25% of their TTL remaining.
func (p *Plugin) rotateDynamicCredentialsIfNeeded(ctx context.Context) {
	for _, role := range p.cfg.Dynamic.Postgres.Roles {
		p.dynCredsMu.RLock()
		creds, ok := p.dynCreds[role.Name]
		p.dynCredsMu.RUnlock()

		if !ok {
			continue
		}

		remaining := time.Until(creds.ExpiresAt())
		threshold := time.Duration(float64(creds.TTL) * renewThreshold)
		if remaining > threshold {
			continue
		}

		// Credentials are within the renewal window — try lease renewal first.
		p.logger.InfoContext(ctx, "secrets plugin: renewing dynamic credential lease",
			slog.String("role", role.Name),
			slog.Duration("remaining", remaining),
		)

		newTTL, err := p.store.RenewLease(ctx, creds.LeaseID, int(creds.TTL.Seconds()))
		if err == nil {
			p.dynCredsMu.Lock()
			p.dynCreds[role.Name].TTL = newTTL
			p.dynCreds[role.Name].IssuedAt = time.Now()
			p.dynCredsMu.Unlock()

			p.emitEvent(ctx, events.EventTypeSecretRotated,
				fmt.Sprintf("dynamic credential for role %q renewed (new TTL: %s)", role.Name, newTTL),
				map[string]any{"role": role.Name, "new_ttl_seconds": int(newTTL.Seconds())},
			)
			continue
		}

		// Renewal failed — request a completely new set of credentials.
		p.logger.WarnContext(ctx, "secrets plugin: lease renewal failed, requesting new credentials",
			slog.String("role", role.Name),
			slog.String("error", err.Error()),
		)
		newCreds, newErr := p.store.RequestDynamicCredentials(ctx, role.Name)
		if newErr != nil {
			p.logger.ErrorContext(ctx, "secrets plugin: credential rotation failed",
				slog.String("role", role.Name),
				slog.String("error", newErr.Error()),
			)
			p.emitEvent(ctx, events.EventTypeSecretRotationFailed,
				fmt.Sprintf("credential rotation for role %q failed: %s", role.Name, newErr.Error()),
				map[string]any{"role": role.Name, "error": newErr.Error()},
			)
			continue
		}

		// Store new credentials and update the env file.
		p.dynCredsMu.Lock()
		oldCreds := p.dynCreds[role.Name]
		p.dynCreds[role.Name] = newCreds
		p.dynCredsMu.Unlock()

		p.cacheMu.Lock()
		if role.EnvVarUser != "" {
			p.cache[cacheKeyFor("_dynamic/"+role.Name, "user")] = newCreds.Username
		}
		if role.EnvVarPassword != "" {
			p.cache[cacheKeyFor("_dynamic/"+role.Name, "password")] = newCreds.Password
		}
		p.cacheMu.Unlock()

		if p.cfg.Inject.EnvFile != "" {
			if writeErr := p.writeEnvFile(); writeErr != nil {
				p.logger.WarnContext(ctx, "secrets plugin: env file update after rotation failed",
					slog.String("error", writeErr.Error()),
				)
			}
		}

		// Revoke the old lease after a short grace period.
		// context.Background is intentional: revocation must outlive the request context.
		go func(old *openbao.DynamicCredentials) { //nolint:gosec // G118: background context is correct — revocation runs after the request completes
			time.Sleep(5 * time.Second)
			revokeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if revokeErr := p.store.RevokeLease(revokeCtx, old.LeaseID); revokeErr != nil {
				p.logger.Warn("secrets plugin: old lease revocation failed",
					slog.String("lease_id", old.LeaseID),
					slog.String("error", revokeErr.Error()),
				)
			}
		}(oldCreds)

		p.emitEvent(ctx, events.EventTypeSecretRotated,
			fmt.Sprintf("dynamic credential for role %q rotated (new user: %s)", role.Name, newCreds.Username),
			map[string]any{"role": role.Name, "new_username": newCreds.Username},
		)
	}
}

// runHealthCheckLoop periodically runs secret health checks and emits findings.
func (p *Plugin) runHealthCheckLoop(ctx context.Context) {
	// Run immediately on start.
	p.runHealthCheck(ctx)

	ticker := time.NewTicker(p.cfg.Health.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.runHealthCheck(ctx)
		}
	}
}

// Severity levels for health findings.
const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
)

// HealthFinding describes a single issue found during a health check.
type HealthFinding struct {
	// Path is the secret store path that has the issue.
	Path string `json:"path"`

	// Check is the check name: "weak", "short", "stale", "expiring", "rotation_failed".
	Check string `json:"check"`

	// Severity is "critical" or "warning".
	Severity string `json:"severity"`

	// Message is a human-readable description of the finding.
	Message string `json:"message"`
}

// runHealthCheck performs a single health check pass and emits findings.
func (p *Plugin) runHealthCheck(ctx context.Context) {
	var findings []HealthFinding

	// Check static secrets via metadata (no value reading — only timestamps/length).
	for _, inj := range p.cfg.Inject.Headers {
		p.checkStaticSecret(ctx, inj.SecretPath, inj.SecretKey, &findings)
	}
	for _, inj := range p.cfg.Inject.Env {
		p.checkStaticSecret(ctx, inj.SecretPath, inj.SecretKey, &findings)
	}

	// Check dynamic credentials for expiring leases.
	if p.cfg.Dynamic.Postgres.Enabled {
		for _, role := range p.cfg.Dynamic.Postgres.Roles {
			p.dynCredsMu.RLock()
			creds, ok := p.dynCreds[role.Name]
			p.dynCredsMu.RUnlock()

			if !ok {
				findings = append(findings, HealthFinding{
					Path:     "_dynamic/" + role.Name,
					Check:    "rotation_failed",
					Severity: SeverityCritical,
					Message:  fmt.Sprintf("no dynamic credentials available for role %q", role.Name),
				})
				continue
			}

			remaining := time.Until(creds.ExpiresAt())
			tenPercent := time.Duration(float64(creds.TTL) * 0.10)
			if remaining < tenPercent {
				findings = append(findings, HealthFinding{
					Path:     "_dynamic/" + role.Name,
					Check:    "expiring",
					Severity: SeverityWarning,
					Message: fmt.Sprintf(
						"dynamic credentials for role %q expire in %s (< 10%% TTL remaining)",
						role.Name, remaining.Round(time.Second),
					),
				})
			}
		}
	}

	findingMaps := make([]map[string]any, len(findings))
	for i, f := range findings {
		findingMaps[i] = map[string]any{
			"path":     f.Path,
			"check":    f.Check,
			"severity": f.Severity,
			"message":  f.Message,
		}
	}

	summary := fmt.Sprintf("secret health check: %d finding(s)", len(findings))
	if len(findings) == 0 {
		summary = "secret health check: all secrets healthy"
	}

	p.emitEvent(ctx, events.EventTypeSecretHealthCheck, summary, map[string]any{
		"finding_count": len(findings),
		"findings":      findingMaps,
	})

	if len(findings) > 0 {
		p.logger.WarnContext(ctx, "secrets plugin: health check findings",
			slog.Int("count", len(findings)),
		)
	}
}

// checkStaticSecret performs health checks on a single static secret.
// It checks staleness via metadata and weakness/length via the cached value.
func (p *Plugin) checkStaticSecret(ctx context.Context, path, key string, findings *[]HealthFinding) {
	// Staleness check via metadata (no value needed).
	meta, err := p.store.GetMetadata(ctx, path)
	if err == nil && !meta.UpdatedTime.IsZero() {
		age := time.Since(meta.UpdatedTime)
		if age > p.cfg.Health.MaxStaticAge {
			*findings = append(*findings, HealthFinding{
				Path:     path,
				Check:    "stale",
				Severity: SeverityWarning,
				Message: fmt.Sprintf(
					"secret at %q (key: %q) has not been updated in %s (max: %s)",
					path, key, age.Round(time.Hour), p.cfg.Health.MaxStaticAge,
				),
			})
		}
	}

	// Value-based checks: weakness and length.
	// We use the cache to avoid extra network calls; values are already loaded.
	p.cacheMu.RLock()
	val, ok := p.cache[cacheKeyFor(path, key)]
	p.cacheMu.RUnlock()

	if !ok {
		return
	}

	if len(val) < 16 {
		*findings = append(*findings, HealthFinding{
			Path:     path,
			Check:    "short",
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("secret at %q (key: %q) is shorter than 16 characters", path, key),
		})
	}

	valLower := strings.ToLower(val)
	for _, pattern := range p.cfg.Health.WeakPatterns {
		if strings.Contains(valLower, strings.ToLower(pattern)) {
			*findings = append(*findings, HealthFinding{
				Path:     path,
				Check:    "weak",
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("secret at %q (key: %q) matches a known weak pattern", path, key),
			})
			break
		}
	}
}

// emitEvent emits a structured domain event via the EventLogger port.
// Errors are logged but never propagated — event emission is best-effort.
func (p *Plugin) emitEvent(ctx context.Context, eventType, aiSummary string, payload map[string]any) {
	if p.eventLog == nil {
		return
	}
	ev := events.Event{
		SchemaVersion: events.SchemaVersion,
		EventType:     eventType,
		Timestamp:     time.Now().UTC(),
		AISummary:     aiSummary,
		Payload:       payload,
	}
	if err := p.eventLog.Log(ctx, ev); err != nil {
		p.logger.WarnContext(ctx, "secrets plugin: failed to emit event",
			slog.String("event_type", eventType),
			slog.String("error", err.Error()),
		)
	}
}

// cacheKeyFor returns the cache key for a given path and key combination.
func cacheKeyFor(path, key string) string {
	return path + "/" + key
}

// Interface guards — compile-time verification that Plugin implements the required interfaces.
var (
	_ ports.Plugin           = (*Plugin)(nil)
	_ ports.CaddyContributor = (*Plugin)(nil)
	_ ports.PluginMeta       = (*Plugin)(nil)
)
