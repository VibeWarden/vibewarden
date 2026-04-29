package ops

import (
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"
)

// composeFileServices is the minimal YAML shape we parse from a
// docker-compose.yml — only the top-level services: map is needed.
type composeFileServices struct {
	Services map[string]yaml.Node `yaml:"services"`
}

// ServicesFromComposeFile reads the docker-compose.yml at path and returns the
// sorted list of service names declared under the top-level services: key.
// An error is returned when the file cannot be read or its YAML is invalid.
func ServicesFromComposeFile(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is a caller-supplied compose file path, not user input
	if err != nil {
		return nil, fmt.Errorf("reading compose file: %w", err)
	}

	var cf composeFileServices
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing compose file: %w", err)
	}

	names := make([]string, 0, len(cf.Services))
	for name := range cf.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
