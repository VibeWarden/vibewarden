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
)

// bundleExtraFiles are the file names written by writeBundleExtras. They
// are enumerated here so tests and documentation can agree on the exact
// artifact set.
const (
	fileSampleEnv = "sample.env"
	fileDotEnv    = ".env"
	fileReadme    = "README.md"
	fileImageTar  = "image.tar"
)

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

	// README.md — always overwrite.
	readmeBody := renderBundleReadme(projectName, opts.SkipImage)
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

// renderBundleReadme produces the README.md shipped inside the bundle.
// Output is stable for a given projectName and fits under 70 lines.
// The Deploy section describes the deploy contract as pure instruction —
// no shell snippets, no scp/ssh/docker command literals — per
// CLAUDE.md §Artifact policy. The skipImage parameter is accepted for
// API compatibility but both image modes are covered by the single
// "if image.tar is present" prose.
func renderBundleReadme(projectName string, _ bool) string {
	return "# " + projectName + " deploy bundle\n" +
		"\n" +
		"## What this is\n" +
		"\n" +
		"A self-contained Docker Compose bundle generated by `vibew bundle`. Everything needed to run " + projectName + " on a remote host with Docker installed ships inside this directory — no source code, no toolchains, no vibew binary required on the server.\n" +
		"\n" +
		"## Deploy\n" +
		"\n" +
		"This bundle is everything needed to run on a remote host with Docker installed. Copy the contents of this directory to a path on the host, load the image (or pull it from your registry), bring the stack up with Docker Compose, and verify with a request to `https://yourdomain/_vibewarden/health`.\n" +
		"\n" +
		"Two things easy to get wrong: the remote directory must exist before you copy (create it on the host first), and the healthcheck port in production is **443** (TLS), not the dev port 8443.\n" +
		"\n" +
		"Image-load mode: if `image.tar` is present, load it into Docker on the host before starting the stack. Registry-pull mode (built with `--skip-image`): the compose file has `image:` set; pulling from the registry is optional if it is reachable at start time.\n" +
		"\n" +
		"**Upgrading from a previous deployment?** If your old stack ran under the project name `vibewarden-app`, run `docker compose -p vibewarden-app down` on the remote ONCE to remove orphan containers/networks before bringing up the new stack. The new stack uses your `app.name` for the project name.\n" +
		"\n" +
		"## What's in this bundle\n" +
		"\n" +
		"| File | Purpose |\n" +
		"|---|---|\n" +
		"| `docker-compose.yml` | Pinned, deterministic. Do not edit by hand. |\n" +
		"| `vibewarden.yaml` | Merged production config. |\n" +
		"| `image.tar` | Your app image. (Omitted in registry-pull mode — see `--skip-image`.) |\n" +
		"| `.env` | Generated environment variables. Preserved across re-bundles. |\n" +
		"| `sample.env` | Template for hand-edits, regenerated each bundle. |\n" +
		"| `README.md` | This file. |\n" +
		"\n" +
		"## Secrets\n" +
		"\n" +
		"This bundle contains files with credentials: `.env`, `.credentials`, and anything under `kratos/`. They will land on the remote host once you copy the bundle there. If the destination is shared or the transport is untrusted, generate fresh credentials on the host instead rather than shipping them from your local machine.\n" +
		"\n" +
		"## Rebuild for a different arch\n" +
		"\n" +
		"The `image.tar` inside this bundle (when present) matches the architecture of whatever machine built it. If your local laptop is arm64 and your VPS is amd64, rerun `vibew build --platform linux/amd64` before `vibew bundle`. Edit `.env` to change environment variables; it is preserved across `vibew bundle` runs unless you pass `--overwrite`.\n"
}
