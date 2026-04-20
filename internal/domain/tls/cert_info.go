// Package tls provides domain value objects for TLS certificate inspection.
package tls

import (
	"errors"
	"fmt"
	"time"
)

// CertInfo is an immutable value object representing the relevant fields of a
// remote TLS certificate as presented during a TLS handshake. It is produced
// by parsing the text output of an `openssl s_client` invocation.
type CertInfo struct {
	subject   string
	issuer    string
	notBefore time.Time
	notAfter  time.Time
	sans      []string
	serial    string
}

// NewCertInfo creates a CertInfo value object. Subject and issuer are required.
// notAfter must be after notBefore.
func NewCertInfo(subject, issuer string, notBefore, notAfter time.Time, sans []string, serial string) (CertInfo, error) {
	if subject == "" {
		return CertInfo{}, errors.New("certificate subject cannot be empty")
	}
	if issuer == "" {
		return CertInfo{}, errors.New("certificate issuer cannot be empty")
	}
	if !notAfter.After(notBefore) {
		return CertInfo{}, fmt.Errorf("notAfter (%s) must be after notBefore (%s)", notAfter, notBefore)
	}

	// Defensive copy of SANs slice.
	sansCopy := make([]string, len(sans))
	copy(sansCopy, sans)

	return CertInfo{
		subject:   subject,
		issuer:    issuer,
		notBefore: notBefore,
		notAfter:  notAfter,
		sans:      sansCopy,
		serial:    serial,
	}, nil
}

// Subject returns the certificate subject (e.g. "CN=example.com").
func (c CertInfo) Subject() string { return c.subject }

// Issuer returns the certificate issuer (e.g. "O=Let's Encrypt, CN=R3").
func (c CertInfo) Issuer() string { return c.issuer }

// NotBefore returns the start of the certificate's validity period.
func (c CertInfo) NotBefore() time.Time { return c.notBefore }

// NotAfter returns the end of the certificate's validity period.
func (c CertInfo) NotAfter() time.Time { return c.notAfter }

// SANs returns the Subject Alternative Names (DNS names) on the certificate.
// The returned slice is a copy; mutating it does not affect the value object.
func (c CertInfo) SANs() []string {
	out := make([]string, len(c.sans))
	copy(out, c.sans)
	return out
}

// Serial returns the certificate serial number as a hex string.
func (c CertInfo) Serial() string { return c.serial }

// IsExpired returns true when the certificate's notAfter time is in the past
// relative to the provided reference time.
func (c CertInfo) IsExpired(now time.Time) bool {
	return now.After(c.notAfter)
}

// DaysUntilExpiry returns the number of days remaining until the certificate
// expires, relative to the provided reference time. Returns 0 if already
// expired.
func (c CertInfo) DaysUntilExpiry(now time.Time) int {
	if c.IsExpired(now) {
		return 0
	}
	d := c.notAfter.Sub(now)
	return int(d.Hours() / 24)
}

// ExpiryStatus returns a human-readable status string describing the
// certificate's expiry relative to the provided reference time.
func (c CertInfo) ExpiryStatus(now time.Time) string {
	if c.IsExpired(now) {
		return "EXPIRED"
	}
	days := c.DaysUntilExpiry(now)
	if days <= 7 {
		return fmt.Sprintf("CRITICAL (%d days)", days)
	}
	if days <= 30 {
		return fmt.Sprintf("WARNING (%d days)", days)
	}
	return fmt.Sprintf("OK (%d days)", days)
}
