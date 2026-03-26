package ports

import (
	"context"

	"github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// TemplateRenderer renders Go text/template templates to bytes or files.
// Implementations embed template files via embed.FS.
type TemplateRenderer interface {
	// Render executes the named template with the supplied data and returns
	// the rendered bytes.
	Render(templateName string, data any) ([]byte, error)

	// RenderToFile renders the named template and writes the result to path.
	// Parent directories are created when they do not exist.
	// Returns an error when the file already exists and overwrite is false.
	RenderToFile(templateName string, data any, path string, overwrite bool) error
}

// ProjectDetector analyses a directory and returns detected project state.
type ProjectDetector interface {
	// Detect inspects dir and returns the detected ProjectConfig.
	Detect(dir string) (*scaffold.ProjectConfig, error)
}

// FeatureToggler reads and modifies a vibewarden.yaml file to enable or
// inspect feature flags. Implementations must preserve YAML comments and
// existing formatting when writing.
type FeatureToggler interface {
	// ReadFeatures parses path and returns the current feature state.
	ReadFeatures(ctx context.Context, path string) (*scaffold.FeatureState, error)

	// EnableFeature enables the named feature in the file at path, applying
	// opts as feature-specific options. The file is written back atomically.
	// Returns scaffold.ErrFeatureAlreadyEnabled when the feature is already on.
	EnableFeature(ctx context.Context, path string, feature scaffold.Feature, opts scaffold.FeatureOptions) error
}
