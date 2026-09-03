package templates_test

import (
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/config/templates"
)

// devComposePath points at the repo-root docker-compose.yml used for local
// development. Go runs tests with the package directory as the working
// directory, so the path is relative to internal/config/templates.
//
// This file is the one Dependabot actually parses (docker ecosystem, root
// directory), which makes it the upstream-tracking reference for every image
// the templates also pin.
const devComposePath = "../../../docker-compose.yml"

// imagePinRe captures the name and tag of a `image: <name>:<tag>` line in a
// compose file or compose template. Templated references such as
// `image: {{ .SidecarImage }}` and `image: ${VIBEWARDEN_APP_IMAGE:-...}` do not
// match, since `{`, `}` and `$` are outside the name character class.
var imagePinRe = regexp.MustCompile(`(?m)^\s*image:\s*([A-Za-z0-9][A-Za-z0-9./_-]*):([A-Za-z0-9][A-Za-z0-9._-]*)\s*(?:#.*)?$`)

// unmonitoredTemplateImages lists images pinned in the templates that have no
// counterpart in the dev compose file, together with the reason. Nothing bumps
// these automatically — Dependabot cannot parse .tmpl files (issue #1298) — so
// they need a manual audit. Keep the list as short as possible: adding an image
// here is a decision to maintain it by hand.
var unmonitoredTemplateImages = map[string]string{
	"otel/opentelemetry-collector-contrib": "OTLP pipeline ships only in the generated stack; the dev stack scrapes Prometheus and Loki directly",
	"jaegertracing/jaeger":                 "trace UI ships only in the generated stack; the dev stack has no tracing backend",
}

// imagePins maps an image name to the tag it is pinned to. Every pin of the
// same image inside one source must agree, so a single tag per name is enough.
type imagePins map[string]string

// parseImagePins extracts the image pins from content. It reports an error for
// every image pinned to two different tags within the same source, and fails
// the test if content pins no image at all — a zero-match regex would make the
// drift assertions vacuously true.
func parseImagePins(t *testing.T, source, content string) imagePins {
	t.Helper()

	matches := imagePinRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		t.Fatalf("%s: no image pin found — has the image reference syntax moved?", source)
	}

	pins := make(imagePins, len(matches))
	for _, m := range matches {
		name, tag := m[1], m[2]
		if existing, ok := pins[name]; ok && existing != tag {
			t.Errorf("%s pins conflicting tags %q and %q for image %q — all services must share one tag",
				source, existing, tag, name)
			continue
		}
		pins[name] = tag
	}
	return pins
}

// templatePins returns the image pins found across every embedded *.tmpl file,
// keyed by image name. The template file each pin came from is returned
// alongside so failures can name it.
func templatePins(t *testing.T) (imagePins, map[string]string) {
	t.Helper()

	pins := imagePins{}
	origin := map[string]string{}

	err := fs.WalkDir(templates.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tmpl") {
			return nil
		}

		data, readErr := templates.FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range imagePinRe.FindAllStringSubmatch(string(data), -1) {
			name, tag := m[1], m[2]
			if existing, ok := pins[name]; ok && existing != tag {
				t.Errorf("templates pin conflicting tags %q (%s) and %q (%s) for image %q — all services must share one tag",
					existing, origin[name], tag, path, name)
				continue
			}
			pins[name] = tag
			origin[name] = path
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded templates: %v", err)
	}

	if len(pins) == 0 {
		t.Fatal("no image pin found in any embedded template — has the image reference syntax moved?")
	}
	return pins, origin
}

// devComposePins returns the image pins of the repo-root docker-compose.yml.
func devComposePins(t *testing.T) imagePins {
	t.Helper()

	data, err := os.ReadFile(devComposePath)
	if err != nil {
		t.Fatalf("reading %s: %v", devComposePath, err)
	}
	return parseImagePins(t, devComposePath, string(data))
}

// sortedNames returns the image names of pins in a stable order so failures
// read the same way on every run.
func sortedNames(pins imagePins) []string {
	names := make([]string, 0, len(pins))
	for name := range pins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestTemplateImagePins_MatchDevCompose verifies that every image pinned in a
// template uses the same tag as the dev compose file. Dependabot bumps the dev
// compose but cannot see .tmpl files (issue #1298), so this test is what makes
// a Dependabot PR fail until the templates are bumped with it. The two drifted
// apart before: the template stayed on OpenBao 2.2.0 while the dev stack moved
// to 2.5.2, so users exercised a version nobody tested (issue #1291).
func TestTemplateImagePins_MatchDevCompose(t *testing.T) {
	tmplPins, origin := templatePins(t)
	devPins := devComposePins(t)

	for _, name := range sortedNames(tmplPins) {
		devTag, monitored := devPins[name]
		if !monitored {
			continue // covered by TestTemplateImagePins_AreMonitoredOrDocumented
		}
		if tmplPins[name] != devTag {
			t.Errorf("image pin drift for %s: %s uses %q but %s uses %q — bump both together",
				name, devComposePath, devTag, origin[name], tmplPins[name])
		}
	}
}

// TestTemplateImagePins_AreMonitoredOrDocumented verifies that every image
// pinned in a template is either present in the Dependabot-parsed dev compose
// file or explicitly listed as unmonitored. Adding a new image to a template
// without one of the two fails here, so template-only pins cannot quietly
// accumulate (issue #1298).
func TestTemplateImagePins_AreMonitoredOrDocumented(t *testing.T) {
	tmplPins, origin := templatePins(t)
	devPins := devComposePins(t)

	for _, name := range sortedNames(tmplPins) {
		if _, monitored := devPins[name]; monitored {
			continue
		}
		if _, documented := unmonitoredTemplateImages[name]; !documented {
			t.Errorf("%s pins %s:%s, which is absent from %s and from unmonitoredTemplateImages — "+
				"add the image to the dev stack so Dependabot tracks it, or document why it is audited by hand",
				origin[name], name, tmplPins[name], devComposePath)
		}
	}
}

// TestUnmonitoredTemplateImages_HasNoStaleEntries verifies that the manual
// audit list stays honest: an entry must still be pinned by a template, and
// must still be missing from the dev compose file. Once an image reaches the
// dev stack, Dependabot covers it and the exemption has to go.
func TestUnmonitoredTemplateImages_HasNoStaleEntries(t *testing.T) {
	tmplPins, _ := templatePins(t)
	devPins := devComposePins(t)

	names := make([]string, 0, len(unmonitoredTemplateImages))
	for name := range unmonitoredTemplateImages {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, pinned := tmplPins[name]; !pinned {
			t.Errorf("unmonitoredTemplateImages lists %q, but no template pins it — drop the entry", name)
		}
		if _, monitored := devPins[name]; monitored {
			t.Errorf("unmonitoredTemplateImages lists %q, but %s pins it too — Dependabot covers it, drop the entry",
				name, devComposePath)
		}
		if strings.TrimSpace(unmonitoredTemplateImages[name]) == "" {
			t.Errorf("unmonitoredTemplateImages entry %q has no reason — say why it is audited by hand", name)
		}
	}
}
