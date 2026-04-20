package templates_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	templateadapter "github.com/vibewarden/vibewarden/internal/adapters/template"
	"github.com/vibewarden/vibewarden/internal/cli/templates"
	"github.com/vibewarden/vibewarden/internal/config"
	domainscaffold "github.com/vibewarden/vibewarden/internal/domain/scaffold"
)

// TestYAMLTemplates_StrictLoadable verifies that the fully-rendered output of
// every user-facing vibewarden.yaml template parses cleanly against the
// current schema under config.LoadStrict. This is the regression guard for
// issue #1053 (docs-runtime drift): a commented example referencing a
// non-existent key, like overrides.docker_compose, must not ship in the
// template because the next thing a vibe coder does after `vibew init` is
// uncomment it.
//
// Test strategy: for each template we render it with representative data,
// then render it again after un-commenting the escape-hatch example blocks
// (any line starting with "# <key>:" inside a `# overrides:` block). Both
// renderings must satisfy LoadStrict.
func TestYAMLTemplates_StrictLoadable(t *testing.T) {
	renderer := templateadapter.NewRenderer(templates.FS)

	initData := domainscaffold.InitProjectData{
		ProjectName: "myapp",
		Port:        3000,
		Name:        "myapp",
	}
	wrapData := domainscaffold.TemplateData{
		UpstreamPort:     3000,
		AuthEnabled:      true,
		RateLimitEnabled: true,
		TLSEnabled:       true,
		TLSDomain:        "example.com",
		ProjectName:      "myapp",
	}

	tests := []struct {
		name     string
		template string
		data     any
	}{
		{name: "init-vibewarden.yaml.tmpl", template: "init-vibewarden.yaml.tmpl", data: initData},
		{name: "vibewarden.yaml.tmpl", template: "vibewarden.yaml.tmpl", data: wrapData},
	}

	for _, tt := range tests {
		t.Run(tt.name+"/as-rendered", func(t *testing.T) {
			rendered, err := renderer.Render(tt.template, tt.data)
			if err != nil {
				t.Fatalf("Render(%s) error = %v", tt.template, err)
			}
			strictLoad(t, tt.template, string(rendered))
		})

		t.Run(tt.name+"/escape-hatches-uncommented", func(t *testing.T) {
			rendered, err := renderer.Render(tt.template, tt.data)
			if err != nil {
				t.Fatalf("Render(%s) error = %v", tt.template, err)
			}
			uncommented := uncommentEscapeHatchBlock(string(rendered))
			if !strings.Contains(uncommented, "overrides:") {
				t.Skipf("template %s does not contain an escape-hatch block; nothing to check", tt.template)
			}
			strictLoad(t, tt.template, uncommented)
		})
	}
}

// strictLoad writes content to a temp file and runs config.LoadStrict against
// it. A non-nil error or an error that unwraps to *config.UnknownKeyError
// fails the test. The label is used in error messages so the offending
// template is easy to identify.
func strictLoad(t *testing.T, label, content string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vibewarden.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing rendered %s: %v", label, err)
	}
	if _, err := config.LoadStrict(path, ""); err != nil {
		t.Errorf("LoadStrict rejected rendered %s: %v\n\n--- rendered content ---\n%s", label, err, content)
	}
}

// uncommentEscapeHatchBlock rewrites a template region that begins with
// `# overrides:` (followed by `#   <key>: <value>` lines) into active YAML.
// Everything else is returned verbatim. This simulates the first thing a
// new user does: uncomment the commented-out example to try it.
func uncommentEscapeHatchBlock(content string) string {
	var (
		out        strings.Builder
		inBlock    bool
		leadRe     = regexp.MustCompile(`^# (\S+:.*)$`)
		childRe    = regexp.MustCompile(`^#(\s{2,}\S.*)$`)
		blankCmtRe = regexp.MustCompile(`^#\s*$`)
	)
	for _, line := range strings.Split(content, "\n") {
		switch {
		case !inBlock && line == "# overrides:":
			out.WriteString("overrides:\n")
			inBlock = true
		case inBlock && childRe.MatchString(line):
			out.WriteString(childRe.ReplaceAllString(line, "$1"))
			out.WriteByte('\n')
		case inBlock && blankCmtRe.MatchString(line):
			// Blank `#` line inside block — treat as end of block.
			inBlock = false
			out.WriteString(line)
			out.WriteByte('\n')
		case inBlock && !leadRe.MatchString(line) && !strings.HasPrefix(line, "#"):
			// First non-comment line ends the block.
			inBlock = false
			out.WriteString(line)
			out.WriteByte('\n')
		default:
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	// strings.Split produces a trailing empty element for trailing \n; trim one.
	result := out.String()
	return strings.TrimSuffix(result, "\n")
}
