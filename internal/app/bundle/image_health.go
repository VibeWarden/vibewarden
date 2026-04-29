package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

// ErrPlatformMismatch is returned by runImageHealthCheck (and propagated by
// Service.Bundle) when the bundled image's architecture does not match the
// resolved deploy target. The CLI maps this to exit code 1.
//
// Note: local `docker image inspect` always returns a single architecture —
// the variant that was loaded into the local Docker daemon. There is no
// manifest-list case for vibew bundle; buildx --load writes only one arch.
var ErrPlatformMismatch = errors.New("image platform mismatch")

// defaultTargetPlatform is the expected deployment platform when no
// --target-platform flag is supplied. VPS targets are almost always amd64.
const defaultTargetPlatform = "linux/amd64"

// legacyGenericTag is the old project-agnostic tag that v0.16.0 and earlier
// scaffold tooling hardcoded. When detected, a migration warning is emitted.
const legacyGenericTag = "vibewarden-app:latest"

// FreshnessMode describes how the freshness verdict was computed.
type FreshnessMode string

const (
	// FreshnessModeDigest means the verdict was computed by comparing SHA-256
	// content digests. A valid v2 digest file was present.
	FreshnessModeDigest FreshnessMode = "digest"

	// FreshnessModeBaseline means no prior digest was found (first run, wiped
	// .vibewarden/, corrupt file, or schema-version mismatch). The verdict is
	// always FRESH — the current digest is recorded as the new baseline for the
	// next comparison. No mtime fallback is performed.
	FreshnessModeBaseline FreshnessMode = "baseline"

	// FreshnessModeTime means the verdict was computed by comparing file mtimes
	// against the image creation timestamp.
	//
	// Deprecated: this mode was removed in #1223. The constant is retained so
	// callers that previously checked for it (e.g. existing tests) still compile.
	// It will never be set on a FreshnessVerdict produced by CheckImageHealth.
	FreshnessModeTime FreshnessMode = "mtime"
)

// FreshnessVerdict is the outcome of the staleness walk for a given image.
type FreshnessVerdict struct {
	// Stale is true when the source inputs have changed since the last bundle.
	Stale bool
	// ChangedCount is the total number of changed paths detected. Populated only
	// when Mode == FreshnessModeDigest and Stale == true.
	ChangedCount int
	// ChangedPaths is the list of changed paths (capped at maxChangedPathsRendered).
	// Only populated when Mode == FreshnessModeDigest and Stale == true.
	ChangedPaths []ChangedPath
	// NewestMTime was used by the removed mtime path (FreshnessModeTime). It is
	// always the zero time in digest and baseline modes.
	//
	// Deprecated: field is kept for API compatibility; always zero as of #1223.
	NewestMTime time.Time
	// Mode describes which algorithm produced this verdict.
	Mode FreshnessMode
}

// ImageHealth is the complete health report rendered to stdout before any
// bundle files are written.
type ImageHealth struct {
	// Image is the raw metadata returned by ImageInspector.
	Image ports.ImageInfo
	// Target is the resolved target platform string (e.g. "linux/amd64").
	Target string
	// Freshness is the result of the staleness walk.
	Freshness FreshnessVerdict
	// ArchMismatch is true when Image.Platform() != Target.
	ArchMismatch bool
	// LegacyTag is true when the image tag equals the old generic tag
	// "vibewarden-app:latest".
	LegacyTag bool
	// AllowStale, when true, suppresses the STALE label in the rendered block.
	// The freshness walk result is still populated; only the display changes.
	AllowStale bool
}

// CheckImageHealthOptions holds the inputs for CheckImageHealth.
type CheckImageHealthOptions struct {
	// ImageTag is the fully qualified image reference to inspect.
	ImageTag string
	// ProjectRoot is the directory walked for content-hash comparison.
	ProjectRoot string
	// TargetPlatform is the expected deployment platform (e.g. "linux/amd64").
	// When empty, defaultTargetPlatform ("linux/amd64") is used.
	TargetPlatform string
	// AllowStale, when true, suppresses the STALE warning in the rendered block.
	AllowStale bool
	// Inspector is the ImageInspector port to use. Must not be nil.
	Inspector ports.ImageInspector
	// Walker is the StalenessWalker to use. Accepted for API compatibility with
	// existing call sites; not used for the freshness verdict since #1223 (the
	// verdict is now purely content-hash based). May be nil.
	//
	// Deprecated: the Walker parameter is ignored for freshness computation.
	// The field will be removed in a future release.
	Walker StalenessWalker
}

// CheckImageHealth runs the full image health pipeline:
//  1. Calls Inspector.Inspect to retrieve image metadata.
//  2. On ErrImageNotFound or ErrDockerUnavailable, returns the error unwrapped
//     so the CLI layer can map it to the correct exit code.
//  3. Reads the prior digest file (<projectRoot>/.vibewarden/.input-digest).
//     - If a valid v2 digest is found, computes the current digest and diffs.
//     - If the file is missing, corrupt, or carries a different schema version,
//     the verdict is FRESH with Mode=FreshnessModeBaseline (first-run
//     baseline). No mtime fallback is performed.
//  4. Assembles and returns the ImageHealth value.
//
// CheckImageHealth never writes any output — callers render the result via
// RenderImageHealth.
func CheckImageHealth(ctx context.Context, opts CheckImageHealthOptions) (ImageHealth, error) {
	target := opts.TargetPlatform
	if target == "" {
		target = defaultTargetPlatform
	}

	info, err := opts.Inspector.Inspect(ctx, opts.ImageTag)
	if err != nil {
		// Surface sentinel errors unwrapped so CLI can switch on them.
		if errors.Is(err, ports.ErrImageNotFound) || errors.Is(err, ports.ErrDockerUnavailable) {
			return ImageHealth{}, err
		}
		return ImageHealth{}, fmt.Errorf("inspecting image: %w", err)
	}

	var verdict FreshnessVerdict
	if opts.ProjectRoot != "" {
		prior, ok := readInputDigest(opts.ProjectRoot)
		if ok {
			// Digest path: compute current digest and compare.
			current, digestErr := computeInputDigest(opts.ProjectRoot)
			if digestErr == nil {
				if current.Digest == prior.Digest {
					verdict = FreshnessVerdict{
						Stale: false,
						Mode:  FreshnessModeDigest,
					}
				} else {
					changed := diffDigests(prior, current)
					total := len(changed)
					capped := changed
					if len(capped) > maxChangedPathsRendered {
						capped = capped[:maxChangedPathsRendered]
					}
					verdict = FreshnessVerdict{
						Stale:        true,
						Mode:         FreshnessModeDigest,
						ChangedCount: total,
						ChangedPaths: capped,
					}
				}
			} else {
				// Digest computation failed — treat as baseline (FRESH).
				slog.Debug("input-digest: compute failed, reporting baseline", "err", digestErr)
				verdict = FreshnessVerdict{Stale: false, Mode: FreshnessModeBaseline}
			}
		} else {
			// No valid prior digest: first-run baseline — always FRESH.
			verdict = FreshnessVerdict{Stale: false, Mode: FreshnessModeBaseline}
		}
	}

	return ImageHealth{
		Image:        info,
		Target:       target,
		Freshness:    verdict,
		ArchMismatch: info.Platform() != "" && info.Platform() != target,
		LegacyTag:    opts.ImageTag == legacyGenericTag,
		AllowStale:   opts.AllowStale,
	}, nil
}

// RenderImageHealth formats an ImageHealth into the stable key-value block
// that is printed to stdout before any bundle files are written.
//
// The block is always printed, even when there are zero warnings — agents
// depend on the stable layout. ANSI colour is never used.
//
// When the image arch does not match the target platform, RenderImageHealth
// still renders the full block (showing Arch and Target for context), and then
// the caller (runImageHealthCheck) returns ErrPlatformMismatch with the
// actionable rebuild instruction.
//
// Example output (stale image with changed paths):
//
//	Image health
//	  Tag:          qr-van-gogh-app:latest
//	  Digest:       sha256:abc123…
//	  Arch:         linux/amd64
//	  Created:      2026-04-19 14:02:11 UTC (1 day ago)
//	  Size:         162.4 MB
//	  Target:       linux/amd64  (override with --target-platform)
//	  Freshness:    STALE — source files changed since image was built:
//	                  - main.go (modified)
//	                  - newfile.txt (added)
//	  Warnings:
//	    - image is stale (pass --allow-stale to suppress, or rebuild)
func RenderImageHealth(out io.Writer, h ImageHealth) {
	age := formatAge(time.Since(h.Image.Created))
	createdStr := h.Image.Created.Format("2006-01-02 15:04:05 UTC")
	sizeStr := formatBytes(h.Image.SizeBytes)
	warnings := collectWarnings(h)

	fmt.Fprintf(out, "Image health\n")
	fmt.Fprintf(out, "  Tag:          %s\n", h.Image.Tag)
	fmt.Fprintf(out, "  Digest:       %s\n", h.Image.Digest)
	fmt.Fprintf(out, "  Arch:         %s\n", h.Image.Platform())
	fmt.Fprintf(out, "  Created:      %s (%s)\n", createdStr, age)
	fmt.Fprintf(out, "  Size:         %s\n", sizeStr)
	fmt.Fprintf(out, "  Target:       %s  (override with --target-platform)\n", h.Target)
	renderFreshnessLine(out, h)
	if len(warnings) == 0 {
		fmt.Fprintf(out, "  Warnings:     none\n")
	} else {
		fmt.Fprintf(out, "  Warnings:\n")
		renderWarnings(out, warnings)
	}
}

// renderFreshnessLine writes the "Freshness:" line (and optional path list) to
// out. It is extracted from RenderImageHealth to keep the rendering logic
// testable in isolation.
func renderFreshnessLine(out io.Writer, h ImageHealth) {
	label := freshnessLabel(h)

	if h.Freshness.Stale && !h.AllowStale &&
		h.Freshness.Mode == FreshnessModeDigest &&
		len(h.Freshness.ChangedPaths) > 0 {
		// Multi-line form: header + path list.
		fmt.Fprintf(out, "  Freshness:    %s\n", label)
		for _, cp := range h.Freshness.ChangedPaths {
			fmt.Fprintf(out, "                  - %s (%s)\n", cp.Path, cp.Kind)
		}
		remaining := h.Freshness.ChangedCount - len(h.Freshness.ChangedPaths)
		if remaining > 0 {
			fmt.Fprintf(out, "                  (and %d more)\n", remaining)
		}
	} else {
		fmt.Fprintf(out, "  Freshness:    %s\n", label)
	}
}

// freshnessLabel returns the Freshness line value based on the health state.
func freshnessLabel(h ImageHealth) string {
	if !h.Freshness.Stale {
		switch h.Freshness.Mode {
		case FreshnessModeBaseline:
			return "FRESH (no prior baseline; digest written for next run)"
		default:
			return "FRESH"
		}
	}
	// Stale cases.
	if h.AllowStale {
		return "FRESH (stale suppressed — source files changed since image was built)"
	}
	return "STALE — source files changed since image was built"
}

// platformMismatchMessage returns the exact actionable error copy for an arch
// mismatch. The wording is pinned by tests — do not change it without updating
// the test golden strings.
//
// Example output:
//
//	image arch is linux/arm64, target is linux/amd64. Rebuild with: vibew build --platform linux/amd64
//	Then re-run: vibew bundle
func platformMismatchMessage(h ImageHealth) string {
	return fmt.Sprintf(
		"image arch is %s, target is %s. Rebuild with: vibew build --platform %s\nThen re-run: vibew bundle",
		h.Image.Platform(), h.Target, h.Target,
	)
}

// collectWarnings builds the ordered list of human-readable warning strings
// for the ImageHealth report. Arch mismatch is NOT included here — it is a
// hard failure returned by runImageHealthCheck before bundling proceeds.
func collectWarnings(h ImageHealth) []string {
	var warnings []string

	if h.Freshness.Stale && !h.AllowStale {
		warnings = append(warnings, "image is stale (pass --allow-stale to suppress, or rebuild)")
	}

	if h.LegacyTag {
		warnings = append(warnings, fmt.Sprintf(
			"legacy generic tag %q detected — run `vibew validate` for migration hint",
			legacyGenericTag,
		))
	}

	return warnings
}

// renderWarnings is an alias used by the io.Writer variant of RenderImageHealth
// to write warning lines with the correct indent.
func renderWarnings(w io.Writer, warnings []string) {
	for _, warn := range warnings {
		fmt.Fprintf(w, "    - %s\n", warn)
	}
}

// formatAge formats a duration into a human-readable age string (e.g.
// "3 days 2 hours ago", "5 minutes ago", "just now").
func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	}
	days := int(d.Hours() / 24)
	remaining := d - time.Duration(days)*24*time.Hour
	hours := int(remaining.Hours())
	if days == 1 && hours == 0 {
		return "1 day ago"
	}
	if hours == 0 {
		return fmt.Sprintf("%d days ago", days)
	}
	if days == 1 {
		if hours == 1 {
			return "1 day 1 hour ago"
		}
		return fmt.Sprintf("1 day %d hours ago", hours)
	}
	if hours == 1 {
		return fmt.Sprintf("%d days 1 hour ago", days)
	}
	return fmt.Sprintf("%d days %d hours ago", days, hours)
}

// formatBytes formats a byte count into a human-readable string (e.g. "162.4 MB").
func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
