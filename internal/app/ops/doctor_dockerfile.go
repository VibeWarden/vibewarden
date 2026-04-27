package ops

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/vibewarden/vibewarden/internal/app/dockerfile"
	"github.com/vibewarden/vibewarden/internal/config"
)

// sectionDockerfile is the section header for Dockerfile contract checks.
const sectionDockerfile = "Dockerfile"

// checkDockerfile parses the project's Dockerfile and evaluates all six
// contract rules. When no Dockerfile is present the function returns nil (the
// "Dockerfile" section is omitted entirely from the doctor report). When the
// Dockerfile is present but unparseable the function also returns nil,
// consistent with the validate package's silent-skip convention.
//
// Each rule that returns SeverityOff is filtered out; the remaining results are
// tagged with sectionDockerfile and returned. The non-root USER rule returns
// SeverityWarn on failure, which does NOT cause vibew doctor to exit non-zero.
func (s *DoctorService) checkDockerfile(_ context.Context, projectRoot string, cfg *config.Config) []CheckResult {
	if projectRoot == "" {
		return nil
	}

	dockerfilePath := filepath.Join(projectRoot, "Dockerfile")
	f, err := os.Open(dockerfilePath) //nolint:gosec // projectRoot is resolved by the caller
	if err != nil {
		// No Dockerfile → section omitted.
		return nil
	}
	defer func() { _ = f.Close() }()

	parsed, err := dockerfile.Parse(f)
	if err != nil {
		// Unparseable — silent skip.
		return nil
	}

	// Fallback to zero Toolchain when detection fails (e.g. manifest read
	// error). RuleMultiStageForCompiled and RuleToolchainMatch degrade
	// gracefully with an empty Toolchain, so the remaining rules still run.
	tc, _, tcErr := dockerfile.DetectToolchain(projectRoot)
	if tcErr != nil {
		slog.Warn("dockerfile toolchain detection failed; skipping toolchain rules", "error", tcErr)
		tc = dockerfile.Toolchain{}
	}

	upstreamPort := cfg.Upstream.Port
	if upstreamPort == 0 {
		upstreamPort = 3000
	}

	outcomes := []dockerfile.RuleOutcome{
		dockerfile.RuleAlpineBase(parsed),
		dockerfile.RuleExposeMatchesPort(parsed, upstreamPort),
		dockerfile.RuleNoHealthcheck(parsed),
		dockerfile.RuleNonRootUser(parsed),
		dockerfile.RuleMultiStageForCompiled(parsed, tc),
		dockerfile.RuleToolchainMatch(parsed, tc),
	}

	out := make([]CheckResult, 0, len(outcomes))
	for _, o := range outcomes {
		if o.State == dockerfile.SeverityOff {
			continue
		}
		out = append(out, withSection(toCheckResult(o), sectionDockerfile))
	}
	return out
}

// toCheckResult translates a dockerfile.RuleOutcome to an ops.CheckResult.
func toCheckResult(o dockerfile.RuleOutcome) CheckResult {
	return CheckResult{
		Name:     o.Name,
		Severity: dockerfileSeverity(o.State),
		Detail:   o.Detail,
	}
}

// dockerfileSeverity maps dockerfile.Severity to ops.Severity.
func dockerfileSeverity(s dockerfile.Severity) Severity {
	switch s {
	case dockerfile.SeverityOK:
		return SeverityOK
	case dockerfile.SeverityWarn:
		return SeverityWarn
	default:
		return SeverityFail
	}
}
