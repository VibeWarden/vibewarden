package bundle

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// bundleExtraFiles are the file names written by writeBundleExtras. They
// are enumerated here so tests and documentation can agree on the exact
// artifact set.
const (
	fileSampleEnv = "sample.env"
	fileDotEnv    = ".env"
	fileReadme    = "README.md"
	fileImageTar  = "image.tar"
	fileManifest  = "MANIFEST.md"
)

// appPlaceholder is used in README and stdout when appName is empty.
const appPlaceholder = "<your-app>"

// domainPlaceholder is used in README and stdout when domain is empty.
const domainPlaceholder = "<your-domain>"

// sshPlaceholder is used in README and stdout when deploy.host is unset.
// It is clearly bracketed so LLM agents do not treat it as a literal SSH
// target — the bracketed form is the cross-LLM literal-vs-template clarity
// standard locked by #1244 and CLAUDE.md §Architecture principles.
const sshPlaceholder = "<your-ssh-user>@<your-ssh-host>"

// manifestVersion is set at build time via ldflags in production; defaults to
// "dev" so unit tests produce stable deterministic output (excluding the header).
var manifestVersion = "dev"

// manifestFileDescriptions maps known bundle filenames to their one-line
// descriptions for MANIFEST.md. Unknown files receive a generic description.
var manifestFileDescriptions = map[string]string{
	"vibewarden.yaml":            "merged production config",
	"vibewarden.production.yaml": "production overrides",
	"docker-compose.yml":         "deterministic compose file",
	"image.tar":                  "docker-saved app image (omit when --skip-image)",
	"README.md":                  "deploy contract + command reference",
	".env":                       "environment variables (preserved across re-bundles, contains credentials)",
	"sample.env":                 "environment template (regenerated each bundle)",
}

// envTemplateName is the name of the .env template file the bundle service
// looks for in the project directory (the directory containing the base
// vibewarden.yaml) to seed sample.env. Missing template is not an error;
// sample.env is still generated with the default VIBEWARDEN_APP_IMAGE key.
const envTemplateName = ".env.template"

// writeBundleExtras produces the four artifacts added by "vibew bundle" on
// top of the compose/yaml pipeline: sample.env, .env, README.md, and
// image.tar. Idempotency rules live here (not in the render functions)
// so the pure renderers stay trivially testable.
//
// priorDotEnv is the byte content of the user's .env captured BEFORE the
// generator ran (priorExisted reports whether the snapshot was actually
// taken). The generator unconditionally writes a fresh .env with
// randomised credentials on every invocation, so the snapshot is how we
// honour the "preserved across runs" contract for .env.
//
// When s.bundleFS is nil this function is a no-op — existing callers like
// vibew deploy --dry-run never wired a BundleFS, so the extras are
// opt-in per Service instance. This is the hinge that lets ADR-085's
// migration option (c) work: both commands share Service.Bundle but only
// vibew bundle gets the additional artifacts.
func (s *Service) writeBundleExtras(ctx context.Context, opts BundleOptions, outDir string, priorDotEnv []byte, priorExisted bool) error {
	if s.bundleFS == nil {
		return nil
	}

	// Resolve the project name used in defaults. It matches the deploy
	// pipeline: opts.ProjectName wins when set; otherwise the config's
	// compose project name; otherwise the sanitised directory name.
	projectName := opts.ProjectName
	if projectName == "" && opts.Config != nil {
		projectName = opts.Config.ComposeProjectName()
	}
	if projectName == "" {
		projectName = ProjectNameFromConfig(opts.ConfigPath)
	}

	imageTag := opts.ImageTag
	if imageTag == "" && opts.Config != nil {
		imageTag = opts.Config.ComposeProjectName() + "-app:latest"
	}

	// Resolve the .env.template next to the base config, if present.
	templateKeys, err := readEnvTemplateKeys(opts.ConfigPath)
	if err != nil {
		return fmt.Errorf("reading .env.template: %w", err)
	}

	// sample.env — always overwrite.
	sampleBody := renderSampleEnv(imageTag, templateKeys)
	samplePath := filepath.Join(outDir, fileSampleEnv)
	if err := s.bundleFS.WriteFile(samplePath, []byte(sampleBody), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", fileSampleEnv, err)
	}

	// .env idempotency:
	//   - priorExisted + !Overwrite  → restore the snapshot (user's edits win).
	//   - priorExisted + Overwrite   → drop the snapshot; treat as first run.
	//   - !priorExisted              → first run: merge whatever the
	//                                   generator just wrote with the
	//                                   bundle body so the file holds both
	//                                   credentials and VIBEWARDEN_APP_IMAGE.
	dotEnvPath := filepath.Join(outDir, fileDotEnv)
	if priorExisted && !opts.Overwrite {
		// Generator already clobbered the file; restore the user's copy.
		if err := s.bundleFS.WriteFile(dotEnvPath, priorDotEnv, 0o600); err != nil {
			return fmt.Errorf("restoring %s: %w", fileDotEnv, err)
		}
	} else {
		// First-run OR --overwrite: merge whatever the generator just
		// wrote (credentials) with the bundle body (VIBEWARDEN_APP_IMAGE
		// + template keys). Keys that already exist in the generator
		// output are NOT duplicated. With --overwrite the merge source
		// is always the freshly-written generator file (no user state
		// leaks through).
		existing, readErr := s.bundleFS.ReadFile(dotEnvPath)
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return fmt.Errorf("reading generator %s: %w", fileDotEnv, readErr)
		}
		merged := mergeDotEnv(existing, renderDotEnv(imageTag, templateKeys))
		if err := s.bundleFS.WriteFile(dotEnvPath, []byte(merged), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", fileDotEnv, err)
		}
	}

	// Resolve the domain from config (may be empty for dev setups).
	domain := ""
	if opts.Config != nil {
		domain = opts.Config.TLS.Domain
	}

	// Resolve the SSH deploy host from config. When set, the literal value is
	// substituted verbatim into the README deploy block. When empty, the
	// bracketed placeholder is used and a hint paragraph is appended.
	sshHost := ""
	if opts.Config != nil {
		sshHost = opts.Config.Deploy.Host
	}

	// README.md — always overwrite.
	readmeBody := renderBundleReadme(projectName, domain, sshHost, opts.SkipImage)
	readmePath := filepath.Join(outDir, fileReadme)
	if err := s.bundleFS.WriteFile(readmePath, []byte(readmeBody), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", fileReadme, err)
	}

	// image.tar — skipped when --skip-image, or when no ImageSaver is wired.
	if !opts.SkipImage && s.imageSaver != nil {
		imagePath := filepath.Join(outDir, fileImageTar)
		if err := s.imageSaver.Save(ctx, imageTag, imagePath); err != nil {
			return fmt.Errorf("saving image %s to %s: %w", imageTag, fileImageTar, err)
		}
	}

	// MANIFEST.md — written last so it walks the actual file set on disk.
	// It uses os.ReadDir on outDir to enumerate real files, not the in-memory FS,
	// so it reflects everything the generator plus extras pipeline wrote.
	manifestBody, err := renderBundleManifest(outDir, projectName)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", fileManifest, err)
	}
	manifestPath := filepath.Join(outDir, fileManifest)
	if err := s.bundleFS.WriteFile(manifestPath, []byte(manifestBody), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", fileManifest, err)
	}

	return nil
}

// mergeDotEnv merges existing KEY=value lines from existing (typically the
// generator-written credentials) with the bundle-rendered body. Keys that
// already appear in existing keep their existing values — the bundle body
// only contributes keys not already present. This preserves the user's
// passwords across re-runs while still layering the bundle defaults on
// top for fresh installs.
//
// The output order is: existing lines first (in their original order),
// then bundle-only lines in their original order. Comments and blank
// lines from existing are preserved verbatim.
func mergeDotEnv(existing []byte, bundleBody string) string {
	existingKeys := map[string]bool{}
	for _, line := range strings.Split(string(existing), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if eq := strings.IndexByte(trim, '='); eq > 0 {
			existingKeys[strings.TrimSpace(trim[:eq])] = true
		}
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteString("\n")
		}
	}
	for _, line := range strings.Split(bundleBody, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			// Skip comments and blanks from the bundle body when merging
			// — the existing file already has its own header.
			continue
		}
		eq := strings.IndexByte(trim, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(trim[:eq])
		if existingKeys[key] {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// readEnvTemplateKeys loads the optional .env.template file sitting next to
// the base vibewarden.yaml and returns the KEY names (no values). A missing
// file returns (nil, nil) — that's the common case for new projects.
// Lines that are blank, comments (#...), or not KEY=value are skipped.
func readEnvTemplateKeys(configPath string) ([]string, error) {
	if configPath == "" {
		return nil, nil
	}
	tmplPath := filepath.Join(filepath.Dir(configPath), envTemplateName)
	data, err := os.ReadFile(tmplPath) //nolint:gosec // tmplPath derived from configPath (user project root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", tmplPath, err)
	}

	var keys []string
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys, nil
}

// renderSampleEnv produces the deterministic sample.env body.
// It always starts with VIBEWARDEN_APP_IMAGE set to imageTag, then emits
// any keys discovered in the project .env.template with an empty default
// value. Output is stable for a given input — no timestamps, no randomness.
func renderSampleEnv(imageTag string, templateKeys []string) string {
	var b strings.Builder
	b.WriteString("# sample.env — generated by `vibew bundle`.\n")
	b.WriteString("# Copy to .env and fill in real values before deploying.\n")
	b.WriteString("\n")
	b.WriteString("VIBEWARDEN_APP_IMAGE=")
	b.WriteString(imageTag)
	b.WriteString("\n")
	// Sort template keys to keep output stable regardless of line order in
	// the source template.
	keys := append([]string(nil), templateKeys...)
	sort.Strings(keys)
	for _, k := range keys {
		if k == "VIBEWARDEN_APP_IMAGE" {
			// Do not duplicate the managed key.
			continue
		}
		b.WriteString(k)
		b.WriteString("=\n")
	}
	return b.String()
}

// renderDotEnv produces the initial .env body.
// For v1 it is identical to sample.env — the two exist as distinct
// functions so the orchestrator can enforce different idempotency rules
// (sample.env is always regenerated; .env is preserved across runs).
func renderDotEnv(imageTag string, templateKeys []string) string {
	return renderSampleEnv(imageTag, templateKeys)
}

// shellQuoteSSHTarget wraps an SSH target value in POSIX single-quotes for
// safe interpolation into shell command strings. This is defence-in-depth:
// ValidateDeployHost already rejects metacharacters at config-load time, but
// wrapping in POSIX single-quotes ensures the value cannot be interpreted as
// shell code even if validation is bypassed.
func shellQuoteSSHTarget(host string) string {
	return "'" + host + "'"
}

// RenderBundleReadme produces the README.md shipped inside the bundle.
// It is exported so tests can call it directly to verify placeholder
// substitution without going through the full Bundle pipeline.
// See renderBundleReadme for the implementation.
//
// sshHost is the SSH deploy target (e.g. "alice@host.example" or a
// ~/.ssh/config alias). When empty, the bracketed placeholder
// "<your-ssh-user>@<your-ssh-host>" is used and a hint paragraph is appended
// after the deploy fenced block.
func RenderBundleReadme(appName, domain, sshHost string, skipImage bool) string {
	return renderBundleReadme(appName, domain, sshHost, skipImage)
}

// renderBundleReadme produces the README.md shipped inside the bundle.
// Output is stable for given inputs. The README opens with a copy-pasteable
// fenced deploy block so agents have a literal command sequence immediately.
//
// appName and domain are substituted into the command block. Empty values
// produce safe placeholders (<your-app>, <your-domain>). When sshHost is
// non-empty, it is substituted verbatim into all three ssh lines; otherwise
// the bracketed placeholder "<your-ssh-user>@<your-ssh-host>" is used and a
// hint paragraph is appended after the fenced block. When skipImage is true,
// the `docker load -i image.tar &&` clause is omitted from the deploy block
// so the printed sequence remains valid for registry-pull mode.
func renderBundleReadme(appName, domain, sshHost string, skipImage bool) string {
	app := appName
	if app == "" {
		app = appPlaceholder
	}
	dom := domain
	if dom == "" {
		dom = domainPlaceholder
	}

	// Resolve the SSH target. When empty, use the bracketed placeholder so
	// agents never mistake it for a real host. The hint paragraph is emitted
	// below (outside the fenced block) only when the placeholder was chosen.
	sshTarget := sshHost
	if sshTarget == "" {
		sshTarget = sshPlaceholder
	}
	// Single-quote the SSH target for safe shell interpolation. This is
	// defence-in-depth: ValidateDeployHost already rejects metacharacters at
	// config-load time, but wrapping in POSIX single-quotes ensures the value
	// cannot be interpreted as shell code even if validation is bypassed.
	quotedTarget := shellQuoteSSHTarget(sshTarget)

	// Build the deploy command block. When skipImage is true, the docker load
	// clause is omitted so the sequence is valid for registry-pull mode.
	dockerCmd := "docker load -i image.tar && docker compose up -d"
	if skipImage {
		dockerCmd = "docker compose up -d"
	}

	var b strings.Builder
	b.WriteString("# " + app + " deploy bundle\n")
	b.WriteString("\n")

	// Section: Deploy commands (at top, copy-pasteable).
	b.WriteString("## Deploy\n")
	b.WriteString("\n")
	b.WriteString("```bash\n")
	b.WriteString("ssh " + quotedTarget + " 'mkdir -p /opt/" + app + "'\n")
	b.WriteString("tar -czf - -C .vibewarden/bundle . | ssh " + quotedTarget + " 'tar -xzf - -C /opt/" + app + "/'\n")
	b.WriteString("ssh " + quotedTarget + " \"cd /opt/" + app + " && " + dockerCmd + "\"\n")
	b.WriteString("curl -fsSL https://" + dom + "/_vibewarden/health\n")
	b.WriteString("```\n")
	b.WriteString("\n")

	// Hint paragraph — emitted only when the placeholder was used (host unset).
	// It is omitted when deploy.host is configured so the block is clean.
	if sshHost == "" {
		b.WriteString("Replace `<your-ssh-user>@<your-ssh-host>` with your actual SSH target.\n")
		b.WriteString("  - Check `~/.ssh/config` for an existing alias.\n")
		b.WriteString("  - Or set `deploy.host: user@host` in `vibewarden.production.yaml`\n")
		b.WriteString("    (vibew will substitute it into the bundle stdout next time).\n")
		b.WriteString("\n")
	}

	// Section: What this is.
	b.WriteString("## What this is\n")
	b.WriteString("\n")
	b.WriteString("A self-contained Docker Compose bundle generated by `vibew bundle`. ")
	b.WriteString("Everything needed to run " + app + " on a remote host with Docker installed ships inside this directory — no source code, no toolchains, no vibew binary required on the server.\n")
	b.WriteString("\n")

	// Section: What's in this bundle (pointer to MANIFEST.md).
	b.WriteString("## What's in this bundle\n")
	b.WriteString("\n")
	b.WriteString("See `MANIFEST.md` for the full file listing.\n")
	b.WriteString("\n")
	b.WriteString("| File | Purpose |\n")
	b.WriteString("|---|---|\n")
	b.WriteString("| `docker-compose.yml` | Pinned, deterministic. Do not edit by hand. |\n")
	b.WriteString("| `vibewarden.yaml` | Merged production config. |\n")
	b.WriteString("| `image.tar` | Your app image. (Omitted in registry-pull mode — see `--skip-image`.) |\n")
	b.WriteString("| `.env` | Generated environment variables. Preserved across re-bundles. |\n")
	b.WriteString("| `sample.env` | Template for hand-edits, regenerated each bundle. |\n")
	b.WriteString("| `README.md` | This file. |\n")
	b.WriteString("| `MANIFEST.md` | Full file listing with descriptions. |\n")
	b.WriteString("\n")

	// Section: Why these commands (prose explanation).
	b.WriteString("## Why these commands\n")
	b.WriteString("\n")
	b.WriteString("Two things easy to get wrong: the remote directory must exist before you copy (create it on the host first), and the healthcheck port in production is **443** (TLS), not the dev port 8443.\n")
	b.WriteString("\n")
	b.WriteString("Image-load mode: if `image.tar` is present, load it into Docker on the host before starting the stack (`docker load -i image.tar`). Registry-pull mode (built with `--skip-image`): the compose file has `image:` set; pulling from the registry is optional if it is reachable at start time.\n")
	b.WriteString("\n")
	b.WriteString("The `curl` healthcheck returns HTTP 200 with JSON body containing `\"status\":\"ok\"` and `\"components\":{\"upstream\":\"ok\"}`. If `components.upstream` is `\"unknown\"`, the first health probe has not yet completed — wait 10 seconds and retry.\n")
	b.WriteString("\n")

	// Section: Upgrading.
	b.WriteString("**Upgrading from a previous deployment?** If your old stack ran under the project name `vibewarden-app`, run `docker compose -p vibewarden-app down` on the remote ONCE to remove orphan containers/networks before bringing up the new stack. The new stack uses your `app.name` for the project name.\n")
	b.WriteString("\n")

	// Section: Read-only inspection recipes.
	b.WriteString("## Read-only inspection\n")
	b.WriteString("\n")
	b.WriteString("```bash\n")
	b.WriteString("ssh " + quotedTarget + " docker compose -f /opt/" + app + "/docker-compose.yml logs --tail 50\n")
	b.WriteString("ssh " + quotedTarget + " docker compose -f /opt/" + app + "/docker-compose.yml ps\n")
	b.WriteString("curl -fsSL https://" + dom + "/_vibewarden/health\n")
	b.WriteString("```\n")
	b.WriteString("\n")

	// Section: Secrets.
	b.WriteString("## Secrets\n")
	b.WriteString("\n")
	b.WriteString("This bundle contains files with credentials: `.env`, `.credentials`, and anything under `kratos/`. They will land on the remote host once you copy the bundle there. If the destination is shared or the transport is untrusted, generate fresh credentials on the host instead rather than shipping them from your local machine.\n")
	b.WriteString("\n")

	// Section: Rebuild for a different arch.
	b.WriteString("## Rebuild for a different arch\n")
	b.WriteString("\n")
	b.WriteString("The `image.tar` inside this bundle (when present) matches the architecture of whatever machine built it. If your local laptop is arm64 and your VPS is amd64, rerun `vibew build --platform linux/amd64` before `vibew bundle`. Edit `.env` to change environment variables; it is preserved across `vibew bundle` runs unless you pass `--overwrite`.\n")

	return b.String()
}

// RenderBundleManifest produces the MANIFEST.md content for the bundle. It
// walks outDir on disk (not the in-memory FS) so it reflects the actual file
// set after every pipeline step has run. MANIFEST.md itself is excluded from
// its own listing. Entries are sorted alphabetically for determinism.
//
// The header line contains the timestamp and version (non-deterministic). The
// body (entry list) is fully deterministic for a given file set. Tests that
// assert determinism should strip the header line before comparing.
func RenderBundleManifest(outDir, appName string) (string, error) {
	return renderBundleManifest(outDir, appName)
}

// renderBundleManifest is the internal implementation of RenderBundleManifest.
func renderBundleManifest(outDir, appName string) (string, error) {
	entries, err := os.ReadDir(outDir) //nolint:gosec // outDir is the bundle output directory controlled by the caller
	if err != nil {
		return "", fmt.Errorf("reading bundle dir: %w", err)
	}

	// Collect file names (not MANIFEST.md itself; skip subdirs at this level).
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			// Walk into subdirectories to enumerate their files too.
			subPath := filepath.Join(outDir, e.Name())
			subEntries, subErr := os.ReadDir(subPath) //nolint:gosec // subPath is within outDir
			if subErr != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() {
					names = append(names, filepath.Join(e.Name(), se.Name()))
				}
			}
			continue
		}
		if e.Name() == fileManifest {
			continue // exclude self
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	heading := appName
	if heading == "" {
		heading = appPlaceholder
	}

	var b strings.Builder
	b.WriteString("# " + heading + " bundle manifest\n")
	b.WriteString("\n")
	b.WriteString("Generated by vibew " + manifestVersion + " at " + time.Now().UTC().Format(time.RFC3339) + ".\n")
	b.WriteString("\n")
	for _, name := range names {
		desc, ok := manifestFileDescriptions[name]
		if !ok {
			// Use the base filename for lookup on nested paths.
			desc = manifestFileDescriptions[filepath.Base(name)]
		}
		if desc == "" {
			desc = "bundle artifact"
		}
		b.WriteString("- " + name + " — " + desc + "\n")
	}

	return b.String(), nil
}
