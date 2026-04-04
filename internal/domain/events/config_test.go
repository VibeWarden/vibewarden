package events_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/domain/events"
)

func TestNewConfigReloaded(t *testing.T) {
	tests := []struct {
		name   string
		params events.ConfigReloadedParams
	}{
		{
			name: "file watcher source",
			params: events.ConfigReloadedParams{
				ConfigPath:    "/etc/vibewarden/vibewarden.yaml",
				TriggerSource: "file_watcher",
				DurationMS:    42,
			},
		},
		{
			name: "admin api source",
			params: events.ConfigReloadedParams{
				ConfigPath:    "vibewarden.yaml",
				TriggerSource: "admin_api",
				DurationMS:    100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewConfigReloaded(tt.params)

			if ev.EventType != events.EventTypeConfigReloaded {
				t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeConfigReloaded)
			}
			if ev.SchemaVersion != events.SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
			}
			if ev.AISummary == "" {
				t.Error("AISummary should not be empty")
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp should not be zero")
			}
			if ev.Payload["config_path"] != tt.params.ConfigPath {
				t.Errorf("config_path = %v, want %q", ev.Payload["config_path"], tt.params.ConfigPath)
			}
			if ev.Payload["trigger_source"] != tt.params.TriggerSource {
				t.Errorf("trigger_source = %v, want %q", ev.Payload["trigger_source"], tt.params.TriggerSource)
			}
			if ev.Payload["duration_ms"] != tt.params.DurationMS {
				t.Errorf("duration_ms = %v, want %d", ev.Payload["duration_ms"], tt.params.DurationMS)
			}
		})
	}
}

func TestNewConfigReloadFailed(t *testing.T) {
	tests := []struct {
		name   string
		params events.ConfigReloadFailedParams
	}{
		{
			name: "validation failure with errors",
			params: events.ConfigReloadFailedParams{
				ConfigPath:       "vibewarden.yaml",
				TriggerSource:    "file_watcher",
				Reason:           "config validation failed",
				ValidationErrors: []string{"rate_limit.per_ip.requests_per_second must be positive"},
			},
		},
		{
			name: "parse error with no validation errors",
			params: events.ConfigReloadFailedParams{
				ConfigPath:       "vibewarden.yaml",
				TriggerSource:    "admin_api",
				Reason:           "yaml: line 5: mapping values are not allowed here",
				ValidationErrors: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := events.NewConfigReloadFailed(tt.params)

			if ev.EventType != events.EventTypeConfigReloadFailed {
				t.Errorf("EventType = %q, want %q", ev.EventType, events.EventTypeConfigReloadFailed)
			}
			if ev.SchemaVersion != events.SchemaVersion {
				t.Errorf("SchemaVersion = %q, want %q", ev.SchemaVersion, events.SchemaVersion)
			}
			if ev.AISummary == "" {
				t.Error("AISummary should not be empty")
			}
			if ev.Timestamp.IsZero() {
				t.Error("Timestamp should not be zero")
			}
			if ev.Payload["config_path"] != tt.params.ConfigPath {
				t.Errorf("config_path = %v, want %q", ev.Payload["config_path"], tt.params.ConfigPath)
			}
			if ev.Payload["trigger_source"] != tt.params.TriggerSource {
				t.Errorf("trigger_source = %v, want %q", ev.Payload["trigger_source"], tt.params.TriggerSource)
			}
			if ev.Payload["reason"] != tt.params.Reason {
				t.Errorf("reason = %v, want %q", ev.Payload["reason"], tt.params.Reason)
			}

			errs, ok := ev.Payload["validation_errors"].([]string)
			if !ok {
				t.Fatalf("validation_errors is not []string, got %T", ev.Payload["validation_errors"])
			}
			wantLen := len(tt.params.ValidationErrors)
			if len(errs) != wantLen {
				t.Errorf("len(validation_errors) = %d, want %d", len(errs), wantLen)
			}
		})
	}
}
