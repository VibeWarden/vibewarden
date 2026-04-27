package dockerfile

import (
	"fmt"
	"regexp"
	"strings"
)

// Severity classifies the outcome of a single Dockerfile rule check.
// It is a local type so that the dockerfile package does not import the ops
// package and risk a cycle; the ops package translates these to ops.Severity
// at the doctor boundary.
type Severity string

const (
	// SeverityOK means the rule check passed.
	SeverityOK Severity = "OK"
	// SeverityWarn means the check found something worth noting but not critical.
	SeverityWarn Severity = "WARN"
	// SeverityFail means the check found a critical problem.
	SeverityFail Severity = "FAIL"
	// SeverityOff means the check does not apply (skipped — no row emitted).
	SeverityOff Severity = "OFF"
)

// RuleOutcome is the result of a single Dockerfile rule evaluation.
// It is translated to ops.CheckResult at the doctor boundary.
type RuleOutcome struct {
	// Name is the human-readable check name (e.g. "Dockerfile: alpine base").
	Name string
	// State is the severity of the outcome.
	State Severity
	// Detail includes the result description and, when applicable, the fix hint.
	Detail string
}

// ─── Rule 1: Alpine base ─────────────────────────────────────────────────────

// RuleAlpineBase checks that the final-stage FROM tag is Alpine-based.
// The sidecar's compose healthcheck uses wget which ships on Alpine but is
// absent from distroless and scratch images.
//
// Returns SeverityOff when the Parsed struct has no stages (Dockerfile absent
// or empty). Returns SeverityOK when the final stage's tag contains "alpine"
// (case-insensitive). Returns SeverityFail otherwise with a fix hint.
func RuleAlpineBase(p Parsed) RuleOutcome {
	const name = "Dockerfile: alpine base"
	if len(p.Stages) == 0 {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	final := p.Stages[len(p.Stages)-1]
	if isAlpineTag(final.Image, final.Tag) {
		return RuleOutcome{Name: name, State: SeverityOK, Detail: fmt.Sprintf("final stage uses alpine-based image (%s:%s)", final.Image, final.Tag)}
	}
	return RuleOutcome{
		Name:  name,
		State: SeverityFail,
		Detail: "final stage is not alpine-based — " +
			"the sidecar's `wget` healthcheck requires alpine; " +
			"add `-alpine` to your final-stage FROM tag (e.g. `FROM alpine:latest` or `FROM golang:1.26-alpine`)",
	}
}

// isAlpineTag returns true when the image+tag combination is Alpine-based.
// Covers: pure alpine image, *-alpine tag suffix, and the well-known
// alpine-derived images shipped with an alpine tag variant.
func isAlpineTag(image, tag string) bool {
	image = strings.ToLower(image)
	tag = strings.ToLower(tag)

	// The "alpine" image itself.
	if image == "alpine" {
		return true
	}
	// Any image whose tag contains "alpine" (e.g. golang:1.26-alpine).
	if strings.Contains(tag, "alpine") {
		return true
	}
	return false
}

// ─── Rule 2: EXPOSE matches upstream.port ─────────────────────────────────────

// RuleExposeMatchesPort checks that the EXPOSE port in the Dockerfile matches
// the upstream port configured in vibewarden.yaml.
//
// Returns SeverityOff when no EXPOSE directive is present. Returns SeverityOK
// when the last EXPOSE port matches upstreamPort. Returns SeverityFail on
// mismatch.
func RuleExposeMatchesPort(p Parsed, upstreamPort int) RuleOutcome {
	const name = "Dockerfile: EXPOSE port"
	if len(p.Exposes) == 0 {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	last := p.Exposes[len(p.Exposes)-1]
	if last.Port == upstreamPort {
		return RuleOutcome{Name: name, State: SeverityOK, Detail: fmt.Sprintf("EXPOSE %d matches upstream.port", last.Port)}
	}
	return RuleOutcome{
		Name:  name,
		State: SeverityFail,
		Detail: fmt.Sprintf(
			"Dockerfile EXPOSE %d does not match upstream.port %d in vibewarden.yaml — update one to match the other",
			last.Port, upstreamPort,
		),
	}
}

// ─── Rule 3: No HEALTHCHECK ──────────────────────────────────────────────────

// RuleNoHealthcheck checks that the Dockerfile does not contain a HEALTHCHECK
// directive. Compose owns the healthcheck; a Dockerfile HEALTHCHECK conflicts
// with compose's container state view.
//
// Returns SeverityOff when the Parsed struct has no stages. Returns SeverityFail
// when HasHealthcheck is true. Returns SeverityOK otherwise.
func RuleNoHealthcheck(p Parsed) RuleOutcome {
	const name = "Dockerfile: HEALTHCHECK"
	if len(p.Stages) == 0 {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	if p.HasHealthcheck {
		return RuleOutcome{
			Name:  name,
			State: SeverityFail,
			Detail: "HEALTHCHECK directive found — compose owns the healthcheck " +
				"(already declared in the generated docker-compose.yml); " +
				"remove the HEALTHCHECK directive from the Dockerfile to avoid conflicts",
		}
	}
	return RuleOutcome{Name: name, State: SeverityOK, Detail: "no HEALTHCHECK directive"}
}

// ─── Rule 4: Non-root USER ───────────────────────────────────────────────────

// RuleNonRootUser checks that the final stage sets a non-root USER directive.
// Running as root is flagged at warn level (non-blocking per architect spec).
//
// Returns SeverityOff when the Parsed struct has no stages. Returns SeverityOK
// when the final stage USER is set and is not root/0. Returns SeverityWarn
// when the USER is missing or is root/0.
func RuleNonRootUser(p Parsed) RuleOutcome {
	const name = "Dockerfile: non-root USER"
	if len(p.Stages) == 0 {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	user := p.FinalUser
	if isRootUser(user) {
		detail := "WARN: set USER to non-root for production hardening — " +
			"the final stage runs as root; add `USER nonroot` (or any non-root UID) for production hardening"
		return RuleOutcome{Name: name, State: SeverityWarn, Detail: detail}
	}
	return RuleOutcome{Name: name, State: SeverityOK, Detail: fmt.Sprintf("USER %s (non-root)", user)}
}

// isRootUser returns true when the user string indicates the root user: empty
// string (no USER set), "root", "0", or "root:<group>".
func isRootUser(user string) bool {
	if user == "" {
		return true
	}
	lower := strings.ToLower(user)
	// Strip group component: "root:root" → "root", "0:0" → "0".
	if idx := strings.Index(lower, ":"); idx >= 0 {
		lower = lower[:idx]
	}
	return lower == "root" || lower == "0"
}

// ─── Rule 5: Multi-stage for compiled languages ───────────────────────────────

// RuleMultiStageForCompiled checks that the Dockerfile uses a multi-stage build
// when the project's toolchain manifest indicates a compiled language (Go, Rust,
// Java, etc.). Single-stage builds ship the toolchain to the runtime image.
//
// Returns SeverityOff when no toolchain was detected or the Parsed struct has
// no stages. Returns SeverityOK when the Dockerfile is already multi-stage.
// Returns SeverityFail when the language is compiled and only a single stage is
// present.
func RuleMultiStageForCompiled(p Parsed, tc Toolchain) RuleOutcome {
	const name = "Dockerfile: multi-stage build"
	if len(p.Stages) == 0 {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	if !isCompiledLang(tc.Lang) {
		return RuleOutcome{Name: name, State: SeverityOff}
	}
	if p.IsMultiStage {
		return RuleOutcome{Name: name, State: SeverityOK, Detail: fmt.Sprintf("%d stages detected", len(p.Stages))}
	}
	return RuleOutcome{
		Name:  name,
		State: SeverityFail,
		Detail: fmt.Sprintf(
			"single-stage %s build detected — use a multi-stage build: "+
				"a builder stage pins the toolchain and compiles the binary; "+
				"the final stage copies only the compiled binary onto Alpine — no source, no toolchain",
			tc.Lang,
		),
	}
}

// isCompiledLang returns true when the language requires a build step that
// produces a compiled binary (Go, Rust, Java; not Node or Python).
func isCompiledLang(lang Lang) bool {
	switch lang {
	case LangGo:
		return true
	}
	return false
}

// ─── Rule 6: Toolchain version match ─────────────────────────────────────────

// reTagVersion matches an optional major.minor or just major at the start of
// a tag. The minor group is optional so that tags like "20-alpine" (Node) or
// "3-alpine" (Python) are handled as well as "1.26-alpine" (Go).
var reTagVersion = regexp.MustCompile(`^(\d+)(?:\.(\d+))?`)

// builderImagePrefix maps Lang to the expected Docker Hub image name prefix for
// the builder stage (e.g. LangGo → "golang").
var builderImagePrefix = map[Lang]string{
	LangGo:     "golang",
	LangNode:   "node",
	LangPython: "python",
}

// RuleToolchainMatch checks that the builder stage's image tag major.minor
// matches the version declared in the project's toolchain manifest.
//
// This is the qr-dali check: catches `go.mod` requiring Go 1.26 while the
// Dockerfile uses `golang:1.24-alpine`, which manifests as an opaque
// `go mod download exit code 1`.
//
// Returns SeverityOff when:
//   - No stages are present.
//   - No toolchain was detected.
//   - The builder stage image does not match the expected prefix for the lang.
//   - The tag contains no recognisable major.minor version (e.g. "latest").
//
// Returns SeverityOK when the builder tag major.minor matches the manifest.
// Returns SeverityFail on mismatch.
func RuleToolchainMatch(p Parsed, tc Toolchain) RuleOutcome {
	checkName := fmt.Sprintf("Dockerfile: %s toolchain version", tc.Lang)
	if len(p.Stages) == 0 {
		return RuleOutcome{Name: checkName, State: SeverityOff}
	}
	prefix, ok := builderImagePrefix[tc.Lang]
	if !ok {
		return RuleOutcome{Name: checkName, State: SeverityOff}
	}

	// Builder stage is the first stage (index 0) in a multi-stage build, or
	// the only stage in a single-stage build.
	builder := p.Stages[0]
	if !strings.EqualFold(builder.Image, prefix) {
		return RuleOutcome{Name: checkName, State: SeverityOff}
	}

	// Extract major.minor from the builder tag.
	m := reTagVersion.FindStringSubmatch(builder.Tag)
	if m == nil {
		// Tag has no leading version (e.g. "latest", "alpine", digest-only) — skip.
		return RuleOutcome{Name: checkName, State: SeverityOff}
	}

	tagMajor := parseInt(m[1])
	tagMinor := 0
	if len(m) > 2 && m[2] != "" {
		tagMinor = parseInt(m[2])
	}

	if tagMajor == tc.Major && tagMinor == tc.Minor {
		return RuleOutcome{
			Name:   checkName,
			State:  SeverityOK,
			Detail: fmt.Sprintf("builder %s:%s matches %s %d.%d (%s)", builder.Image, builder.Tag, tc.Lang, tc.Major, tc.Minor, tc.Source),
		}
	}

	return RuleOutcome{
		Name:  checkName,
		State: SeverityFail,
		Detail: fmt.Sprintf(
			"%s requires %s %d.%d (%s) but the Dockerfile builder uses %s:%s — bump the FROM tag to `%s:%d.%d-alpine`",
			tc.Source, tc.Lang, tc.Major, tc.Minor, tc.Source,
			builder.Image, builder.Tag,
			prefix, tc.Major, tc.Minor,
		),
	}
}

// parseInt parses a string as an integer, returning 0 on failure.
func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
