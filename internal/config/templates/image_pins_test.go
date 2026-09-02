package templates_test

import (
	"os"
	"regexp"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// devComposePath points at the repo-root docker-compose.yml used for local
// development. Go runs tests with the package directory as the working
// directory, so the path is relative to internal/config/templates.
const devComposePath = "../../../docker-compose.yml"

// composeTemplatePath is the docker-compose template rendered by `vibew init`
// and `vibew generate` — the artifact every new deployment ships with.
const composeTemplatePath = "docker-compose.yml.tmpl"

// openBaoImageRe captures the tag of a quay.io/openbao/openbao image pin.
var openBaoImageRe = regexp.MustCompile(`quay\.io/openbao/openbao:([0-9A-Za-z._-]+)`)

// openBaoTags returns every OpenBao image tag pinned in content. It fails the
// test if there are none, since a zero-match regex would make the drift
// assertions below vacuously true.
func openBaoTags(t *testing.T, source, content string) []string {
	t.Helper()

	matches := openBaoImageRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatalf("%s: no quay.io/openbao/openbao image pin found — has the image reference moved?", source)
	}

	tags := make([]string, 0, len(matches))
	for _, m := range matches {
		tags = append(tags, m[1])
	}
	return tags
}

// readTemplate returns the embedded docker-compose template as a string.
func readTemplate(t *testing.T) string {
	t.Helper()

	data, err := templates.FS.ReadFile(composeTemplatePath)
	if err != nil {
		t.Fatalf("reading %s: %v", composeTemplatePath, err)
	}
	return string(data)
}

// TestOpenBaoImagePin_TemplateIsInternallyConsistent verifies that every
// OpenBao image pin inside the generated compose template uses the same tag.
// The openbao and seed-secrets services must run identical binaries — the
// seed script talks to the server over the API and relies on matching CLI
// behaviour.
func TestOpenBaoImagePin_TemplateIsInternallyConsistent(t *testing.T) {
	tags := openBaoTags(t, composeTemplatePath, readTemplate(t))

	want := tags[0]
	for _, got := range tags[1:] {
		if got != want {
			t.Errorf("%s pins conflicting OpenBao tags %q and %q — all OpenBao services must share one tag",
				composeTemplatePath, want, got)
		}
	}
}

// TestOpenBaoImagePin_TemplateMatchesDevCompose verifies that the OpenBao tag
// shipped to users by `vibew init` matches the tag developers run locally.
// The two drifted apart once before (template stuck on 2.2.0 while the dev
// stack moved to 2.5.2), so users exercised a version nobody tested. See
// issue #1291.
func TestOpenBaoImagePin_TemplateMatchesDevCompose(t *testing.T) {
	devCompose, err := os.ReadFile(devComposePath)
	if err != nil {
		t.Fatalf("reading %s: %v", devComposePath, err)
	}

	devTags := openBaoTags(t, devComposePath, string(devCompose))
	tmplTags := openBaoTags(t, composeTemplatePath, readTemplate(t))

	if devTags[0] != tmplTags[0] {
		t.Errorf("OpenBao image pin drift: %s uses %q but %s uses %q — bump both together",
			devComposePath, devTags[0], composeTemplatePath, tmplTags[0])
	}
}
