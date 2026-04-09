package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultConfigName = "vibewarden.yaml"

// scaffoldingMarker is the file created by `vibew init` and `vibew wrap` that
// signals the current directory has been properly initialised. Its presence is
// used as a proxy for "the user ran init/wrap before running dev/generate/deploy".
const scaffoldingMarker = "AGENTS-VIBEWARDEN.md"

// requireConfig checks that the config file exists at configPath (or at
// ./vibewarden.yaml when configPath is empty) and returns a descriptive error
// directing the user to run `vibew init` or `vibew wrap` when the file is
// missing.
//
// Call this at the top of any command's RunE that requires an initialised
// project before doing any other work.
func requireConfig(configPath string) error {
	path := configPath
	if path == "" {
		path = defaultConfigName
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("vibewarden.yaml not found\n\nRun one of:\n  vibew init --name myapp --lang go    # new project\n  vibew wrap --upstream 3000           # existing project\n\nThese commands generate the required config, Dockerfile, and agent context files")
	}

	return nil
}

// RequireScaffolding checks that AGENTS-VIBEWARDEN.md exists in dir (pass "."
// to check the current working directory). This file is only created by
// `vibew init` or `vibew wrap`, so its absence means the user (or an AI agent)
// skipped the scaffolding step and is trying to run a command that depends on it.
//
// Call this at the top of any command's RunE that requires a fully scaffolded
// project — before calling requireConfig or any other logic.
//
// RequireScaffolding is exported so that tests can invoke it directly with a
// temp-dir path, avoiding any dependency on the process working directory.
func RequireScaffolding(dir string) error {
	path := filepath.Join(dir, scaffoldingMarker)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("vibewarden not initialized in this directory\n\nRun one of:\n  vibew init --name myapp --lang go    # new project\n  vibew wrap --upstream 3000           # existing project\n\nThese commands generate the required config, Dockerfile, and agent context files")
	}
	return nil
}

// requireScaffolding is the unexported wrapper used by command RunE handlers.
// It always checks the current working directory.
func requireScaffolding() error {
	return RequireScaffolding(".")
}
