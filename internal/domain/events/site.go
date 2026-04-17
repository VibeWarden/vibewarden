package events

import (
	"fmt"
	"time"
)

// Site event type constants.
const (
	// EventTypeSiteAdded is emitted when a new site is discovered and
	// successfully loaded into the registry.
	EventTypeSiteAdded = "site.added"

	// EventTypeSiteUpdated is emitted when an existing site's configuration
	// is reloaded after a file change.
	EventTypeSiteUpdated = "site.updated"

	// EventTypeSiteRemoved is emitted when a site's configuration file is
	// deleted and the site is removed from the registry.
	EventTypeSiteRemoved = "site.removed"

	// EventTypeSiteLoadFailed is emitted when a site's configuration cannot
	// be loaded or validated. The site is marked as errored but healthy
	// sites are not disrupted.
	EventTypeSiteLoadFailed = "site.load_failed"
)

// SiteAddedParams holds the parameters for a site.added event.
type SiteAddedParams struct {
	// SiteName is the DNS-safe directory name of the site.
	SiteName string

	// ConfigPath is the absolute path to the site's vibewarden.yaml.
	ConfigPath string

	// Domain is the TLS domain configured for the site, if any.
	Domain string
}

// NewSiteAdded creates a site.added event.
func NewSiteAdded(params SiteAddedParams) Event {
	summary := fmt.Sprintf("Site %q added from %s", params.SiteName, params.ConfigPath)
	if params.Domain != "" {
		summary = fmt.Sprintf("Site %q (domain: %s) added from %s", params.SiteName, params.Domain, params.ConfigPath)
	}
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeSiteAdded,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryAudit,
		AISummary:     summary,
		Payload: map[string]any{
			"site_name":   params.SiteName,
			"config_path": params.ConfigPath,
			"domain":      params.Domain,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeAllowed,
		TriggeredBy: "site_watcher",
	}
}

// SiteUpdatedParams holds the parameters for a site.updated event.
type SiteUpdatedParams struct {
	// SiteName is the DNS-safe directory name of the site.
	SiteName string

	// ConfigPath is the absolute path to the site's vibewarden.yaml.
	ConfigPath string

	// Domain is the TLS domain configured for the site, if any.
	Domain string
}

// NewSiteUpdated creates a site.updated event.
func NewSiteUpdated(params SiteUpdatedParams) Event {
	summary := fmt.Sprintf("Site %q config updated from %s", params.SiteName, params.ConfigPath)
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeSiteUpdated,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryAudit,
		AISummary:     summary,
		Payload: map[string]any{
			"site_name":   params.SiteName,
			"config_path": params.ConfigPath,
			"domain":      params.Domain,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeAllowed,
		TriggeredBy: "site_watcher",
	}
}

// SiteRemovedParams holds the parameters for a site.removed event.
type SiteRemovedParams struct {
	// SiteName is the DNS-safe directory name of the site.
	SiteName string

	// ConfigPath is the absolute path to the site's vibewarden.yaml.
	ConfigPath string
}

// NewSiteRemoved creates a site.removed event.
func NewSiteRemoved(params SiteRemovedParams) Event {
	summary := fmt.Sprintf("Site %q removed (config: %s)", params.SiteName, params.ConfigPath)
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeSiteRemoved,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryAudit,
		AISummary:     summary,
		Payload: map[string]any{
			"site_name":   params.SiteName,
			"config_path": params.ConfigPath,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeAllowed,
		TriggeredBy: "site_watcher",
	}
}

// SiteLoadFailedParams holds the parameters for a site.load_failed event.
type SiteLoadFailedParams struct {
	// SiteName is the DNS-safe directory name of the site.
	SiteName string

	// ConfigPath is the absolute path to the site's vibewarden.yaml.
	ConfigPath string

	// Reason is a human-readable description of the failure.
	Reason string
}

// NewSiteLoadFailed creates a site.load_failed event.
func NewSiteLoadFailed(params SiteLoadFailedParams) Event {
	summary := fmt.Sprintf("Site %q load failed: %s", params.SiteName, params.Reason)
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeSiteLoadFailed,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryAudit,
		AISummary:     summary,
		Payload: map[string]any{
			"site_name":   params.SiteName,
			"config_path": params.ConfigPath,
			"reason":      params.Reason,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeFailed,
		TriggeredBy: "site_watcher",
	}
}
