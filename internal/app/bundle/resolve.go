package bundle

import (
	"fmt"
	"os"
	"strings"

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

// ResolveProdUpstream patches upstream.host in a YAML map for production
// Docker networking. This operates on a map[string]any (parsed from raw YAML)
// rather than a Config struct, so field names are preserved exactly as written
// in the user's YAML file.
//
// The rules mirror ResolveProdConfig: loopback/wildcard addresses are replaced
// with the Docker container name for multi-site, or "app" for single-site when
// a container is configured.
func ResolveProdUpstream(m map[string]any, projectName string, multiSite bool) {
	upstream, ok := m["upstream"].(map[string]any)
	if !ok {
		return
	}

	host, _ := upstream["host"].(string)
	if !isLocalUpstreamHost(host) {
		return
	}

	if multiSite {
		upstream["host"] = appContainerName(projectName)
	} else {
		app, _ := m["app"].(map[string]any)
		if app != nil {
			image, _ := app["image"].(string)
			build, _ := app["build"].(string)
			if image != "" || build != "" {
				upstream["host"] = "app"
			}
		}
	}
}

// ResolveProdAppBuild removes app.build from a production config map when
// app.image is set. A bundle is a production artifact: the app image is either
// supplied by the user (registry reference) or built locally by `vibew build`
// and shipped as image.tar, so a build context has no meaning on the remote
// host. Keeping both keys made the bundled vibewarden.yaml ambiguous about
// which field wins (#1341).
//
// When app.image is absent or empty the map is left untouched — app.build is
// then the only description of how the image is produced.
func ResolveProdAppBuild(m map[string]any) {
	app, ok := m["app"].(map[string]any)
	if !ok {
		return
	}

	image, _ := app["image"].(string)
	if strings.TrimSpace(image) == "" {
		return
	}

	delete(app, "build")
}

// MarshalYAMLMap serialises a map[string]any to YAML bytes. This is used
// instead of marshalling the Config struct directly, because Config only has
// mapstructure tags (not yaml tags). Marshalling via the map preserves the
// original YAML field names (e.g. rate_limit, security_headers).
func MarshalYAMLMap(m map[string]any) ([]byte, error) {
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling YAML map: %w", err)
	}
	return data, nil
}

// MergeConfigYAML deep-merges overrideYAML on top of baseYAML. Both inputs are
// raw YAML bytes. The merge is recursive: mapping keys in the override replace
// or extend keys in the base; non-mapping values in the override replace the
// base value entirely. The result is a merged map suitable for further patching
// and marshalling.
//
// When overrideYAML is nil or empty the base is returned as-is.
func MergeConfigYAML(baseYAML, overrideYAML []byte) (map[string]any, error) {
	var base map[string]any
	if err := yaml.Unmarshal(baseYAML, &base); err != nil {
		return nil, fmt.Errorf("parsing base YAML: %w", err)
	}
	if base == nil {
		base = make(map[string]any)
	}

	if len(overrideYAML) == 0 {
		return base, nil
	}

	var override map[string]any
	if err := yaml.Unmarshal(overrideYAML, &override); err != nil {
		return nil, fmt.Errorf("parsing override YAML: %w", err)
	}

	deepMerge(base, override)
	return base, nil
}

// PatchYAMLMap sets a nested value in m at the given key path. Intermediate
// maps are created as needed. For example, PatchYAMLMap(m, []string{"upstream",
// "host"}, "app") is equivalent to m["upstream"]["host"] = "app".
func PatchYAMLMap(m map[string]any, path []string, value any) {
	if len(path) == 0 {
		return
	}

	current := m
	for _, key := range path[:len(path)-1] {
		next, ok := current[key]
		if !ok {
			child := make(map[string]any)
			current[key] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			current[key] = child
		}
		current = child
	}
	current[path[len(path)-1]] = value
}

// deepMerge recursively merges src into dst. For keys that exist in both and
// whose values are both maps, the merge is recursive. Otherwise the src value
// overwrites the dst value.
func deepMerge(dst, src map[string]any) {
	for key, srcVal := range src {
		dstVal, exists := dst[key]
		if !exists {
			dst[key] = srcVal
			continue
		}

		srcMap, srcIsMap := srcVal.(map[string]any)
		dstMap, dstIsMap := dstVal.(map[string]any)
		if srcIsMap && dstIsMap {
			deepMerge(dstMap, srcMap)
		} else {
			dst[key] = srcVal
		}
	}
}

// LoadMergedConfig loads the base vibewarden.yaml at configPath, deep-merges
// the production override at prodConfigPath on top of it, and returns the
// resulting *config.Config.
//
// The merge routes through MergeConfigYAML — the same deep-merge used to
// produce the on-disk bundle YAML — so every schema field set in the override
// reaches the returned struct (including newer fields like tls.email,
// tls.acme_ca, and any future plugin key). This replaces the previous
// hand-written field allow-list that silently dropped unknown fields
// (ADR-082, #1053).
//
// When prodConfigPath is empty, configPath is loaded as-is via config.Load
// (with defaults and env-var overrides applied). When configPath is also empty
// (or the file does not exist), config.Load falls back to defaults plus
// environment variables, matching its standard behaviour.
//
// The merged YAML is written to a tempfile inside os.TempDir() and removed
// before return. This is acceptable for a one-shot deploy/bundle command and
// is never called from the serve hot path.
func LoadMergedConfig(configPath, prodConfigPath string) (*config.Config, error) {
	if prodConfigPath == "" {
		return config.Load(configPath)
	}

	var baseYAML []byte
	if configPath != "" {
		data, err := os.ReadFile(configPath) //nolint:gosec // configPath is the vibewarden.yaml resolved by the caller
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading base config %s: %w", configPath, err)
		}
		baseYAML = data
	}

	overrideYAML, err := os.ReadFile(prodConfigPath) //nolint:gosec // prodConfigPath is the production override resolved by the caller
	if err != nil {
		return nil, fmt.Errorf("reading production config %s: %w", prodConfigPath, err)
	}

	merged, err := MergeConfigYAML(baseYAML, overrideYAML)
	if err != nil {
		return nil, fmt.Errorf("merging config YAML: %w", err)
	}

	mergedYAML, err := MarshalYAMLMap(merged)
	if err != nil {
		return nil, fmt.Errorf("marshalling merged config: %w", err)
	}

	tmp, err := os.CreateTemp("", "vibewarden-merged-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("creating temp file for merged config: %w", err)
	}
	tmpPath := tmp.Name()
	// Ensure the tempfile is always removed, even on error paths.
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(mergedYAML); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("writing merged config to %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("closing merged config %s: %w", tmpPath, err)
	}

	cfg, err := config.Load(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("loading merged config: %w", err)
	}
	return cfg, nil
}
