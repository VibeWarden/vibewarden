// Package scaffold provides filesystem-based adapters for project scaffolding.
package scaffold

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vibewarden/vibewarden/internal/cli/scaffold"
)

// Detector implements ports.ProjectDetector by inspecting the filesystem.
type Detector struct{}

// NewDetector creates a new Detector.
func NewDetector() *Detector {
	return &Detector{}
}

// Detect inspects dir and returns the detected ProjectConfig.
// It checks for well-known project indicator files and attempts to infer the
// upstream port from common patterns.
func (d *Detector) Detect(dir string) (*scaffold.ProjectConfig, error) {
	cfg := &scaffold.ProjectConfig{
		Type: scaffold.ProjectTypeUnknown,
	}

	// Detect project type from known indicator files.
	switch {
	case fileExists(filepath.Join(dir, "package.json")):
		cfg.Type = scaffold.ProjectTypeNode
		port, err := detectNodePort(filepath.Join(dir, "package.json"))
		if err == nil && port > 0 {
			cfg.DetectedPort = port
		}
	case fileExists(filepath.Join(dir, "go.mod")):
		cfg.Type = scaffold.ProjectTypeGo
	case fileExists(filepath.Join(dir, "requirements.txt")):
		cfg.Type = scaffold.ProjectTypePython
	}

	// Check for existing VibeWarden-managed files.
	cfg.HasDockerCompose = fileExists(filepath.Join(dir, "docker-compose.yml")) ||
		fileExists(filepath.Join(dir, "docker-compose.yaml"))
	cfg.HasVibeWardenConfig = fileExists(filepath.Join(dir, "vibewarden.yaml"))

	return cfg, nil
}

// detectNodePort tries to read the PORT from the package.json scripts section.
// It looks for patterns like "PORT=3000" in start/dev/serve scripts.
func detectNodePort(pkgPath string) (int, error) {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return 0, fmt.Errorf("reading package.json: %w", err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return 0, fmt.Errorf("parsing package.json: %w", err)
	}

	// Search common script names for a PORT= assignment.
	for _, scriptName := range []string{"start", "dev", "serve"} {
		script, ok := pkg.Scripts[scriptName]
		if !ok {
			continue
		}
		if port := extractPortFromScript(script); port > 0 {
			return port, nil
		}
	}

	return 0, nil
}

// extractPortFromScript searches a shell script string for a PORT=<number>
// assignment and returns the parsed port number, or 0 if none found.
func extractPortFromScript(script string) int {
	const prefix = "PORT="
	idx := strings.Index(script, prefix)
	if idx < 0 {
		return 0
	}
	rest := script[idx+len(prefix):]
	// Read digits until non-digit.
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	port, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return port
}

// fileExists returns true when path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
