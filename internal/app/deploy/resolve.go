package deploy

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/vibewarden/vibewarden/internal/config"
)

// ResolveProdConfig returns a shallow copy of cfg with upstream.host resolved
// for production deployment inside Docker networking.
//
// For multi-site mode: if upstream.host is a loopback or wildcard address
// (0.0.0.0, 127.0.0.1, localhost), it is replaced with the Docker container
// name (vibewarden-<projectName>-app).
//
// For single-site mode: if upstream.host is a loopback or wildcard address and
// the app runs in a container (app.image or app.build is set), it is replaced
// with "app" (the Docker Compose service name in the single-site compose).
//
// All other config values are left unchanged. The returned config can be
// marshalled to YAML and deployed as-is with no further patching.
func ResolveProdConfig(cfg *config.Config, projectName string, multiSite bool) *config.Config {
	// Shallow copy the config so the caller's original is not mutated.
	resolved := *cfg
	resolved.Upstream = cfg.Upstream

	if !isLocalUpstreamHost(resolved.Upstream.Host) {
		return &resolved
	}

	if multiSite {
		resolved.Upstream.Host = appContainerName(projectName)
	} else if cfg.App.Image != "" || cfg.App.Build != "" {
		// Single-site: the docker-compose.yml template names the app service "app".
		resolved.Upstream.Host = "app"
	}

	return &resolved
}

// MarshalConfig serialises a Config to YAML bytes suitable for writing to the
// deploy bundle. The output is a valid vibewarden.yaml that can be read back
// with config.Load.
func MarshalConfig(cfg *config.Config) ([]byte, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshalling config to YAML: %w", err)
	}
	return data, nil
}
