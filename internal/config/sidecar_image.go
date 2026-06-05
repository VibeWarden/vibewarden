package config

import "regexp"

// sidecarImageRepo is the canonical container registry path for the
// VibeWarden sidecar image. It is the single source of truth used by
// SidecarImageRef — do not duplicate this constant elsewhere.
const sidecarImageRepo = "ghcr.io/vibewarden/vibewarden"

// releaseVersionRE matches a semver string with no leading "v":
//
//	^\d+\.\d+\.\d+([-.].+)?$
//
// A release build produced by goreleaser sets main.version to the tag value
// with the leading "v" stripped (e.g. tag v0.20.0 → main.version "0.20.0").
// The image tag on ghcr.io uses the same stripped value verbatim, so
// sidecarImageRepo+":"+version is the correct fully-qualified image reference
// for any version string that matches this expression.
//
// Values that do NOT match (dev, v0.20.0-5-gabc1234, v0.20.0, dirty, …)
// signal a non-release build; SidecarImageRef falls back to :latest with
// pull_policy: always.
var releaseVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+([-.].+)?$`)

// isReleaseVersion reports whether v is a goreleaser-produced release version
// (semver, no leading "v", no git-describe suffix, no "-dirty").
//
// Examples:
//
//	isReleaseVersion("0.20.0")        → true
//	isReleaseVersion("0.20.0-rc.1")   → true  (pre-release suffix via dash)
//	isReleaseVersion("1.0.0-beta.2")  → true
//	isReleaseVersion("dev")           → false
//	isReleaseVersion("v0.20.0")       → false  (leading v → not a release version)
//	isReleaseVersion("v0.20.0-5-gabc") → false
//	isReleaseVersion("")              → false
func isReleaseVersion(v string) bool {
	return releaseVersionRE.MatchString(v)
}

// SidecarImageRef computes the Docker image reference and optional pull policy
// for the VibeWarden sidecar based on the CLI build version.
//
// For a release build (version matches ^\d+\.\d+\.\d+([-.].+)?$, no leading
// "v", as set by goreleaser):
//   - image = "ghcr.io/vibewarden/vibewarden:<version>"
//   - pullPolicy = "" (omit pull_policy; the pinned tag is immutable — no
//     forced pull → no latency on `vibew dev`, airgap-friendly once pulled)
//
// For any non-release build (dev, git-describe like v0.20.0-5-gabc1234,
// anything with a leading "v", the string "dirty", or empty):
//   - image = "ghcr.io/vibewarden/vibewarden:latest"
//   - pullPolicy = "always" (keeps contributors current; no :dev image is
//     published so :latest is the only valid target for source builds)
//
// The goreleaser build invariant: goreleaser uses the same {{ .Version }}
// (stripped of its leading "v") for both main.version and the image tag,
// so `sidecarImageRepo + ":" + version` is the correct verbatim reference
// — no normalization is needed.
func SidecarImageRef(version string) (image, pullPolicy string) {
	if isReleaseVersion(version) {
		return sidecarImageRepo + ":" + version, ""
	}
	return sidecarImageRepo + ":latest", "always"
}
