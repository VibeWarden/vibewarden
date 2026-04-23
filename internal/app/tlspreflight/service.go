package tlspreflight

import (
	"context"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// Service orchestrates the LE rate-limit preflight. It is stateless and safe
// for concurrent use. Each call to Check issues one crt.sh query per domain
// (sequential — crt.sh friendly, N is small in practice).
type Service struct {
	ct    ports.CertTransparencyQuerier
	clock func() time.Time
}

// NewService creates a new Service using the given CertTransparencyQuerier.
func NewService(ct ports.CertTransparencyQuerier) *Service {
	return &Service{
		ct:    ct,
		clock: time.Now,
	}
}

// WithClock returns a copy of the Service with an injected clock function.
// Used in tests to produce deterministic "next slot" timestamps.
func (s *Service) WithClock(fn func() time.Time) *Service {
	cp := *s
	cp.clock = fn
	return &cp
}

// Check runs the preflight for each registered domain in domains. Domains must
// already be normalised to eTLD+1; the service does not re-normalise (that is
// the CLI wiring's job).
//
// Returns one Result per input domain in the same order. A query failure
// produces a Result with Status=WARN, Err set, and Detail populated with the
// appropriate "crt.sh …" message per AC-8.
func (s *Service) Check(ctx context.Context, registeredDomains []string) []tlspreflight.Result {
	results := make([]tlspreflight.Result, 0, len(registeredDomains))
	now := s.clock()
	threshold := now.Add(-tlspreflight.BudgetWindow)

	for _, domain := range registeredDomains {
		records, err := s.ct.Query(ctx, domain)
		if err != nil {
			results = append(results, tlspreflight.NewFromError(domain, err))
			continue
		}

		// Map ports.CrtShRecord → domain.CrtShRecord for the pure parser.
		domainRecs := make([]tlspreflight.CrtShRecord, len(records))
		for i, r := range records {
			domainRecs[i] = tlspreflight.CrtShRecord{
				NotBefore:  r.NotBefore,
				IssuerName: r.IssuerName,
				CommonName: r.CommonName,
				NameValue:  r.NameValue,
			}
		}

		count, oldest := tlspreflight.CountIssuedSince(domainRecs, threshold)
		results = append(results, tlspreflight.NewFromQuery(domain, count, oldest))
	}

	return results
}
