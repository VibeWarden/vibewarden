package main

import (
	"context"
	"testing"
)

func TestRunServe_MissingConfig(t *testing.T) {
	// runServe should return an error when given a path to a non-existent config
	// file that is not a standard search path (explicit path forces a load attempt).
	err := runServe(context.Background(), serveOptions{
		configPath: "/nonexistent/path/to/vibewarden.yaml",
		version:    "test",
	})
	if err == nil {
		t.Error("runServe() expected error for missing explicit config file, got nil")
	}
}
