// Package template provides a ports.TemplateRenderer implementation that uses
// Go's text/template package with templates embedded via embed.FS.
package template

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"
)

// File permission constants.
const (
	// permDir is the permission mode for directories created during rendering.
	permDir = os.FileMode(0o750)
	// permConfig is the permission mode for rendered config/template output files.
	permConfig = os.FileMode(0o600)
)

// Renderer implements ports.TemplateRenderer using an embed.FS.
type Renderer struct {
	fs fs.ReadFileFS
}

// NewRenderer creates a Renderer that reads templates from the supplied FS.
// The FS must be an fs.ReadFileFS (embed.FS satisfies this interface).
func NewRenderer(f fs.ReadFileFS) *Renderer {
	return &Renderer{fs: f}
}

// Render executes the named template with data and returns the rendered bytes.
// templateName must be the filename as stored in the FS (e.g. "vibewarden.yaml.tmpl").
func (r *Renderer) Render(templateName string, data any) ([]byte, error) {
	src, err := r.fs.ReadFile(templateName)
	if err != nil {
		return nil, fmt.Errorf("reading template %q: %w", templateName, err)
	}

	tmpl, err := template.New(templateName).Parse(string(src))
	if err != nil {
		return nil, fmt.Errorf("parsing template %q: %w", templateName, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("executing template %q: %w", templateName, err)
	}

	return buf.Bytes(), nil
}

// RenderToFile renders templateName with data and writes the output to path.
// All parent directories of path are created if they do not exist.
// When overwrite is false and path already exists, os.ErrExist is returned.
func (r *Renderer) RenderToFile(templateName string, data any, path string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("file already exists at %q: %w", path, os.ErrExist)
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("checking file %q: %w", path, err)
		}
	}

	rendered, err := r.Render(templateName, data)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), permDir); err != nil {
		return fmt.Errorf("creating directories for %q: %w", path, err)
	}

	if err := os.WriteFile(path, rendered, permConfig); err != nil {
		return fmt.Errorf("writing file %q: %w", path, err)
	}

	return nil
}
