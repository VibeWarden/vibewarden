package ops

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/vibewarden/vibewarden/internal/config"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// sectionPreflight returns the section header string for the given env name.
func sectionPreflight(envName string) string {
	return "Preflight: " + envName
}

// runPreflightChecks executes the five pre-deploy checks against the merged
// config for envName. Each check is independent — a failing check does not stop
// subsequent ones. All results are tagged with the preflight section header.
func runPreflightChecks(
	ctx context.Context,
	cfg *config.Config,
	envName string,
	dnsResolver ports.DNSResolver,
	imageInspector ports.ImageInspector,
) []CheckResult {
	section := sectionPreflight(envName)

	checks := []CheckResult{
		checkDNSResolves(ctx, cfg, dnsResolver),
		checkProductionPort(cfg),
		checkTargetPlatform(cfg),
		checkImageArch(ctx, cfg, imageInspector),
		checkTLSEmail(cfg),
	}

	for i := range checks {
		checks[i].Section = section
	}
	return checks
}

// checkDNSResolves verifies that tls.domain resolves to at least one IP address.
//
// Severity mapping (per architect spec):
//   - Non-empty addrs → OK with list of IPs (truncated to 3 + "...").
//   - Empty addrs or *net.DNSError{IsNotFound: true} → WARN (DNS not yet configured).
//   - Any other lookup error → WARN (network unreachable; doctor must run offline).
func checkDNSResolves(ctx context.Context, cfg *config.Config, resolver ports.DNSResolver) CheckResult {
	domain := cfg.TLS.Domain
	if domain == "" {
		return CheckResult{
			Name:     "DNS resolves",
			Severity: SeverityOK,
			Detail:   "tls.domain is empty — skipping DNS check",
		}
	}

	addrs, err := resolver.LookupHost(ctx, domain)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return CheckResult{
				Name:     "DNS resolves",
				Severity: SeverityWarn,
				Detail:   fmt.Sprintf("NXDOMAIN: no records for %s (DNS not yet configured?)", domain),
			}
		}
		return CheckResult{
			Name:     "DNS resolves",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("lookup failed: %v (network unreachable? doctor can run offline)", err),
		}
	}

	if len(addrs) == 0 {
		return CheckResult{
			Name:     "DNS resolves",
			Severity: SeverityWarn,
			Detail:   fmt.Sprintf("NXDOMAIN: no records for %s (DNS not yet configured?)", domain),
		}
	}

	displayAddrs := addrs
	suffix := ""
	if len(displayAddrs) > 3 {
		displayAddrs = displayAddrs[:3]
		suffix = ", ..."
	}
	return CheckResult{
		Name:     "DNS resolves",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("%s → %s%s", domain, strings.Join(displayAddrs, ", "), suffix),
	}
}

// checkProductionPort verifies that server.port is 443 for production deploys.
//
// Severity mapping:
//   - port == 443 → OK.
//   - any other value → WARN.
func checkProductionPort(cfg *config.Config) CheckResult {
	port := cfg.Server.Port
	if port == 0 {
		port = 8443 // match the default used elsewhere in doctor
	}
	if port == 443 {
		return CheckResult{
			Name:     "Production port",
			Severity: SeverityOK,
			Detail:   "server.port = 443",
		}
	}
	return CheckResult{
		Name:     "Production port",
		Severity: SeverityWarn,
		Detail:   fmt.Sprintf("server.port = %d (expected 443 for production)", port),
	}
}

// checkTargetPlatform verifies that deploy.target_platform is set.
//
// Severity mapping:
//   - non-empty → OK.
//   - empty → FAIL (the config default is "linux/amd64"; empty means the user
//     explicitly deleted it).
func checkTargetPlatform(cfg *config.Config) CheckResult {
	platform := cfg.Deploy.TargetPlatform
	if platform == "" {
		return CheckResult{
			Name:     "Target platform",
			Severity: SeverityFail,
			Detail:   "deploy.target_platform is empty — set it in vibewarden.<env>.yaml",
		}
	}
	return CheckResult{
		Name:     "Target platform",
		Severity: SeverityOK,
		Detail:   platform,
	}
}

// checkImageArch inspects the app image and compares its OS/arch against
// deploy.target_platform.
//
// Image tag resolution: cfg.App.Image when set; falls back to the project-name
// basename convention "<project>-app:latest" using cfg.Name. When neither is
// resolvable the check is skipped with an OK result.
//
// Severity mapping:
//   - ports.ErrImageNotFound → WARN (registry image or not yet built).
//   - ports.ErrDockerUnavailable → WARN (static doctor check already FAILed for this).
//   - any other Inspect error → WARN (best-effort; don't block the doctor run).
//   - os/arch matches target_platform → OK.
//   - os/arch does NOT match → FAIL with rebuild hint.
func checkImageArch(ctx context.Context, cfg *config.Config, inspector ports.ImageInspector) CheckResult {
	tag := cfg.App.Image
	if tag == "" && cfg.Name != "" {
		tag = cfg.Name + "-app:latest"
	}
	if tag == "" {
		return CheckResult{
			Name:     "App image arch",
			Severity: SeverityOK,
			Detail:   "no app.image configured — skipping arch check",
		}
	}

	info, err := inspector.Inspect(ctx, tag)
	if err != nil {
		switch {
		case errors.Is(err, ports.ErrImageNotFound):
			return CheckResult{
				Name:     "App image arch",
				Severity: SeverityWarn,
				Detail:   fmt.Sprintf("image %q not found locally — registry image? skipping arch check", tag),
			}
		case errors.Is(err, ports.ErrDockerUnavailable):
			return CheckResult{
				Name:     "App image arch",
				Severity: SeverityWarn,
				Detail:   "could not verify image arch — Docker unavailable",
			}
		default:
			return CheckResult{
				Name:     "App image arch",
				Severity: SeverityWarn,
				Detail:   fmt.Sprintf("could not inspect image %q: %v", tag, err),
			}
		}
	}

	imgPlatform := info.Platform()
	cfgPlatform := cfg.Deploy.TargetPlatform
	if cfgPlatform == "" || imgPlatform == cfgPlatform {
		return CheckResult{
			Name:     "App image arch",
			Severity: SeverityOK,
			Detail:   fmt.Sprintf("%s matches deploy.target_platform", imgPlatform),
		}
	}
	return CheckResult{
		Name:     "App image arch",
		Severity: SeverityFail,
		Detail: fmt.Sprintf(
			"image arch %s does not match deploy.target_platform %s — rebuild with --platform=%s",
			imgPlatform, cfgPlatform, cfgPlatform,
		),
	}
}

// checkTLSEmail verifies that tls.email is set when using Let's Encrypt, so
// the user receives certificate expiry warnings.
//
// Severity mapping (per architect spec):
//   - provider == "letsencrypt" AND email == "" → WARN.
//   - provider == "letsencrypt" AND email != "" → OK with "email configured: <addr>".
//   - any other provider → OK with "not using Let's Encrypt — email not required".
func checkTLSEmail(cfg *config.Config) CheckResult {
	provider := strings.ToLower(cfg.TLS.Provider)
	if provider != "letsencrypt" {
		return CheckResult{
			Name:     "TLS email",
			Severity: SeverityOK,
			Detail:   "not using Let's Encrypt — email not required",
		}
	}
	if cfg.TLS.Email == "" {
		return CheckResult{
			Name:     "TLS email",
			Severity: SeverityWarn,
			Detail:   "empty (Let's Encrypt accepts anonymous, but warnings won't reach you. Set tls.email in vibewarden.<env>.yaml)",
		}
	}
	return CheckResult{
		Name:     "TLS email",
		Severity: SeverityOK,
		Detail:   fmt.Sprintf("email configured: %s", cfg.TLS.Email),
	}
}
