package ports

import (
	"context"
	"time"
)

// CertTransparencyQuerier returns the raw certificate records published to
// Certificate Transparency logs for a single registered domain.
//
// Implementations call the crt.sh public JSON API. Query MUST receive the
// registered domain (eTLD+1), not a full FQDN — callers are responsible for
// normalising via golang.org/x/net/publicsuffix.
//
// Error conventions (sentinel values live in domain/tlspreflight):
//   - tlspreflight.ErrCTUnavailable  — network failure, DNS failure, non-200/non-429 status.
//   - tlspreflight.ErrCTThrottled    — HTTP 429 from crt.sh.
//   - tlspreflight.ErrCTResponseMalformed — invalid JSON or unexpected Content-Type.
type CertTransparencyQuerier interface {
	Query(ctx context.Context, registeredDomain string) ([]CrtShRecord, error)
}

// CrtShRecord is the slim projection of a crt.sh JSON row that the preflight
// needs. It mirrors domain/tlspreflight.CrtShRecord — declared here in the
// ports layer so adapters and application services can type-check against a
// stable interface without importing the domain package.
type CrtShRecord struct {
	// NotBefore is the certificate's not-before timestamp parsed from RFC 3339.
	NotBefore time.Time
	// IssuerName is the full issuer DN string from crt.sh,
	// e.g. "C=US, O=Let's Encrypt, CN=R3".
	IssuerName string
	// CommonName is the certificate's CN field.
	CommonName string
	// NameValue is the SAN / name-value field from crt.sh.
	NameValue string
}
