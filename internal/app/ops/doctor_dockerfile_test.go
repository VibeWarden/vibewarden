package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/app/ops"
	"github.com/vibewarden/vibewarden/internal/config"
)

// ── fixture Dockerfiles ──────────────────────────────────────────────────────

// allOKDockerfile is a fully compliant Dockerfile for Go projects.
const allOKDockerfile = `FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /app/server ./cmd/server
FROM alpine:latest
COPY --from=builder /app/server .
EXPOSE 3000
USER nonroot
CMD ["./server"]
`

// failAlpineDockerfile uses a non-alpine final stage.
const failAlpineDockerfile = `FROM golang:1.26-alpine AS builder
RUN go build -o /app/server .
FROM ubuntu:22.04
EXPOSE 3000
USER nonroot
CMD ["./server"]
`

// failExposeDockerfile has an EXPOSE that doesn't match upstream.port 3000.
const failExposeDockerfile = `FROM alpine:latest
EXPOSE 8080
USER nonroot
CMD ["./app"]
`

// failHealthcheckDockerfile includes a HEALTHCHECK directive.
const failHealthcheckDockerfile = `FROM alpine:latest
EXPOSE 3000
HEALTHCHECK CMD wget -q http://localhost:3000/health
USER nonroot
CMD ["./app"]
`

// warnNonrootDockerfile omits the USER directive (non-blocking warn).
const warnNonrootDockerfile = `FROM alpine:latest
EXPOSE 3000
CMD ["./app"]
`

// failMultistageDockerfile is a single-stage Go Dockerfile.
const failMultistageDockerfile = `FROM golang:1.26-alpine
WORKDIR /app
COPY . .
RUN go build -o /app/server .
EXPOSE 3000
USER nonroot
CMD ["/app/server"]
`

// failToolchainDockerfile uses golang:1.24 but go.mod says go 1.26.
const failToolchainDockerfile = `FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /app/server .
FROM alpine:latest
COPY --from=builder /app/server .
EXPOSE 3000
USER nonroot
CMD ["./server"]
`

// goMod126 is a minimal go.mod requiring Go 1.26.
const goMod126 = "module example.com/app\n\ngo 1.26\n"

// ── helpers ──────────────────────────────────────────────────────────────────

// doctorSvc returns a minimal DoctorService suitable for Dockerfile-only tests.
// The compose/port/health fakes ensure all non-Dockerfile checks pass.
func doctorSvc() *ops.DoctorService {
	fc := noContainersCompose()
	pc := &fakePortChecker{available: map[int]bool{8443: true}}
	hc := reachableHealthChecker()
	return ops.NewDoctorService(fc, pc, hc)
}

// projectWithDockerfile creates a temp dir with the given Dockerfile content
// and optionally a go.mod file.
func projectWithDockerfile(t *testing.T, dockerfileContent, goModContent string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfileContent), 0o600); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if goModContent != "" {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goModContent), 0o600); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	}
	return dir
}

// runDockerfileDoctor runs the doctor and returns the JSON results.
func runDockerfileDoctor(t *testing.T, svc *ops.DoctorService, cfg *config.Config, projectRoot string) []ops.CheckResult {
	t.Helper()
	opts := ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    projectRoot,
		JSON:       true,
	}
	var buf bytes.Buffer
	if _, err := svc.Run(context.Background(), cfg, opts, &buf); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	var results []ops.CheckResult
	if err := json.Unmarshal(buf.Bytes(), &results); err != nil {
		t.Fatalf("JSON unmarshal: %v\nbody: %s", err, buf.String())
	}
	return results
}

// dockerfileResults filters results to those with section "Dockerfile".
func dockerfileResults(results []ops.CheckResult) []ops.CheckResult {
	var out []ops.CheckResult
	for _, r := range results {
		if r.Section == "Dockerfile" {
			out = append(out, r)
		}
	}
	return out
}

// findByName returns the first CheckResult with the given name, or zero value.
func findByName(results []ops.CheckResult, name string) (ops.CheckResult, bool) {
	for _, r := range results {
		if r.Name == name {
			return r, true
		}
	}
	return ops.CheckResult{}, false
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestCheckDockerfile_NoDockerfile_SectionOmitted(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000
	dir := t.TempDir() // no Dockerfile

	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	if len(dfResults) != 0 {
		t.Errorf("expected Dockerfile section to be omitted when no Dockerfile, got %d results", len(dfResults))
	}
}

func TestCheckDockerfile_AllOK(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, allOKDockerfile, goMod126)
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	if len(dfResults) == 0 {
		t.Fatal("expected Dockerfile section to have results, got none")
	}
	for _, r := range dfResults {
		if r.Severity == ops.SeverityFail {
			t.Errorf("unexpected FAIL for %q: %s", r.Name, r.Detail)
		}
	}
	// Toolchain match check should be OK for go 1.26 / golang:1.26-alpine.
	tc, found := findByName(dfResults, "Dockerfile: go toolchain version")
	if !found {
		t.Error("expected toolchain version check in results")
	} else if tc.Severity != ops.SeverityOK {
		t.Errorf("toolchain version check severity = %q, want OK; detail: %s", tc.Severity, tc.Detail)
	}
}

func TestCheckDockerfile_FailAlpineBase(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, failAlpineDockerfile, "")
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: alpine base")
	if !found {
		t.Fatal("expected 'Dockerfile: alpine base' check in results")
	}
	if r.Severity != ops.SeverityFail {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "alpine") {
		t.Errorf("expected 'alpine' in detail, got: %s", r.Detail)
	}

	// Other checks should not also be FAIL (check independence).
	for _, other := range dfResults {
		if other.Name == "Dockerfile: alpine base" {
			continue
		}
		if other.Severity == ops.SeverityFail {
			t.Logf("additional FAIL for %q: %s", other.Name, other.Detail)
		}
	}
}

func TestCheckDockerfile_FailExposePort(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000 // Dockerfile exposes 8080

	dir := projectWithDockerfile(t, failExposeDockerfile, "")
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: EXPOSE port")
	if !found {
		t.Fatal("expected 'Dockerfile: EXPOSE port' check in results")
	}
	if r.Severity != ops.SeverityFail {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "8080") {
		t.Errorf("expected '8080' in detail, got: %s", r.Detail)
	}
}

func TestCheckDockerfile_FailHealthcheck(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, failHealthcheckDockerfile, "")
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: HEALTHCHECK")
	if !found {
		t.Fatal("expected 'Dockerfile: HEALTHCHECK' check in results")
	}
	if r.Severity != ops.SeverityFail {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckDockerfile_WarnNonRootUser_DoesNotFailAllOK(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, warnNonrootDockerfile, "")
	opts := ops.DoctorOptions{
		ConfigPath: "vibewarden.yaml",
		WorkDir:    dir,
	}
	var buf bytes.Buffer
	allOK, err := svc.Run(context.Background(), cfg, opts, &buf)
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	// Non-root WARN must NOT cause allOK = false (non-blocking contract).
	if !allOK {
		t.Errorf("expected allOK = true for a WARN-only Dockerfile result\noutput:\n%s", buf.String())
	}

	// Verify the WARN is present.
	if !strings.Contains(buf.String(), "[WARN]") {
		t.Errorf("expected [WARN] in output for non-root USER check\ngot:\n%s", buf.String())
	}
}

func TestCheckDockerfile_FailMultiStage(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, failMultistageDockerfile, goMod126)
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: multi-stage build")
	if !found {
		t.Fatal("expected 'Dockerfile: multi-stage build' check in results")
	}
	if r.Severity != ops.SeverityFail {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "multi-stage") {
		t.Errorf("expected 'multi-stage' in detail, got: %s", r.Detail)
	}
}

func TestCheckDockerfile_FailToolchainMatch(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	// go.mod says go 1.26 but Dockerfile uses golang:1.24-alpine.
	dir := projectWithDockerfile(t, failToolchainDockerfile, goMod126)
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: go toolchain version")
	if !found {
		t.Fatal("expected 'Dockerfile: go toolchain version' check in results")
	}
	if r.Severity != ops.SeverityFail {
		t.Errorf("severity = %q, want FAIL; detail: %s", r.Severity, r.Detail)
	}
	if !strings.Contains(r.Detail, "1.24") {
		t.Errorf("expected '1.24' in detail, got: %s", r.Detail)
	}
	if !strings.Contains(r.Detail, "1.26") {
		t.Errorf("expected '1.26' in detail, got: %s", r.Detail)
	}
}

func TestCheckDockerfile_JSONIncludesSection(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, allOKDockerfile, goMod126)
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	if len(dfResults) == 0 {
		t.Fatal("expected Dockerfile checks in JSON results")
	}
	for _, r := range dfResults {
		if r.Section != "Dockerfile" {
			t.Errorf("check %q has section %q, want 'Dockerfile'", r.Name, r.Section)
		}
	}
}

func TestCheckDockerfile_DefaultUpstreamPort3000(t *testing.T) {
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 0 // triggers default of 3000

	// Dockerfile EXPOSE 3000 should match.
	dir := projectWithDockerfile(t, allOKDockerfile, goMod126)
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	r, found := findByName(dfResults, "Dockerfile: EXPOSE port")
	if !found {
		t.Fatal("expected EXPOSE port check")
	}
	if r.Severity != ops.SeverityOK {
		t.Errorf("severity = %q, want OK; detail: %s", r.Severity, r.Detail)
	}
}

func TestCheckDockerfile_AllChecksIndependent(t *testing.T) {
	// Use failHealthcheckDockerfile which triggers exactly one FAIL and the
	// non-root WARN (no USER directive). All checks must still be in the output.
	svc := doctorSvc()
	cfg := doctorConfig()
	cfg.Upstream.Port = 3000

	dir := projectWithDockerfile(t, failHealthcheckDockerfile, "")
	results := runDockerfileDoctor(t, svc, cfg, dir)
	dfResults := dockerfileResults(results)

	// We expect at minimum: alpine base, EXPOSE port, HEALTHCHECK, non-root USER.
	expectedNames := []string{
		"Dockerfile: alpine base",
		"Dockerfile: EXPOSE port",
		"Dockerfile: HEALTHCHECK",
		"Dockerfile: non-root USER",
	}
	for _, name := range expectedNames {
		if _, found := findByName(dfResults, name); !found {
			t.Errorf("check %q missing from results", name)
		}
	}
}
