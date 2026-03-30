package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config"
)

func TestBuildLogger_Level(t *testing.T) {
	tests := []struct {
		name      string
		cfg       config.LogConfig
		wantLevel slog.Level
	}{
		{
			name:      "debug level",
			cfg:       config.LogConfig{Level: "debug", Format: "json"},
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "info level",
			cfg:       config.LogConfig{Level: "info", Format: "json"},
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "warn level",
			cfg:       config.LogConfig{Level: "warn", Format: "json"},
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "error level",
			cfg:       config.LogConfig{Level: "error", Format: "json"},
			wantLevel: slog.LevelError,
		},
		{
			name:      "unknown level defaults to info",
			cfg:       config.LogConfig{Level: "verbose", Format: "json"},
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "empty level defaults to info",
			cfg:       config.LogConfig{Level: "", Format: "json"},
			wantLevel: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := buildLogger(tt.cfg)
			if logger == nil {
				t.Fatal("buildLogger() returned nil logger")
			}
			if !logger.Enabled(context.TODO(), tt.wantLevel) {
				t.Errorf("buildLogger(%q) logger does not enable level %v", tt.cfg.Level, tt.wantLevel)
			}
			// Verify that one level below is disabled (except for debug which has nothing below)
			if tt.wantLevel > slog.LevelDebug {
				lowerLevel := tt.wantLevel - 4
				if logger.Enabled(context.TODO(), lowerLevel) {
					t.Errorf("buildLogger(%q) unexpectedly enables level %v (below minimum %v)", tt.cfg.Level, lowerLevel, tt.wantLevel)
				}
			}
		})
	}
}

func TestBuildLogger_Format(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.LogConfig
		wantNil bool
	}{
		{
			name:    "json format returns a logger",
			cfg:     config.LogConfig{Level: "info", Format: "json"},
			wantNil: false,
		},
		{
			name:    "text format returns a logger",
			cfg:     config.LogConfig{Level: "info", Format: "text"},
			wantNil: false,
		},
		{
			name:    "unknown format falls back to json and returns a logger",
			cfg:     config.LogConfig{Level: "info", Format: "unknown"},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := buildLogger(tt.cfg)
			if (logger == nil) != tt.wantNil {
				t.Errorf("buildLogger() = %v, wantNil %v", logger, tt.wantNil)
			}
		})
	}
}

func TestRunServe_MissingConfig(t *testing.T) {
	// runServe should return an error when given a path to a non-existent config file
	// that is not a standard search path (explicit path forces a load attempt).
	err := runServe("/nonexistent/path/to/vibewarden.yaml")
	if err == nil {
		t.Error("runServe() expected error for missing explicit config file, got nil")
	}
}
