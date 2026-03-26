package ports

import "github.com/vibewarden/vibewarden/internal/cli/scaffold"

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
