// Package site provides the domain model for multi-app site management.
// A Site represents a single application deployment managed by VibeWarden,
// each with its own configuration, domain, upstream, and status.
//
// This package intentionally imports internal/config as a pragmatic boundary
// violation documented in ADR-068, to avoid ~500 lines of type duplication.
package site

// SiteStatus represents the operational state of a managed site.
type SiteStatus int

const (
	// StatusHealthy indicates the site's configuration loaded successfully
	// and the site is ready to serve traffic.
	StatusHealthy SiteStatus = iota

	// StatusError indicates the site's configuration failed to load or
	// validate. The site cannot serve traffic.
	StatusError

	// StatusDegraded indicates the site is partially functional — for
	// example, the config loaded but a downstream dependency is unhealthy.
	StatusDegraded
)

// String returns a human-readable name for the status.
func (s SiteStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusError:
		return "error"
	case StatusDegraded:
		return "degraded"
	default:
		return "unknown"
	}
}
