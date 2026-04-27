// Package dockerfile provides a lightweight Dockerfile parser and lint rules
// for the VibeWarden contract checks (vibew doctor Dockerfile section).
//
// The parser is intentionally minimal: it extracts only the fields needed by
// the six contract rules — FROM stages, EXPOSE ports, HEALTHCHECK presence, and
// USER directives — without a full Dockerfile syntax tree. Multi-line
// continuation (\) is not supported; such lines are treated as malformed and
// skipped, matching the architect's directive established in #1171.
package dockerfile

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Stage represents a single FROM directive and its associated per-stage
// directives (USER) encountered before the next FROM.
type Stage struct {
	// Image is the base image name without registry prefix or tag (e.g. "golang").
	Image string
	// Tag is the image tag (e.g. "1.26-alpine"). Empty tag is normalised to "latest".
	Tag string
	// Digest is the content-addressable digest (e.g. "sha256:abc123"), if present.
	Digest string
	// Alias is the AS alias declared in the FROM line (e.g. "builder"), empty if none.
	Alias string
	// LineNum is the 1-based line number of the FROM directive.
	LineNum int
	// User is the last USER directive seen in this stage. Empty if no USER is set.
	User string
}

// ExposePort represents a single EXPOSE directive.
type ExposePort struct {
	// Port is the integer port number extracted from the EXPOSE token.
	Port int
	// LineNum is the 1-based line number of the EXPOSE directive.
	LineNum int
}

// Parsed is the output of Parse: a lightweight AST containing only the fields
// required by the six Dockerfile contract rules.
type Parsed struct {
	// Stages holds all FROM directives in source order. The final stage is
	// Stages[len(Stages)-1]; the builder stage (multi-stage) is Stages[0].
	Stages []Stage
	// Exposes holds every valid EXPOSE directive found, in source order.
	Exposes []ExposePort
	// HasHealthcheck is true when any HEALTHCHECK directive appears in any stage.
	HasHealthcheck bool
	// FinalUser is the User value from the last Stage, or empty if no USER was set.
	// Kept as a convenience accessor so callers need not index Stages.
	FinalUser string
	// IsMultiStage is true when the Dockerfile contains two or more FROM directives.
	IsMultiStage bool
}

// Parse reads a Dockerfile from r and returns a Parsed struct.
// Parsing errors (e.g. unreadable input) are returned as a non-nil error.
// Structural anomalies (malformed EXPOSE, continuation lines) are silently
// skipped per the project convention.
func Parse(r io.Reader) (Parsed, error) {
	scanner := bufio.NewScanner(r)
	var p Parsed
	// Track the index of the current in-progress stage so we can attach USER.
	currentStage := -1
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and blank lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip continuation lines (not supported).
		if strings.HasSuffix(line, "\\") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		instruction := strings.ToLower(fields[0])

		switch instruction {
		case "from":
			s, err := parseFrom(fields[1:], lineNum)
			if err != nil {
				// Malformed FROM — skip.
				continue
			}
			p.Stages = append(p.Stages, s)
			currentStage = len(p.Stages) - 1

		case "expose":
			if len(fields) < 2 {
				continue
			}
			port, ok := parseExposePort(fields[1])
			if !ok {
				continue
			}
			p.Exposes = append(p.Exposes, ExposePort{Port: port, LineNum: lineNum})

		case "healthcheck":
			p.HasHealthcheck = true

		case "user":
			if len(fields) < 2 || currentStage < 0 {
				continue
			}
			p.Stages[currentStage].User = fields[1]
		}
	}

	if err := scanner.Err(); err != nil {
		return Parsed{}, fmt.Errorf("scanning dockerfile: %w", err)
	}

	p.IsMultiStage = len(p.Stages) >= 2
	if len(p.Stages) > 0 {
		p.FinalUser = p.Stages[len(p.Stages)-1].User
	}
	return p, nil
}

// parseFrom parses the argument tokens of a FROM instruction (everything after
// the "FROM" keyword). It handles:
//   - Optional --platform=... flags (stripped before parsing).
//   - Optional AS <alias> suffix.
//   - Registry prefixes (stripped: detected as a path component with '.' or ':').
//   - Image@digest and image:tag forms.
//   - Empty tag normalised to "latest".
func parseFrom(tokens []string, lineNum int) (Stage, error) {
	if len(tokens) == 0 {
		return Stage{}, fmt.Errorf("FROM with no argument at line %d", lineNum)
	}

	// Strip --platform=... and similar flags.
	var filtered []string
	for _, t := range tokens {
		if strings.HasPrefix(t, "--") {
			continue
		}
		filtered = append(filtered, t)
	}
	if len(filtered) == 0 {
		return Stage{}, fmt.Errorf("FROM with only flags at line %d", lineNum)
	}

	imageRef := filtered[0]
	alias := ""

	// Extract AS alias (FROM <image> AS <alias>).
	if len(filtered) >= 3 && strings.ToLower(filtered[1]) == "as" {
		alias = filtered[2]
	}

	// Strip registry prefix: a leading path component that contains '.' or ':' is
	// a registry (e.g. "gcr.io", "registry:5000"). Split on the first '/'.
	if idx := strings.Index(imageRef, "/"); idx > 0 {
		prefix := imageRef[:idx]
		if strings.ContainsAny(prefix, ".:") {
			imageRef = imageRef[idx+1:]
		}
	}

	// Split on '@' first (digest pins win over tag).
	digest := ""
	if idx := strings.Index(imageRef, "@"); idx >= 0 {
		digest = imageRef[idx+1:] // e.g. "sha256:abc..."
		imageRef = imageRef[:idx]
	}

	// Split image from tag on ':'.
	image := imageRef
	tag := ""
	if idx := strings.Index(imageRef, ":"); idx >= 0 {
		image = imageRef[:idx]
		tag = imageRef[idx+1:]
	}

	// Normalise empty tag.
	if tag == "" && digest == "" {
		tag = "latest"
	}

	return Stage{
		Image:   image,
		Tag:     tag,
		Digest:  digest,
		Alias:   alias,
		LineNum: lineNum,
	}, nil
}

// parseExposePort extracts an integer port from an EXPOSE token.
// Strips protocol suffixes (e.g. "/tcp", "/udp"). Returns (0, false) for
// malformed tokens.
func parseExposePort(token string) (int, bool) {
	if idx := strings.Index(token, "/"); idx >= 0 {
		token = token[:idx]
	}
	port, err := strconv.Atoi(token)
	if err != nil || port < 1 || port > 65535 {
		return 0, false
	}
	return port, true
}
