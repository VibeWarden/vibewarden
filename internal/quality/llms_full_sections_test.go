package quality_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// deprecatedReferenceSections lists top-level vibewarden.reference.yaml sections
// that are deliberately absent from llms-full.txt. Only deprecated sections
// belong here: llms-full.txt is what AI agents read to write configs, so
// documenting a superseded key there would actively cause bad configs.
var deprecatedReferenceSections = map[string]string{
	"metrics": "deprecated alias for telemetry.* — migrated at startup with a warning",
}

// TestLLMSFullTxt_CoversEveryReferenceSection asserts that every configuration
// key documented in vibewarden.reference.yaml is also discoverable in
// llms-full.txt.
//
// Both files are LLM-consumable content owned by this repo, but they drift
// independently: vibewarden.reference.yaml is the annotated schema reference,
// llms-full.txt is the single file agents load as their primary config
// reference. When a section is added to one and not the other, agents either
// omit valid options or hallucinate field names — PR #1324 added 13 sections to
// the reference file and only 6 reached llms-full.txt (#1326).
//
// The check runs in two passes:
//
//  1. Top-level sections (e.g. "compression:") must have a matching
//     "### compression" heading in llms-full.txt. A trailing parenthetical on
//     the heading is allowed ("### watch (hot config reload)").
//  2. Second-level keys (e.g. "tls:" → "cert_monitoring:") must appear
//     somewhere in llms-full.txt in dotted form ("tls.cert_monitoring"), which
//     is how the reference tables in section 5 name nested fields.
//
// It is intentionally one-directional: llms-full.txt documents a few keys the
// reference file does not yet carry, and this guard must not pressure anyone
// into deleting documentation to make it pass.
func TestLLMSFullTxt_CoversEveryReferenceSection(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	llmsPath := filepath.Join(repoRoot, "llms-full.txt")
	llmsRaw, err := os.ReadFile(llmsPath) //nolint:gosec // fixed repo-relative path
	if err != nil {
		t.Fatalf("reading %s: %v", llmsPath, err)
	}
	llms := string(llmsRaw)

	refPath := filepath.Join(repoRoot, "vibewarden.reference.yaml")
	refRaw, err := os.ReadFile(refPath) //nolint:gosec // fixed repo-relative path
	if err != nil {
		t.Fatalf("reading %s: %v", refPath, err)
	}

	for _, key := range referenceKeys(string(refRaw)) {
		t.Run(key.name, func(t *testing.T) {
			if reason, deprecated := deprecatedReferenceSections[key.section]; deprecated {
				t.Skipf("%s is deprecated (%s) — intentionally absent from llms-full.txt", key.section, reason)
			}
			if !key.matcher.MatchString(llms) {
				t.Errorf("vibewarden.reference.yaml documents %q but llms-full.txt does not (%s).\n"+
					"Add it to the section 5 config reference in llms-full.txt — agents read that "+
					"file as their primary config reference and cannot discover the key otherwise.",
					key.name, key.hint)
			}
		})
	}
}

// referenceKey is one configuration key discovered in vibewarden.reference.yaml
// together with the pattern that proves llms-full.txt documents it.
type referenceKey struct {
	// name is the dotted key path, e.g. "tls" or "tls.cert_monitoring".
	name string
	// section is the top-level section the key belongs to, used for the
	// deprecation skip list.
	section string
	// matcher matches the expected llms-full.txt representation.
	matcher *regexp.Regexp
	// hint describes what the matcher looked for, for the failure message.
	hint string
}

var (
	topLevelKeyRe = regexp.MustCompile(`^([a-z_][a-z0-9_]*):`)
	nestedKeyRe   = regexp.MustCompile(`^ {2}([a-z_][a-z0-9_]*):`)
)

// referenceKeys extracts the top-level and second-level mapping keys from the
// reference YAML source. Commented-out example blocks are ignored, so only keys
// that are live in the file are checked.
func referenceKeys(ref string) []referenceKey {
	var (
		keys    []referenceKey
		section string
	)
	for _, line := range strings.Split(ref, "\n") {
		if m := topLevelKeyRe.FindStringSubmatch(line); m != nil {
			section = m[1]
			keys = append(keys, referenceKey{
				name:    section,
				section: section,
				matcher: regexp.MustCompile(`(?m)^### ` + regexp.QuoteMeta(section) + `\b`),
				hint:    fmt.Sprintf("looked for a %q heading", "### "+section),
			})
			continue
		}
		if section == "" {
			continue
		}
		if m := nestedKeyRe.FindStringSubmatch(line); m != nil {
			dotted := section + "." + m[1]
			keys = append(keys, referenceKey{
				name:    dotted,
				section: section,
				matcher: regexp.MustCompile(regexp.QuoteMeta(dotted)),
				hint:    fmt.Sprintf("looked for the literal text %q", dotted),
			})
		}
	}
	return keys
}
