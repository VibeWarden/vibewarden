package template_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
)

// mapFS wraps fstest.MapFS so it satisfies the fs.ReadFileFS interface
// expected by templateadapter.NewRenderer.
type mapFS struct{ m fstest.MapFS }

func (f mapFS) ReadFile(name string) ([]byte, error) { return f.m.ReadFile(name) }
func (f mapFS) Open(name string) (fs.File, error)    { return f.m.Open(name) }

func newMapFS(files map[string]string) mapFS {
	m := make(fstest.MapFS)
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return mapFS{m}
}

func TestRenderer_Render(t *testing.T) {
	tests := []struct {
		name         string
		templateName string
		tmplContent  string
		data         any
		wantContains string
		wantErr      bool
	}{
		{
			name:         "simple substitution",
			templateName: "hello.tmpl",
			tmplContent:  "Hello, {{ .Name }}!",
			data:         struct{ Name string }{"World"},
			wantContains: "Hello, World!",
		},
		{
			name:         "boolean conditional true",
			templateName: "cond.tmpl",
			tmplContent:  "{{- if .Enabled }}on{{- else }}off{{- end }}",
			data:         struct{ Enabled bool }{true},
			wantContains: "on",
		},
		{
			name:         "boolean conditional false",
			templateName: "cond.tmpl",
			tmplContent:  "{{- if .Enabled }}on{{- else }}off{{- end }}",
			data:         struct{ Enabled bool }{false},
			wantContains: "off",
		},
		{
			name:         "template not found returns error",
			templateName: "missing.tmpl",
			data:         nil,
			wantErr:      true,
		},
		{
			name:         "invalid template syntax returns error",
			templateName: "bad.tmpl",
			tmplContent:  "{{ .Unclosed",
			data:         nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{}
			if tt.tmplContent != "" {
				files[tt.templateName] = tt.tmplContent
			}
			r := templateadapter.NewRenderer(newMapFS(files))

			got, err := r.Render(tt.templateName, tt.data)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Render() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantContains != "" && !strings.Contains(string(got), tt.wantContains) {
				t.Errorf("Render() output %q does not contain %q", string(got), tt.wantContains)
			}
		})
	}
}

func TestRenderer_RenderToFile(t *testing.T) {
	const tmplName = "out.tmpl"
	const tmplContent = "port: {{ .Port }}"

	data := struct{ Port int }{3000}

	t.Run("writes file successfully", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.yaml")

		r := templateadapter.NewRenderer(newMapFS(map[string]string{tmplName: tmplContent}))
		if err := r.RenderToFile(tmplName, data, path, false); err != nil {
			t.Fatalf("RenderToFile() unexpected error: %v", err)
		}

		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading output file: %v", err)
		}
		if string(got) != "port: 3000" {
			t.Errorf("file content = %q, want %q", string(got), "port: 3000")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "sub", "dir", "out.yaml")

		r := templateadapter.NewRenderer(newMapFS(map[string]string{tmplName: tmplContent}))
		if err := r.RenderToFile(tmplName, data, path, false); err != nil {
			t.Fatalf("RenderToFile() unexpected error: %v", err)
		}

		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file at %q: %v", path, err)
		}
	})

	t.Run("returns ErrExist when file exists and overwrite false", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.yaml")

		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := templateadapter.NewRenderer(newMapFS(map[string]string{tmplName: tmplContent}))
		err := r.RenderToFile(tmplName, data, path, false)
		if !errors.Is(err, os.ErrExist) {
			t.Errorf("RenderToFile() error = %v, want os.ErrExist", err)
		}
	})

	t.Run("overwrites when overwrite true", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "out.yaml")

		if err := os.WriteFile(path, []byte("old content"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := templateadapter.NewRenderer(newMapFS(map[string]string{tmplName: tmplContent}))
		if err := r.RenderToFile(tmplName, data, path, true); err != nil {
			t.Fatalf("RenderToFile() unexpected error: %v", err)
		}

		got, _ := os.ReadFile(path)
		if string(got) != "port: 3000" {
			t.Errorf("file content = %q, want %q", string(got), "port: 3000")
		}
	})
}
