package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewSiteAdded(t *testing.T) {
	tests := []struct {
		name   string
		params events.SiteAddedParams
	}{
		{
			name: "with domain",
			params: events.SiteAddedParams{
				SiteName:   "app1",
				ConfigPath: "/srv/sites/app1/vibewarden.yaml",
				Domain:     "app1.example.com",
			},
		},
		{
			name: "without domain",
			params: events.SiteAddedParams{
				SiteName:   "app2",
				ConfigPath: "/srv/sites/app2/vibewarden.yaml",
				Domain:     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewSiteAdded(tt.params)

			assertEvent(t, ev, events.EventTypeSiteAdded)
			requireSummaryContains(t, ev.AISummary, tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "site_name", tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "config_path", tt.params.ConfigPath)
			requirePayloadString(t, ev.Payload, "domain", tt.params.Domain)

			if ev.Actor.Type != events.ActorTypeSystem {
				t.Errorf("Actor.Type = %q, want %q", ev.Actor.Type, events.ActorTypeSystem)
			}
			if ev.Resource.Type != events.ResourceTypeConfig {
				t.Errorf("Resource.Type = %q, want %q", ev.Resource.Type, events.ResourceTypeConfig)
			}
			if ev.Outcome != events.OutcomeAllowed {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
			}
			if ev.TriggeredBy != "site_watcher" {
				t.Errorf("TriggeredBy = %q, want %q", ev.TriggeredBy, "site_watcher")
			}
		})
	}
}

func TestNewSiteUpdated(t *testing.T) {
	tests := []struct {
		name   string
		params events.SiteUpdatedParams
	}{
		{
			name: "standard update",
			params: events.SiteUpdatedParams{
				SiteName:   "my-app",
				ConfigPath: "/srv/sites/my-app/vibewarden.yaml",
				Domain:     "my-app.example.com",
			},
		},
		{
			name: "update without domain",
			params: events.SiteUpdatedParams{
				SiteName:   "backend",
				ConfigPath: "/srv/sites/backend/vibewarden.yaml",
				Domain:     "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewSiteUpdated(tt.params)

			assertEvent(t, ev, events.EventTypeSiteUpdated)
			requireSummaryContains(t, ev.AISummary, tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "site_name", tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "config_path", tt.params.ConfigPath)
			requirePayloadString(t, ev.Payload, "domain", tt.params.Domain)

			if ev.Outcome != events.OutcomeAllowed {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
			}
			if ev.TriggeredBy != "site_watcher" {
				t.Errorf("TriggeredBy = %q, want %q", ev.TriggeredBy, "site_watcher")
			}
		})
	}
}

func TestNewSiteRemoved(t *testing.T) {
	tests := []struct {
		name   string
		params events.SiteRemovedParams
	}{
		{
			name: "standard removal",
			params: events.SiteRemovedParams{
				SiteName:   "old-app",
				ConfigPath: "/srv/sites/old-app/vibewarden.yaml",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewSiteRemoved(tt.params)

			assertEvent(t, ev, events.EventTypeSiteRemoved)
			requireSummaryContains(t, ev.AISummary, tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "site_name", tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "config_path", tt.params.ConfigPath)

			if ev.Outcome != events.OutcomeAllowed {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeAllowed)
			}
		})
	}
}

func TestNewSiteLoadFailed(t *testing.T) {
	tests := []struct {
		name   string
		params events.SiteLoadFailedParams
	}{
		{
			name: "parse error",
			params: events.SiteLoadFailedParams{
				SiteName:   "broken-app",
				ConfigPath: "/srv/sites/broken-app/vibewarden.yaml",
				Reason:     "yaml: line 5: mapping values are not allowed here",
			},
		},
		{
			name: "domain conflict",
			params: events.SiteLoadFailedParams{
				SiteName:   "dup-app",
				ConfigPath: "/srv/sites/dup-app/vibewarden.yaml",
				Reason:     "duplicate domain \"example.com\"",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewSiteLoadFailed(tt.params)

			assertEvent(t, ev, events.EventTypeSiteLoadFailed)
			requireSummaryContains(t, ev.AISummary, tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "site_name", tt.params.SiteName)
			requirePayloadString(t, ev.Payload, "config_path", tt.params.ConfigPath)
			requirePayloadString(t, ev.Payload, "reason", tt.params.Reason)

			if ev.Severity != events.SeverityMedium {
				t.Errorf("Severity = %q, want %q", ev.Severity, events.SeverityMedium)
			}
			if ev.Outcome != events.OutcomeFailed {
				t.Errorf("Outcome = %q, want %q", ev.Outcome, events.OutcomeFailed)
			}
		})
	}
}
