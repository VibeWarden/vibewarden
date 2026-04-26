package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/vibewarden/vibewarden/internal/ports"
)

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
	// content digests. A digest file was present and valid.
	FreshnessModeDigest FreshnessMode = "digest"

	// FreshnessModeTime means the verdict was computed by comparing file mtimes
	// against the image creation timestamp. Used when no valid digest file
	// exists (first run, corrupt file, schema mismatch).
	FreshnessModeTime FreshnessMode = "mtime"
)

// FreshnessVerdict is the outcome of the staleness walk for a given image.
type FreshnessVerdict struct {
	// Stale is true when the source inputs have changed since the last bundle.
	Stale bool
	// ChangedCount is the number of source files with mtime strictly after
	// the image creation time. Populated only when Mode == FreshnessModeTime
	// and Stale == true. Zero in digest mode.
	ChangedCount int
	// NewestMTime is the mtime of the file that tripped the STALE verdict.
	// Zero value when !Stale or when Mode == FreshnessModeDigest.
	NewestMTime time.Time
	// Mode describes which algorithm produced this verdict.
	// FreshnessModeDigest when a content-hash comparison was used;
	// FreshnessModeTime when mtime comparison was the fallback.
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
	// ProjectRoot is the directory walked for source-file mtimes.
	ProjectRoot string
	// TargetPlatform is the expected deployment platform (e.g. "linux/amd64").
	// When empty, defaultTargetPlatform ("linux/amd64") is used.
	TargetPlatform string
	// AllowStale, when true, suppresses the STALE warning in the rendered block.
	AllowStale bool
	// Inspector is the ImageInspector port to use. Must not be nil.
	Inspector ports.ImageInspector
	// Walker is the StalenessWalker to use. Must not be nil.
	Walker StalenessWalker
}

// CheckImageHealth runs the full image health pipeline:
//  1. Calls Inspector.Inspect to retrieve image metadata.
//  2. On ErrImageNotFound or ErrDockerUnavailable, returns the error unwrapped
//     so the CLI layer can map it to the correct exit code.
//  3. Walks the project root via Walker.NewestMTime to compute a FreshnessVerdict.
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

	// Freshness: prefer content-hash comparison; fall back to mtime when no
	// valid digest file exists (first run, corrupt file, schema mismatch).
	var verdict FreshnessVerdict
	if opts.Walker != nil && opts.ProjectRoot != "" {
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
					verdict = FreshnessVerdict{
						Stale: true,
						Mode:  FreshnessModeDigest,
					}
				}
			} else {
				// Digest computation failed — fall through to mtime.
				ok = false
			}
		}
		if !ok {
			// mtime fallback: digest file missing, corrupt, wrong schema, or
			// compute error. Preserves pre-#1146 behaviour exactly.
			newest, count, walkErr := opts.Walker.NewestMTime(opts.ProjectRoot, info.Created)
			if walkErr == nil {
				verdict = FreshnessVerdict{
					Stale:        count > 0,
					ChangedCount: count,
					NewestMTime:  newest,
					Mode:         FreshnessModeTime,
				}
			}
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
// Example output:
//
//	Image health
//	  Tag:          qr-van-gogh-app:latest
//	  Digest:       sha256:abc123…
//	  Arch:         linux/arm64
//	  Created:      2026-04-19 14:02:11 UTC (1 day ago)
//	  Size:         162.4 MB
//	  Target:       linux/amd64  (override with --target-platform)
//	  Freshness:    STALE — 12 source files changed since image was built
//	  Warnings:
//	    - image arch linux/arm64 != target linux/amd64 (rebuild: vibew build --platform linux/amd64)
//	    - image is stale (pass --allow-stale to suppress, or rebuild)
func RenderImageHealth(out io.Writer, h ImageHealth) {
	age := formatAge(time.Since(h.Image.Created))
	createdStr := h.Image.Created.Format("2006-01-02 15:04:05 UTC")
	sizeStr := formatBytes(h.Image.SizeBytes)
	freshness := freshnessLabel(h)
	warnings := collectWarnings(h)

	fmt.Fprintf(out, "Image health\n")
	fmt.Fprintf(out, "  Tag:          %s\n", h.Image.Tag)
	fmt.Fprintf(out, "  Digest:       %s\n", h.Image.Digest)
	fmt.Fprintf(out, "  Arch:         %s\n", h.Image.Platform())
	fmt.Fprintf(out, "  Created:      %s (%s)\n", createdStr, age)
	fmt.Fprintf(out, "  Size:         %s\n", sizeStr)
	fmt.Fprintf(out, "  Target:       %s  (override with --target-platform)\n", h.Target)
	fmt.Fprintf(out, "  Freshness:    %s\n", freshness)
	if len(warnings) == 0 {
		fmt.Fprintf(out, "  Warnings:     none\n")
	} else {
		fmt.Fprintf(out, "  Warnings:\n")
		renderWarnings(out, warnings)
	}
}

// freshnessLabel returns the Freshness line value based on the health state.
func freshnessLabel(h ImageHealth) string {
	if !h.Freshness.Stale {
		return "FRESH"
	}
	if h.Freshness.Mode == FreshnessModeDigest {
		// Digest mode: file count is not meaningful.
		if h.AllowStale {
			return "FRESH (stale suppressed — source files changed since image was built)"
		}
		return "STALE — source files changed since image was built"
	}
	// mtime mode: include file count.
	if h.AllowStale {
		return fmt.Sprintf("FRESH (stale suppressed — %d source files changed since image was built)", h.Freshness.ChangedCount)
	}
	return fmt.Sprintf("STALE — %d source files changed since image was built", h.Freshness.ChangedCount)
}

// collectWarnings builds the ordered list of human-readable warning strings
// for the ImageHealth report.
func collectWarnings(h ImageHealth) []string {
	var warnings []string

	if h.ArchMismatch {
		warnings = append(warnings, fmt.Sprintf(
			"image arch %s != target %s (rebuild: vibew build --platform %s)",
			h.Image.Platform(), h.Target, h.Target,
		))
	}

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
