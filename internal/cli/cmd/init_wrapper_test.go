package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestNewInitCmd_WrapperScripts(t *testing.T) {
	wrapperFiles := []string{"vibew", "vibew.ps1", "vibew.cmd", ".vibewarden-version"}

	tests := []struct {
		name        string
		args        []string
		wantErr     bool
		checkFiles  []string
		absentFiles []string
	}{
		{
			name:       "default init generates wrapper scripts",
			args:       []string{"--skip-docker"},
			checkFiles: wrapperFiles,
		},
		{
			name:        "skip-wrapper omits wrapper files",
			args:        []string{"--skip-docker", "--skip-wrapper"},
			absentFiles: wrapperFiles,
		},
		{
			name:       "version flag written to .vibewarden-version",
			args:       []string{"--skip-docker", "--version", "v1.2.3"},
			checkFiles: wrapperFiles,
		},
		{
			name: "force flag overwrites existing wrapper files",
			args: []string{"--skip-docker", "--force"},
			checkFiles: wrapperFiles,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			root := cmd.NewRootCmd("test")
			allArgs := append([]string{"init", dir}, tt.args...)
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

func TestNewInitCmd_VibewIsExecutable(t *testing.T) {
	dir := t.TempDir()

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", dir, "--skip-docker"})

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

func TestNewInitCmd_VersionFilePinnedContent(t *testing.T) {
	dir := t.TempDir()

	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", dir, "--skip-docker", "--version", "v0.5.0"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".vibewarden-version"))
	if err != nil {
		t.Fatalf(".vibewarden-version not found: %v", err)
	}
	if strings.TrimSpace(string(content)) != "v0.5.0" {
		t.Errorf(".vibewarden-version content = %q, want %q", string(content), "v0.5.0")
	}
}

func TestNewInitCmd_VibewScriptContainsKeyPatterns(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		patterns []string
	}{
		{
			name: "vibew shell script has required patterns",
			file: "vibew",
			patterns: []string{
				"#!/bin/sh",
				"vibewarden/vibewarden",
				"sha256",
				"~/.vibewarden/bin",
				"exec",
			},
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

	dir := t.TempDir()
	root := cmd.NewRootCmd("test")
	root.SetArgs([]string{"init", dir, "--skip-docker"})
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
		})
	}
}

func TestNewInitCmd_SuccessMessageListsWrapperFiles(t *testing.T) {
	dir := t.TempDir()

	root := cmd.NewRootCmd("test")
	var outBuf strings.Builder
	root.SetOut(&outBuf)
	root.SetArgs([]string{"init", dir, "--skip-docker"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	for _, want := range []string{"vibew", "vibew.ps1", "vibew.cmd", ".vibewarden-version"} {
		if !strings.Contains(out, want) {
			t.Errorf("success message does not mention %q\n\nOutput:\n%s", want, out)
		}
	}
}
