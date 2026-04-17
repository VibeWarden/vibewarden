// Package sites provides multi-site configuration loading for VibeWarden.
// It bridges the config layer (internal/config) and the site domain model
// (internal/domain/site), reading global.yaml and per-site vibewarden.yaml
// files from disk and constructing the corresponding domain types.
package sites

import (
	"fmt"
	"os"

	"github.com/vibewarden/vibewarden/internal/domain/site"

	"gopkg.in/yaml.v3"
)

// globalConfigYAML is the on-disk YAML shape for global.yaml.
// It maps 1:1 to the fields in site.GlobalConfig.
type globalConfigYAML struct {
	AdminToken string `yaml:"admin_token"`
	ListenHost string `yaml:"listen_host"`
	ListenPort int    `yaml:"listen_port"`
	LogLevel   string `yaml:"log_level"`
	ACMEEmail  string `yaml:"acme_email"`
}

// LoadGlobal reads a global.yaml file from the given path and returns a
// validated GlobalConfig. If the file does not exist, it returns
// DefaultGlobalConfig with no error (global.yaml is optional).
func LoadGlobal(path string) (*site.GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			g := site.DefaultGlobalConfig()
			return &g, nil
		}
		return nil, fmt.Errorf("reading global config %s: %w", path, err)
	}

	var raw globalConfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing global config %s: %w", path, err)
	}

	g := site.DefaultGlobalConfig()

	// Apply user-provided values over defaults.
	if raw.AdminToken != "" {
		g.AdminToken = raw.AdminToken
	}
	if raw.ListenHost != "" {
		g.ListenHost = raw.ListenHost
	}
	if raw.ListenPort != 0 {
		g.ListenPort = raw.ListenPort
	}
	if raw.LogLevel != "" {
		g.LogLevel = raw.LogLevel
	}
	if raw.ACMEEmail != "" {
		g.ACMEEmail = raw.ACMEEmail
	}

	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("validating global config %s: %w", path, err)
	}

	return &g, nil
}
