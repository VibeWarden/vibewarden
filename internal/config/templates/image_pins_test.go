package templates_test

import (
	"io/fs"
	"os"
	"path/filepath"
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
// This file is the one Dependabot actually parses (docker-compose ecosystem,
// root directory — the docker ecosystem does not fetch compose files), which
// makes it the upstream-tracking reference for every image the templates also
// pin.
const devComposePath = "../../../docker-compose.yml"

// internalRoot points at the repo's internal/ tree, walked by
// TestIntegrationTestImagePins_MatchDevCompose. Same relative-path rule as
// devComposePath: the working directory is internal/config/templates.
const internalRoot = "../../../internal"

// springBootDockerfilePath points at the Spring Boot example's multi-stage
// Dockerfile, whose two stages must share a Java major. Same relative-path
// rule as devComposePath.
const springBootDockerfilePath = "../../../examples/spring-boot/Dockerfile"

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

// integrationImagePinRe captures the name and tag of a Go string literal whose
// entire contents are an image reference, e.g. `"postgres:17-alpine"` or
// `"oryd/kratos:v26.2.0"`. Requiring the literal to be exactly the reference is
// what keeps it free of false positives: `"postgres://user:pass@postgres:5432/db"`,
// `"VIBEWARDEN_RATE_LIMIT_REDIS_ADDRESS=redis:6379"` and `"image: redis:7-alpine"`
// all fail to match, because the name class excludes `/`, `=` and `:` after the
// tag, and the closing quote must follow the tag immediately.
var integrationImagePinRe = regexp.MustCompile(`"([A-Za-z0-9][A-Za-z0-9./_-]*):([A-Za-z0-9][A-Za-z0-9._-]*)"`)

// integrationTestPins walks internalRoot for *_integration_test.go files and
// returns every image reference they pin, keyed by image name, together with
// the file each pin came from. Only names that the dev compose file already
// tracks are returned — an unrelated `"foo:bar"` literal in some other test
// cannot trip the drift assertion.
func integrationTestPins(t *testing.T, tracked imagePins) (imagePins, map[string]string) {
	t.Helper()

	// Collect first, read second: reading inside the WalkDir callback trips
	// gosec G122 (symlink TOCTOU on a walked path).
	var files []string
	err := filepath.WalkDir(internalRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, "_integration_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", internalRoot, err)
	}
	sort.Strings(files)

	pins := imagePins{}
	origin := map[string]string{}

	for _, path := range files {
		data, readErr := os.ReadFile(path) // #nosec G304 -- path comes from a WalkDir over a repo-local tree
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		for _, m := range integrationImagePinRe.FindAllStringSubmatch(string(data), -1) {
			name, tag := m[1], m[2]
			if _, ok := tracked[name]; !ok {
				continue
			}
			if existing, ok := pins[name]; ok && existing != tag {
				t.Errorf("integration tests pin conflicting tags %q (%s) and %q (%s) for image %q — all must share one tag",
					existing, origin[name], tag, path, name)
				continue
			}
			pins[name] = tag
			origin[name] = path
		}
	}
	return pins, origin
}

// TestIntegrationTestImagePins_MatchDevCompose verifies that every testcontainers
// image pinned in an integration test uses the same tag as the dev compose file.
//
// This is the third pin site, invisible to both Dependabot (which parses neither
// Go source nor .tmpl files) and to TestTemplateImagePins_MatchDevCompose. It is
// how a deliberate pin rots: someone bumps the testcontainers image and CI starts
// certifying a Postgres nobody runs, or the compose pin moves and the integration
// suite silently keeps exercising the old one. Two Postgres pins had already
// drifted to 16-alpine when this test was added (issue #1495, ADR-113).
func TestIntegrationTestImagePins_MatchDevCompose(t *testing.T) {
	devPins := devComposePins(t)
	testPins, origin := integrationTestPins(t, devPins)

	if len(testPins) == 0 {
		t.Fatalf("no image pin found in any *_integration_test.go under %s — has the testcontainers image syntax moved?", internalRoot)
	}

	for _, name := range sortedNames(testPins) {
		if testPins[name] != devPins[name] {
			t.Errorf("image pin drift for %s: %s uses %q but %s uses %q — bump both together",
				name, devComposePath, devPins[name], origin[name], testPins[name])
		}
	}
}

// javaMajorRe captures the Java major of a Temurin-based image tag in a
// Dockerfile `FROM` line. It matches both stages of the Spring Boot example:
// `maven:3.9-eclipse-temurin-25-alpine` (build) and `eclipse-temurin:25-jre-alpine`
// (runtime). The `eclipse-temurin[-:]` alternation is what distinguishes the
// Java major from the Maven major that precedes it in the build-stage tag.
var javaMajorRe = regexp.MustCompile(`(?m)^FROM\s+\S*eclipse-temurin[-:](\d+)`)

// TestSpringBootExample_BuildAndRuntimeShareJavaMajor verifies that the Spring
// Boot example's build stage and runtime stage run the same Java major.
//
// This is a fourth pin site with a constraint no tag-equality check can express:
// the two images have different names, so nothing above compares them, yet the
// pom sets no <java.version> and javac therefore targets whatever JDK the build
// stage ships. A newer build stage than runtime stage emits class files the
// runtime refuses with UnsupportedClassVersionError — an error that appears only
// when the container starts, long after the image builds green.
//
// Dependabot treats the two as independent and has proposed the split twice:
// runtime-only in #1494 (caught by hand in #1495, ADR-113) and build-only in
// #1499 (rejected in #1505). The maven image is now ignored in
// .github/dependabot.yml, which stops the automated half; this test stops the
// manual half.
func TestSpringBootExample_BuildAndRuntimeShareJavaMajor(t *testing.T) {
	data, err := os.ReadFile(springBootDockerfilePath)
	if err != nil {
		t.Fatalf("reading %s: %v", springBootDockerfilePath, err)
	}

	matches := javaMajorRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) < 2 {
		t.Fatalf("%s: found %d Temurin FROM line(s), want the build and runtime stages — "+
			"has the example stopped using a multi-stage Temurin build?",
			springBootDockerfilePath, len(matches))
	}

	first := matches[0][1]
	for _, m := range matches[1:] {
		if m[1] != first {
			t.Errorf("%s: Java major drift between stages — one FROM uses Temurin %s, another uses %s. "+
				"The pom sets no <java.version>, so a newer build stage emits class files the runtime "+
				"rejects with UnsupportedClassVersionError. Bump every stage together.",
				springBootDockerfilePath, first, m[1])
		}
	}
}
