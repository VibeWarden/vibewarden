package config

import (
	"strconv"
	"strings"
)

// goMemLimitFraction is the fraction of the container memory cap handed to the
// Go runtime as GOMEMLIMIT. The remaining headroom covers non-heap allocation
// (goroutine stacks, runtime metadata, cgo) so that approaching the cap makes
// the GC work harder rather than getting the container OOM-killed.
const goMemLimitFraction = 0.9

// ParseMemLimit parses a memory limit into a byte count. It accepts both
// VibeWarden's byte-size syntax ("512MB", "1GB") and Docker's single-letter
// shorthand ("512M", "1g"), which is what every Docker Compose example writes.
// An empty string or "0" returns 0, meaning "no limit".
//
// Examples:
//
//	ParseMemLimit("512MB") → 536870912, nil
//	ParseMemLimit("512M")  → 536870912, nil
//	ParseMemLimit("1g")    → 1073741824, nil
//	ParseMemLimit("0")     → 0, nil
func ParseMemLimit(s string) (int64, error) {
	return ParseBodySize(normalizeMemUnit(s))
}

// normalizeMemUnit rewrites a trailing bare k/m/g/t unit (Docker shorthand)
// into the KB/MB/GB/TB spelling that ParseBodySize understands. Anything else
// is returned untouched so ParseBodySize produces its own error message.
func normalizeMemUnit(s string) string {
	t := strings.TrimSpace(s)
	if len(t) < 2 {
		return t
	}
	last := t[len(t)-1]
	switch last {
	case 'k', 'K', 'm', 'M', 'g', 'G', 't', 'T':
	default:
		return t
	}
	// Only a digit may precede the unit; "1MB" or "1kb" must be left alone.
	prev := t[len(t)-2]
	if prev < '0' || prev > '9' {
		return t
	}
	return t + "B"
}

// ComposeResourceLimits is the render-ready view of the vibewarden sidecar
// container's resource caps. Every field is a string and an empty string means
// "omit this key from the generated compose file", so templates need only a
// plain {{ if }} guard and never have to reason about zero values (Go templates
// treat the string "0" as truthy).
type ComposeResourceLimits struct {
	// MemLimitBytes is the compose `mem_limit` value as a decimal byte count,
	// e.g. "536870912". Empty when the memory cap is disabled.
	MemLimitBytes string

	// MemLimitDisplay echoes the configured value, e.g. "512MB". Rendered as a
	// trailing comment so the generated file stays human-readable.
	MemLimitDisplay string

	// GoMemLimit is the GOMEMLIMIT environment value for the sidecar process,
	// e.g. "483183820B" (90% of MemLimitBytes). Empty when the memory cap is
	// disabled.
	GoMemLimit string

	// CPULimit is the compose `cpus` value in cores, e.g. "1" or "0.5".
	// Empty when the CPU cap is disabled.
	CPULimit string

	// PidsLimit is the compose `pids_limit` value, e.g. "200".
	// Empty when the PID cap is disabled.
	PidsLimit string
}

// ResourceLimits returns the render-ready compose resource caps for the
// vibewarden sidecar service. A disabled (zero or empty) cap yields empty
// strings so the corresponding compose key is omitted entirely rather than
// emitted as an explicit 0.
//
// A malformed MemLimit yields empty memory fields: validation rejects such a
// value long before rendering, so that path is a tripwire, not a behaviour.
func (c ServerConfig) ResourceLimits() ComposeResourceLimits {
	var out ComposeResourceLimits

	if bytes, err := ParseMemLimit(c.MemLimit); err == nil && bytes > 0 {
		out.MemLimitBytes = strconv.FormatInt(bytes, 10)
		out.MemLimitDisplay = strings.TrimSpace(c.MemLimit)
		out.GoMemLimit = strconv.FormatInt(int64(float64(bytes)*goMemLimitFraction), 10) + "B"
	}
	if c.CPULimit > 0 {
		out.CPULimit = strconv.FormatFloat(c.CPULimit, 'f', -1, 64)
	}
	if c.PidsLimit > 0 {
		out.PidsLimit = strconv.Itoa(c.PidsLimit)
	}

	return out
}
