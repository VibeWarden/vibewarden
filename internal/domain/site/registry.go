package site

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe aggregate that manages a collection of Sites
// and an optional GlobalConfig. It enforces the invariant that site names
// are unique within the registry.
type Registry struct {
	mu     sync.RWMutex
	sites  map[string]Site
	global *GlobalConfig
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		sites: make(map[string]Site),
	}
}

// SetGlobal stores the global configuration in the registry.
func (r *Registry) SetGlobal(g GlobalConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.global = &g
}

// Global returns the stored global configuration, or nil if none has been set.
func (r *Registry) Global() *GlobalConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.global == nil {
		return nil
	}
	// Return a copy so callers cannot mutate registry state.
	g := *r.global
	return &g
}

// Add inserts a site into the registry. If a site with the same name already
// exists, it is replaced (upsert semantics, useful for hot-reload).
func (r *Registry) Add(s Site) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sites[s.Name()] = s
}

// Remove deletes a site by name. Returns true if the site existed.
func (r *Registry) Remove(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, existed := r.sites[name]
	delete(r.sites, name)
	return existed
}

// Get retrieves a site by name. Returns the site and true if found,
// or a zero-value Site and false if not.
func (r *Registry) Get(name string) (Site, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sites[name]
	return s, ok
}

// All returns a snapshot of all sites in the registry. The returned slice
// is safe to iterate without holding any lock.
func (r *Registry) All() []Site {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Site, 0, len(r.sites))
	for _, s := range r.sites {
		result = append(result, s)
	}
	return result
}

// Len returns the number of sites in the registry.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sites)
}

// HealthySites returns all sites with StatusHealthy.
func (r *Registry) HealthySites() []Site {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Site
	for _, s := range r.sites {
		if s.Status() == StatusHealthy {
			result = append(result, s)
		}
	}
	return result
}

// ErrorSites returns all sites with StatusError.
func (r *Registry) ErrorSites() []Site {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Site
	for _, s := range r.sites {
		if s.Status() == StatusError {
			result = append(result, s)
		}
	}
	return result
}

// DuplicateDomainError is returned when two sites claim the same TLS domain.
type DuplicateDomainError struct {
	Domain string
	SiteA  string
	SiteB  string
}

// Error implements the error interface.
func (e *DuplicateDomainError) Error() string {
	return fmt.Sprintf("duplicate domain %q: claimed by sites %q and %q", e.Domain, e.SiteA, e.SiteB)
}

// ValidateDomains checks that no two healthy sites in the registry claim the
// same TLS domain. Returns nil when all domains are unique.
func (r *Registry) ValidateDomains() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]string) // domain -> site name
	for _, s := range r.sites {
		if s.Status() != StatusHealthy || s.Config() == nil {
			continue
		}
		domain := s.Config().TLS.Domain
		if domain == "" {
			continue
		}
		if existing, ok := seen[domain]; ok {
			return &DuplicateDomainError{
				Domain: domain,
				SiteA:  existing,
				SiteB:  s.Name(),
			}
		}
		seen[domain] = s.Name()
	}
	return nil
}
