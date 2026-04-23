package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestNewWrapCmd_WrapperScripts(t *testing.T) {
	wrapperFiles := []string{"vibew", "vibew.ps1", "vibew.cmd"}

	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		checkFiles  []string
		absentFiles []string
	}{
		{
			name:       "default wrap generates wrapper scripts",
			args:       []string{},
			checkFiles: wrapperFiles,
			// .vibewarden-version must never be generated.
			absentFiles: []string{".vibewarden-version"},
		},
		{
			name:        "skip-wrapper omits wrapper files",
			args:        []string{"--skip-wrapper"},
			absentFiles: append(wrapperFiles, ".vibewarden-version"),
		},
		{
			name:       "force flag overwrites existing wrapper files",
			args:       []string{"--force"},
			checkFiles: wrapperFiles,
			// .vibewarden-version must never be generated even with --force.
			absentFiles: []string{".vibewarden-version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := scaffoldTestDir(t, false)

			root := cmd.NewRootCmd("test")
			allArgs := append([]string{"wrap", dir}, tt.args...)
			root.SetArgs(allArgs)

			err := root.Execute()

			if (err != nil) != tt.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			for _, filename := range tt.checkFiles {
				path := filepath.Join(dir, filename)
				if _, statErr := os.Stat(path); statErr != nil {
					t.Errorf("expected file %q to exist: %v", path, statErr)
				}
			}

			for _, filename := range tt.absentFiles {
				path := filepath.Join(dir, filename)
				if _, statErr := os.Stat(path); statErr == nil {
					t.Errorf("file %q should not exist but does", path)
				}
			}
		})
	}
}

func TestNewWrapCmd_VibewIsExecutable(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"wrap", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	vibewPath := filepath.Join(dir, "vibew")
	info, err := os.Stat(vibewPath)
	if err != nil {
		t.Fatalf("vibew not found: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("vibew should be executable, mode = %v", info.Mode())
	}
}

// TestNewWrapCmd_NoVersionFlag verifies that --version flag is absent from wrap.
func TestNewWrapCmd_NoVersionFlag(t *testing.T) {
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"wrap", "--help"})
	var out strings.Builder
	root.SetOut(&out)

	_ = root.Execute()
	if strings.Contains(out.String(), "--version") {
		t.Errorf("--version flag must not appear in wrap --help output:\n%s", out.String())
	}
}

// TestNewWrapCmd_NoVersionFileCreated verifies that running wrap does not
// produce a .vibewarden-version file on disk.
func TestNewWrapCmd_NoVersionFileCreated(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"wrap", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".vibewarden-version")); err == nil {
		t.Error(".vibewarden-version must not be created by wrap but it exists")
	}
}

func TestNewWrapCmd_VibewScriptContainsKeyPatterns(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		patterns []string
		absent   []string
	}{
		{
			name: "vibew shell script has required patterns",
			file: "vibew",
			patterns: []string{
				"#!/bin/sh",
				"vibewarden/vibewarden",
				"sha256",
				".vibewarden/bin",
				"exec",
			},
			absent: []string{"VERSION_FILE", ".vibewarden-version"},
		},
		{
			name: "vibew.ps1 has required patterns",
			file: "vibew.ps1",
			patterns: []string{
				"vibewarden/vibewarden",
				"SHA256",
				".vibewarden",
				"LASTEXITCODE",
			},
			absent: []string{"$VersionFile", ".vibewarden-version"},
		},
		{
			name: "vibew.cmd has required patterns",
			file: "vibew.cmd",
			patterns: []string{
				"@echo off",
				"vibew.ps1",
				"ERRORLEVEL",
			},
		},
	}

	dir := scaffoldTestDir(t, false)
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"wrap", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatalf("reading %s: %v", tt.file, err)
			}
			for _, pattern := range tt.patterns {
				if !strings.Contains(string(content), pattern) {
					t.Errorf("%s does not contain %q", tt.file, pattern)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(string(content), absent) {
					t.Errorf("%s must not contain %q", tt.file, absent)
				}
			}
		})
	}
}

func TestNewWrapCmd_SuccessMessageListsWrapperFiles(t *testing.T) {
	dir := scaffoldTestDir(t, false)

	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"wrap", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"vibew", "vibew.ps1"} {
		if !strings.Contains(out, want) {
			t.Errorf("success message does not mention %q\n\nOutput:\n%s", want, out)
		}
	}
	// .vibewarden-version must not appear in the success message.
	if strings.Contains(out, ".vibewarden-version") {
		t.Errorf("success message must not mention .vibewarden-version\n\nOutput:\n%s", out)
	}
}
