package tlspreflight

import (
	"strings"
	"time"
)

// CrtShRecord is the projection of a crt.sh JSON row that the preflight parser
// consumes. It mirrors ports.CrtShRecord — defined here in the domain layer to
// keep the domain package free of port-layer imports.
type CrtShRecord struct {
	// NotBefore is the certificate's not-before timestamp as reported by crt.sh.
	NotBefore time.Time
	// IssuerName is the full issuer distinguished name string from crt.sh.
	// Example: "C=US, O=Let's Encrypt, CN=R3"
	IssuerName string
	// CommonName is the certificate's CN field.
	CommonName string
	// NameValue is the SAN / CN value from crt.sh.
	NameValue string
}

// isLEIssuer reports whether the record was issued by Let's Encrypt.
// The check is case-insensitive and matches any LE sub-CA name
// (R3, R10, E1, E5, etc.).
func isLEIssuer(rec CrtShRecord) bool {
	return strings.Contains(strings.ToLower(rec.IssuerName), "let's encrypt")
}

// CountIssuedSince counts LE-issued certificates whose not_before is strictly
// after threshold (exclusive lower bound). It also returns the oldest
// not_before timestamp among the matching records — this is the timestamp
// used to compute when the oldest slot in the current window expires
// (oldest + BudgetWindow).
//
// Records with a zero NotBefore are silently skipped (consistent with the
// adapter's per-row skip-on-parse-error policy).
//
// The function is pure — it has no side effects and is safe for concurrent use.
func CountIssuedSince(records []CrtShRecord, threshold time.Time) (count int, oldestInWindow time.Time) {
	for _, rec := range records {
		if rec.NotBefore.IsZero() {
			continue
		}
		if !isLEIssuer(rec) {
			continue
		}
		if !rec.NotBefore.After(threshold) {
			continue
		}
		count++
		if oldestInWindow.IsZero() || rec.NotBefore.Before(oldestInWindow) {
			oldestInWindow = rec.NotBefore
		}
	}
	return count, oldestInWindow
}
