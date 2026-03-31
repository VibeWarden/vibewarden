package main

import (
	"context"
	"testing"

	appserve "github.com/vibewarden/vibewarden/internal/app/serve"
)

func TestRunServe_MissingConfig(t *testing.T) {
	// RunServe should return an error when given a path to a non-existent config
	// file that is not a standard search path (explicit path forces a load attempt).
	err := appserve.RunServe(context.Background(), appserve.Options{
		ConfigPath: "/nonexistent/path/to/vibewarden.yaml",
		Version:    "test",
	})
	if err == nil {
		t.Error("RunServe() expected error for missing explicit config file, got nil")
	}
}
