// Package tlsstatus provides the application service for inspecting remote TLS
// certificates via SSH. It runs openssl s_client on the remote host and parses
// the output into a domain CertInfo value object.
package tlsstatus

import (
	"fmt"
	"strings"
	"time"

	tlsdomain "github.com/vibewarden/vibewarden/internal/domain/tls"
)

// opensslTimeLayout is the time format used by openssl x509 -dates output.
// Example: "Apr 20 12:00:00 2026 GMT"
const opensslTimeLayout = "Jan  2 15:04:05 2006 GMT"

// opensslTimeLayoutSingleDigit handles single-digit days without leading space.
// Example: "Apr 2 12:00:00 2026 GMT"
const opensslTimeLayoutSingleDigit = "Jan 2 15:04:05 2006 GMT"

// ParseOpenSSLOutput parses the combined text output of:
//
//	echo | openssl s_client -connect host:443 -servername host 2>/dev/null | openssl x509 -noout -subject -issuer -dates -serial -ext subjectAltName
//
// It extracts subject, issuer, validity dates, serial, and SANs into a CertInfo.
func ParseOpenSSLOutput(output string) (tlsdomain.CertInfo, error) {
	var (
		subject   string
		issuer    string
		notBefore time.Time
		notAfter  time.Time
		serial    string
		sans      []string
	)

	lines := strings.Split(output, "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "subject="):
			subject = strings.TrimSpace(strings.TrimPrefix(line, "subject="))

		case strings.HasPrefix(line, "issuer="):
			issuer = strings.TrimSpace(strings.TrimPrefix(line, "issuer="))

		case strings.HasPrefix(line, "notBefore="):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "notBefore="))
			t, err := parseOpenSSLTime(raw)
			if err != nil {
				return tlsdomain.CertInfo{}, fmt.Errorf("parsing notBefore %q: %w", raw, err)
			}
			notBefore = t

		case strings.HasPrefix(line, "notAfter="):
			raw := strings.TrimSpace(strings.TrimPrefix(line, "notAfter="))
			t, err := parseOpenSSLTime(raw)
			if err != nil {
				return tlsdomain.CertInfo{}, fmt.Errorf("parsing notAfter %q: %w", raw, err)
			}
			notAfter = t

		case strings.HasPrefix(line, "serial="):
			serial = strings.TrimSpace(strings.TrimPrefix(line, "serial="))

		case strings.Contains(line, "Subject Alternative Name"):
			// The SANs are on the next line(s) as comma-separated DNS:name entries.
			if i+1 < len(lines) {
				sans = parseSANLine(lines[i+1])
			}
		}
	}

	if subject == "" {
		return tlsdomain.CertInfo{}, fmt.Errorf("subject not found in openssl output")
	}
	if issuer == "" {
		return tlsdomain.CertInfo{}, fmt.Errorf("issuer not found in openssl output")
	}
	if notBefore.IsZero() {
		return tlsdomain.CertInfo{}, fmt.Errorf("notBefore not found in openssl output")
	}
	if notAfter.IsZero() {
		return tlsdomain.CertInfo{}, fmt.Errorf("notAfter not found in openssl output")
	}

	return tlsdomain.NewCertInfo(subject, issuer, notBefore, notAfter, sans, serial)
}

// parseOpenSSLTime parses a time string from openssl x509 output.
// OpenSSL uses a format like "Apr 20 12:00:00 2026 GMT" but may also
// produce single-digit days like "Apr  2 12:00:00 2026 GMT" (with two
// spaces) or "Apr 2 12:00:00 2026 GMT" (after trimming).
func parseOpenSSLTime(raw string) (time.Time, error) {
	// Normalize multiple spaces to single space for consistent parsing.
	normalized := normalizeSpaces(raw)

	t, err := time.Parse(opensslTimeLayout, normalized)
	if err == nil {
		return t, nil
	}

	t, err2 := time.Parse(opensslTimeLayoutSingleDigit, normalized)
	if err2 == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("cannot parse %q as openssl time: %w", raw, err)
}

// normalizeSpaces collapses runs of whitespace into single spaces.
func normalizeSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// parseSANLine parses the SAN line from openssl x509 -ext subjectAltName output.
// The line looks like: "DNS:example.com, DNS:www.example.com, DNS:*.example.com"
func parseSANLine(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	parts := strings.Split(line, ",")
	var sans []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "DNS:") {
			name := strings.TrimPrefix(part, "DNS:")
			name = strings.TrimSpace(name)
			if name != "" {
				sans = append(sans, name)
			}
		}
	}
	return sans
}
