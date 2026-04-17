package site

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/vibewarden/vibewarden/internal/config"
)

// ValidNamePattern defines the DNS-safe name constraint for site names.
// Names must be 1-63 characters, lowercase alphanumeric plus hyphens,
// and must not start or end with a hyphen.
var ValidNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Site is a domain entity representing a single application deployment
// managed by VibeWarden. Each Site has an identity (Name), a complete
// per-app configuration, and an operational status.
//
// The Site holds a *config.Config — this is a documented pragmatic
// boundary violation (ADR-068) that avoids ~500 lines of type
// duplication between the domain and config layers.
type Site struct {
	name       string
	configPath string
	cfg        *config.Config
	status     Status
	err        error
}

// NewSite creates a healthy Site with the given name and configuration.
// The name must match ValidNamePattern (DNS-safe: lowercase alphanumeric
// and hyphens, 1-63 chars, no leading/trailing hyphen). The config must
// not be nil.
func NewSite(name string, configPath string, cfg *config.Config) (*Site, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, errors.New("site config must not be nil")
	}
	return &Site{
		name:       name,
		configPath: configPath,
		cfg:        cfg,
		status:     StatusHealthy,
	}, nil
}

// NewErrorSite creates a Site in the Error state. This is used when
// a site's configuration file exists but failed to load or validate.
// The name must still be valid so the site can be identified in logs
// and status output.
func NewErrorSite(name string, configPath string, err error) (*Site, error) {
	if verr := validateName(name); verr != nil {
		return nil, verr
	}
	if err == nil {
		return nil, errors.New("error site must have a non-nil error")
	}
	return &Site{
		name:       name,
		configPath: configPath,
		status:     StatusError,
		err:        err,
	}, nil
}

// Name returns the site's unique identifier, derived from its directory name.
func (s *Site) Name() string {
	return s.name
}

// ConfigPath returns the filesystem path to the site's vibewarden.yaml.
// Needed by the directory watcher (#873) for hot-reload.
func (s *Site) ConfigPath() string {
	return s.configPath
}

// Config returns the site's loaded configuration.
// Returns nil for error sites.
func (s *Site) Config() *config.Config {
	return s.cfg
}

// Status returns the site's current operational status.
func (s *Site) Status() Status {
	return s.status
}

// Err returns the error that caused an Error status, or nil for healthy sites.
func (s *Site) Err() error {
	return s.err
}

// IsHealthy reports whether the site is in the Healthy state.
func (s *Site) IsHealthy() bool {
	return s.status == StatusHealthy
}

// SetStatus updates the site's operational status. When setting StatusError,
// the caller should also call SetErr with the underlying error.
func (s *Site) SetStatus(status Status) {
	s.status = status
}

// SetErr records an error and transitions the site to StatusError.
func (s *Site) SetErr(err error) {
	s.err = err
	s.status = StatusError
}

// validateName checks that a site name matches the DNS-safe pattern.
func validateName(name string) error {
	if name == "" {
		return errors.New("site name must not be empty")
	}
	if len(name) > 63 {
		return fmt.Errorf("site name %q exceeds 63 characters", name)
	}
	if !ValidNamePattern.MatchString(name) {
		return fmt.Errorf("site name %q is not DNS-safe (must match %s)", name, ValidNamePattern.String())
	}
	return nil
}
