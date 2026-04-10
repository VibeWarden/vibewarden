package events

import (
	"fmt"
	"time"
)

// ConfigReloadedParams holds the parameters needed to construct a
// config.reloaded event.
type ConfigReloadedParams struct {
	// ConfigPath is the path to the configuration file that was reloaded.
	ConfigPath string

	// TriggerSource identifies what initiated the reload: "file_watcher" or "admin_api".
	TriggerSource string

	// DurationMS is how long the reload took in milliseconds.
	DurationMS int64
}

// ConfigReloadFailedParams holds the parameters needed to construct a
// config.reload_failed event.
type ConfigReloadFailedParams struct {
	// ConfigPath is the path to the configuration file.
	ConfigPath string

	// TriggerSource identifies what initiated the reload attempt.
	TriggerSource string

	// Reason is a human-readable description of why the reload failed.
	Reason string

	// ValidationErrors is a list of specific validation errors, if applicable.
	ValidationErrors []string
}

// NewConfigReloaded creates a config.reloaded event indicating that the
// configuration was successfully reloaded from disk and applied to all
// components.
func NewConfigReloaded(params ConfigReloadedParams) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeConfigReloaded,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityInfo,
		Category:      CategoryAudit,
		AISummary: fmt.Sprintf(
			"Configuration reloaded from %s (source: %s) in %dms",
			params.ConfigPath, params.TriggerSource, params.DurationMS,
		),
		Payload: map[string]any{
			"config_path":    params.ConfigPath,
			"trigger_source": params.TriggerSource,
			"duration_ms":    params.DurationMS,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeAllowed,
		TriggeredBy: params.TriggerSource,
	}
}

// NewConfigReloadFailed creates a config.reload_failed event indicating that
// a configuration reload attempt failed. The old configuration remains active.
func NewConfigReloadFailed(params ConfigReloadFailedParams) Event {
	errs := params.ValidationErrors
	if errs == nil {
		errs = []string{}
	}
	return Event{
		SchemaVersion: SchemaVersion,
		EventType:     EventTypeConfigReloadFailed,
		Timestamp:     time.Now().UTC(),
		Severity:      SeverityMedium,
		Category:      CategoryAudit,
		AISummary: fmt.Sprintf(
			"Configuration reload failed for %s (source: %s): %s",
			params.ConfigPath, params.TriggerSource, params.Reason,
		),
		Payload: map[string]any{
			"config_path":       params.ConfigPath,
			"trigger_source":    params.TriggerSource,
			"reason":            params.Reason,
			"validation_errors": errs,
		},
		Actor:       Actor{Type: ActorTypeSystem},
		Resource:    Resource{Type: ResourceTypeConfig, Path: params.ConfigPath},
		Outcome:     OutcomeFailed,
		TriggeredBy: params.TriggerSource,
	}
}
