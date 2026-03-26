package plugins_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/plugins"
)

func TestCatalog_ContainsAllExpectedPlugins(t *testing.T) {
	wantNames := []string{
		"tls",
		"security-headers",
		"rate-limiting",
		"auth",
		"metrics",
		"user-management",
	}

	nameSet := make(map[string]bool, len(plugins.Catalog))
	for _, d := range plugins.Catalog {
		nameSet[d.Name] = true
	}

	for _, name := range wantNames {
		if !nameSet[name] {
			t.Errorf("plugin %q not found in Catalog", name)
		}
	}
}

func TestCatalog_EachDescriptorIsComplete(t *testing.T) {
	for _, d := range plugins.Catalog {
		t.Run(d.Name, func(t *testing.T) {
			if d.Name == "" {
				t.Error("Name must not be empty")
			}
			if d.Description == "" {
				t.Errorf("plugin %q has empty Description", d.Name)
			}
			if len(d.ConfigSchema) == 0 {
				t.Errorf("plugin %q has empty ConfigSchema", d.Name)
			}
			if d.Example == "" {
				t.Errorf("plugin %q has empty Example", d.Name)
			}
			// Every ConfigSchema entry must have a non-empty key and value.
			for k, v := range d.ConfigSchema {
				if k == "" {
					t.Errorf("plugin %q has empty ConfigSchema key", d.Name)
				}
				if v == "" {
					t.Errorf("plugin %q has empty description for key %q", d.Name, k)
				}
			}
		})
	}
}

func TestFindDescriptor_Found(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"tls"},
		{"security-headers"},
		{"rate-limiting"},
		{"auth"},
		{"metrics"},
		{"user-management"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, ok := plugins.FindDescriptor(tt.name)
			if !ok {
				t.Errorf("FindDescriptor(%q) = false, want true", tt.name)
			}
			if d.Name != tt.name {
				t.Errorf("FindDescriptor(%q).Name = %q, want %q", tt.name, d.Name, tt.name)
			}
		})
	}
}

func TestFindDescriptor_NotFound(t *testing.T) {
	tests := []struct {
		name string
	}{
		{"nonexistent"},
		{""},
		{"TLS"},
		{"Rate-Limiting"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := plugins.FindDescriptor(tt.name)
			if ok {
				t.Errorf("FindDescriptor(%q) = true, want false", tt.name)
			}
		})
	}
}
