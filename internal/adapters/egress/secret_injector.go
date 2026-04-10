// Package egress implements the HTTP listener and request forwarding adapter
// for the egress proxy plugin.
package egress

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	domainegress "github.com/vibewarden/vibewarden/internal/domain/egress"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultSecretTTL is how long a resolved secret value is cached before
	// being re-fetched from the secret store.
	defaultSecretTTL = 5 * time.Minute

	// secretValuePlaceholder is the template placeholder replaced with the
	// resolved secret value when formatting the header value.
	secretValuePlaceholder = "{value}"

	// headerInjectSecret is the request header an application sends to
	// dynamically request a secret by name on a per-request basis.
	// It is always stripped before the request is forwarded upstream.
	headerInjectSecret = "X-Inject-Secret"
)

// cachedSecret holds a resolved secret value and the time it expires.
type cachedSecret struct {
	value     string
	expiresAt time.Time
}

// SecretInjectorConfig holds the configuration for a SecretInjector.
type SecretInjectorConfig struct {
	// TTL is how long a resolved secret value is cached. Defaults to 5 minutes.
	TTL time.Duration
}

// SecretInjector resolves secret values from a SecretStore, caches them with a
// configurable TTL, formats the value using the route's format template, and
// returns the header name and formatted value ready for injection.
//
// SecretInjector implements ports.SecretInjector.
type SecretInjector struct {
	store ports.SecretStore
	ttl   time.Duration

	mu    sync.Mutex
	cache map[string]cachedSecret
}

// NewSecretInjector creates a new SecretInjector backed by store.
// Pass a zero-value SecretInjectorConfig to use defaults.
func NewSecretInjector(store ports.SecretStore, cfg SecretInjectorConfig) *SecretInjector {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultSecretTTL
	}
	return &SecretInjector{
		store: store,
		ttl:   ttl,
		cache: make(map[string]cachedSecret),
	}
}

// Inject implements ports.SecretInjector.
//
// It fetches the secret at cfg.Name from the backing store (using a cached
// value when one is still fresh), formats it by replacing "{value}" in
// cfg.Format with the secret value, and returns the header name (cfg.Header)
// and the formatted value.
//
// When cfg.Format is empty, the raw secret value is returned without
// modification.
//
// The resolved secret value is never written to any log or error message.
func (s *SecretInjector) Inject(ctx context.Context, cfg domainegress.SecretConfig) (header, value string, err error) {
	raw, err := s.resolve(ctx, cfg.Name)
	if err != nil {
		// Return a safe error that does not include the secret value.
		return "", "", fmt.Errorf("egress secret injection: resolving %q: %w", cfg.Name, err)
	}

	formatted := formatSecretValue(cfg.Format, raw)
	return cfg.Header, formatted, nil
}

// resolve returns the secret value for name, using the in-memory cache when
// the entry is still fresh. On a cache miss (or expiry) it calls the store.
func (s *SecretInjector) resolve(ctx context.Context, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entry, ok := s.cache[name]; ok && time.Now().Before(entry.expiresAt) {
		return entry.value, nil
	}

	data, err := s.store.Get(ctx, name)
	if err != nil {
		return "", fmt.Errorf("fetching secret: %w", err)
	}

	// Convention: the injected value is stored under the "value" key.
	// If that key is absent but the map has exactly one entry, use that entry.
	raw, ok := data["value"]
	if !ok {
		for _, v := range data {
			raw = v
			ok = true
			break
		}
	}
	if !ok {
		return "", fmt.Errorf("secret %q has no usable value key", name)
	}

	s.cache[name] = cachedSecret{
		value:     raw,
		expiresAt: time.Now().Add(s.ttl),
	}
	return raw, nil
}

// formatSecretValue replaces "{value}" in format with val.
// When format is empty, val is returned unchanged.
func formatSecretValue(format, val string) string {
	if format == "" {
		return val
	}
	return strings.ReplaceAll(format, secretValuePlaceholder, val)
}

// Interface guard — SecretInjector must implement ports.SecretInjector.
var _ ports.SecretInjector = (*SecretInjector)(nil)
