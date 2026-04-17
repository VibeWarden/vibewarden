package site

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestSiteRegistry_AddAndGet(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()

	s, err := NewSite("app1", &config.Config{})
	if err != nil {
		t.Fatalf("NewSite() error = %v", err)
	}

	r.Add(s)

	got, ok := r.Get("app1")
	if !ok {
		t.Fatal("Get(app1) returned false, want true")
	}
	if got.Name() != "app1" {
		t.Errorf("Name() = %q, want %q", got.Name(), "app1")
	}
}

func TestSiteRegistry_GetMissing(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get(nonexistent) returned true, want false")
	}
}

func TestSiteRegistry_AddReplaces(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()

	cfg1 := &config.Config{Profile: "dev"}
	s1, _ := NewSite("app1", cfg1)
	r.Add(s1)

	cfg2 := &config.Config{Profile: "prod"}
	s2, _ := NewSite("app1", cfg2)
	r.Add(s2)

	if r.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after upsert", r.Len())
	}

	got, _ := r.Get("app1")
	if got.Config().Profile != "prod" {
		t.Errorf("Config().Profile = %q, want %q after upsert", got.Config().Profile, "prod")
	}
}

func TestSiteRegistry_Remove(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	s, _ := NewSite("app1", &config.Config{})
	r.Add(s)

	if !r.Remove("app1") {
		t.Error("Remove(app1) returned false, want true")
	}
	if r.Len() != 0 {
		t.Errorf("Len() = %d after Remove, want 0", r.Len())
	}
}

func TestSiteRegistry_RemoveMissing(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	if r.Remove("nonexistent") {
		t.Error("Remove(nonexistent) returned true, want false")
	}
}

func TestSiteRegistry_All(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	s1, _ := NewSite("app1", &config.Config{})
	s2, _ := NewSite("app2", &config.Config{})
	r.Add(s1)
	r.Add(s2)

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d sites, want 2", len(all))
	}

	names := map[string]bool{}
	for _, s := range all {
		names[s.Name()] = true
	}
	if !names["app1"] || !names["app2"] {
		t.Errorf("All() names = %v, want app1 and app2", names)
	}
}

func TestSiteRegistry_HealthySitesAndErrorSites(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	healthy, _ := NewSite("good-app", &config.Config{})
	errSite, _ := NewErrorSite("bad-app", fmt.Errorf("broken"))
	r.Add(healthy)
	r.Add(errSite)

	hs := r.HealthySites()
	if len(hs) != 1 || hs[0].Name() != "good-app" {
		t.Errorf("HealthySites() = %v, want [good-app]", siteNames(hs))
	}

	es := r.ErrorSites()
	if len(es) != 1 || es[0].Name() != "bad-app" {
		t.Errorf("ErrorSites() = %v, want [bad-app]", siteNames(es))
	}
}

func TestSiteRegistry_GlobalConfig(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()

	// Initially nil.
	if g := r.Global(); g != nil {
		t.Errorf("Global() = %v, want nil before SetGlobal", g)
	}

	gc := DefaultGlobalConfig()
	gc.AdminToken = "my-token"
	r.SetGlobal(gc)

	got := r.Global()
	if got == nil {
		t.Fatal("Global() = nil after SetGlobal, want non-nil")
	}
	if got.AdminToken != "my-token" {
		t.Errorf("Global().AdminToken = %q, want %q", got.AdminToken, "my-token")
	}

	// Verify the returned value is a copy.
	got.AdminToken = "mutated"
	got2 := r.Global()
	if got2.AdminToken != "my-token" {
		t.Error("Global() returned a reference, not a copy — mutation leaked")
	}
}

func TestSiteRegistry_ValidateDomains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*SiteRegistry)
		wantErr bool
	}{
		{
			name:    "empty registry",
			setup:   func(_ *SiteRegistry) {},
			wantErr: false,
		},
		{
			name: "unique domains",
			setup: func(r *SiteRegistry) {
				s1, _ := NewSite("app1", &config.Config{TLS: config.TLSConfig{Domain: "app1.example.com"}})
				s2, _ := NewSite("app2", &config.Config{TLS: config.TLSConfig{Domain: "app2.example.com"}})
				r.Add(s1)
				r.Add(s2)
			},
			wantErr: false,
		},
		{
			name: "duplicate domains",
			setup: func(r *SiteRegistry) {
				s1, _ := NewSite("app1", &config.Config{TLS: config.TLSConfig{Domain: "same.example.com"}})
				s2, _ := NewSite("app2", &config.Config{TLS: config.TLSConfig{Domain: "same.example.com"}})
				r.Add(s1)
				r.Add(s2)
			},
			wantErr: true,
		},
		{
			name: "error sites are ignored",
			setup: func(r *SiteRegistry) {
				s1, _ := NewSite("app1", &config.Config{TLS: config.TLSConfig{Domain: "same.example.com"}})
				s2, _ := NewErrorSite("app2", fmt.Errorf("broken"))
				r.Add(s1)
				r.Add(s2)
			},
			wantErr: false,
		},
		{
			name: "empty domains are ignored",
			setup: func(r *SiteRegistry) {
				s1, _ := NewSite("app1", &config.Config{})
				s2, _ := NewSite("app2", &config.Config{})
				r.Add(s1)
				r.Add(s2)
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewSiteRegistry()
			tt.setup(r)
			err := r.ValidateDomains()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomains() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSiteRegistry_DuplicateDomainError(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	s1, _ := NewSite("aaa", &config.Config{TLS: config.TLSConfig{Domain: "dup.example.com"}})
	s2, _ := NewSite("bbb", &config.Config{TLS: config.TLSConfig{Domain: "dup.example.com"}})
	r.Add(s1)
	r.Add(s2)

	err := r.ValidateDomains()
	if err == nil {
		t.Fatal("ValidateDomains() = nil, want DuplicateDomainError")
	}

	var dupErr *DuplicateDomainError
	if !errors.As(err, &dupErr) {
		t.Fatalf("error is %T, want *DuplicateDomainError", err)
	}
	if dupErr.Domain != "dup.example.com" {
		t.Errorf("Domain = %q, want %q", dupErr.Domain, "dup.example.com")
	}
}

// TestSiteRegistry_ConcurrentAccess verifies that the registry is safe for
// concurrent reads and writes. This test uses multiple goroutines hammering
// Add, Remove, Get, All, and Len simultaneously.
func TestSiteRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	r := NewSiteRegistry()
	const numGoroutines = 50
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 5) // 5 operation types

	// Writers: Add
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range numOps {
				name := fmt.Sprintf("app-%d-%d", id, j)
				s, err := NewSite(name, &config.Config{})
				if err != nil {
					t.Errorf("NewSite(%q) error = %v", name, err)
					return
				}
				r.Add(s)
			}
		}(i)
	}

	// Writers: Remove
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range numOps {
				name := fmt.Sprintf("app-%d-%d", id, j)
				r.Remove(name)
			}
		}(i)
	}

	// Readers: Get
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			for j := range numOps {
				name := fmt.Sprintf("app-%d-%d", id, j)
				r.Get(name)
			}
		}(i)
	}

	// Readers: All
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range numOps {
				r.All()
			}
		}()
	}

	// Readers: Len
	for range numGoroutines {
		go func() {
			defer wg.Done()
			for range numOps {
				r.Len()
			}
		}()
	}

	wg.Wait()
}

func siteNames(sites []Site) []string {
	names := make([]string, len(sites))
	for i, s := range sites {
		names[i] = s.Name()
	}
	return names
}
