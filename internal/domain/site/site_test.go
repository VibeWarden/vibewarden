package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		// Valid names
		{"simple lowercase", "myapp", false},
		{"with hyphens", "my-cool-app", false},
		{"single char", "a", false},
		{"single digit", "1", false},
		{"starts with digit", "123app", false},
		{"ends with digit", "app123", false},
		{"max length 63", strings.Repeat("a", 63), false},
		{"two chars", "ab", false},
		{"hyphen in middle", "a-b", false},
		{"digits and hyphens", "app-1-v2", false},

		// Invalid names
		{"empty", "", true},
		{"exceeds 63 chars", strings.Repeat("a", 64), true},
		{"uppercase", "MyApp", true},
		{"leading hyphen", "-myapp", true},
		{"trailing hyphen", "myapp-", true},
		{"underscore", "my_app", true},
		{"dot", "my.app", true},
		{"space", "my app", true},
		{"only hyphen", "-", true},
		{"double hyphen only", "--", true},
		{"leading and trailing hyphen", "-app-", true},
		{"uppercase mixed", "myApp", true},
		{"special chars", "my@app", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestNewSite(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}

	tests := []struct {
		name     string
		siteName string
		cfg      *config.Config
		wantErr  bool
	}{
		{"valid site", "my-app", cfg, false},
		{"nil config", "my-app", nil, true},
		{"invalid name", "My-App", cfg, true},
		{"empty name", "", cfg, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewSite(tt.siteName, "", tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSite(%q, cfg) error = %v, wantErr %v", tt.siteName, err, tt.wantErr)
				return
			}
			if err == nil {
				if s.Name() != tt.siteName {
					t.Errorf("Name() = %q, want %q", s.Name(), tt.siteName)
				}
				if s.Config() != tt.cfg {
					t.Error("Config() does not match input config")
				}
				if s.Status() != StatusHealthy {
					t.Errorf("Status() = %v, want StatusHealthy", s.Status())
				}
				if s.Err() != nil {
					t.Errorf("Err() = %v, want nil", s.Err())
				}
				if !s.IsHealthy() {
					t.Error("IsHealthy() = false, want true")
				}
			}
		})
	}
}

func TestNewErrorSite(t *testing.T) {
	t.Parallel()

	loadErr := errors.New("invalid YAML")

	tests := []struct {
		name     string
		siteName string
		err      error
		wantErr  bool
	}{
		{"valid error site", "broken-app", loadErr, false},
		{"nil error", "broken-app", nil, true},
		{"invalid name", "Broken-App", loadErr, true},
		{"empty name", "", loadErr, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, constructErr := NewErrorSite(tt.siteName, "", tt.err)
			if (constructErr != nil) != tt.wantErr {
				t.Errorf("NewErrorSite(%q, err) error = %v, wantErr %v", tt.siteName, constructErr, tt.wantErr)
				return
			}
			if constructErr == nil {
				if s.Name() != tt.siteName {
					t.Errorf("Name() = %q, want %q", s.Name(), tt.siteName)
				}
				if s.Config() != nil {
					t.Error("Config() should be nil for error site")
				}
				if s.Status() != StatusError {
					t.Errorf("Status() = %v, want StatusError", s.Status())
				}
				if s.Err() != tt.err {
					t.Errorf("Err() = %v, want %v", s.Err(), tt.err)
				}
				if s.IsHealthy() {
					t.Error("IsHealthy() = true, want false")
				}
			}
		})
	}
}

func TestSite_SetStatus(t *testing.T) {
	t.Parallel()

	s, err := NewSite("my-app", "", &config.Config{})
	if err != nil {
		t.Fatalf("NewSite() error = %v", err)
	}

	s.SetStatus(StatusDegraded)
	if s.Status() != StatusDegraded {
		t.Errorf("Status() = %v, want StatusDegraded", s.Status())
	}
}

func TestSite_SetErr(t *testing.T) {
	t.Parallel()

	s, err := NewSite("my-app", "", &config.Config{})
	if err != nil {
		t.Fatalf("NewSite() error = %v", err)
	}

	siteErr := errors.New("something broke")
	s.SetErr(siteErr)

	if s.Status() != StatusError {
		t.Errorf("Status() = %v, want StatusError after SetErr", s.Status())
	}
	if s.Err() != siteErr {
		t.Errorf("Err() = %v, want %v", s.Err(), siteErr)
	}
}
