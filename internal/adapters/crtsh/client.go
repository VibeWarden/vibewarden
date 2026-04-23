package crtsh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
	"github.com/vibewarden/vibewarden/internal/ports"
)

const (
	// defaultBase is the crt.sh base URL used by NewClient.
	defaultBase = "https://crt.sh"

	// userAgent is sent on every request so crt.sh operators can identify the
	// tool. The URL is informational — it helps them contact us if we cause load.
	userAgent = "vibew-doctor/1 (+https://vibewarden.dev)"
)

// Client implements ports.CertTransparencyQuerier by calling the crt.sh
// public JSON API over HTTPS.
//
// Construct via NewClient or NewClientWithBase (for tests). The client is
// stateless and safe for concurrent use.
type Client struct {
	http *http.Client
	base string
}

// NewClient returns a Client that calls the production crt.sh endpoint.
// httpClient must have a Timeout set (the caller in cmd/doctor.go uses 10s
// per AC-8).
func NewClient(httpClient *http.Client) *Client {
	return &Client{http: httpClient, base: defaultBase}
}

// NewClientWithBase returns a Client with a custom base URL, used in tests to
// point at an httptest.Server instead of the real crt.sh.
func NewClientWithBase(httpClient *http.Client, base string) *Client {
	return &Client{http: httpClient, base: base}
}

// crtShRow is the JSON shape returned by crt.sh for each certificate record.
// Only the fields the preflight consumes are decoded; others are ignored.
type crtShRow struct {
	NotBefore  string `json:"not_before"`
	IssuerName string `json:"issuer_name"`
	CommonName string `json:"common_name"`
	NameValue  string `json:"name_value"`
}

// Query fetches certificates from crt.sh for the given registered domain and
// returns them as a slice of ports.CrtShRecord.
//
// Error mapping:
//   - network / DNS / context cancel  → tlspreflight.ErrCTUnavailable (wrapped)
//   - HTTP 429                        → tlspreflight.ErrCTThrottled (wrapped)
//   - HTTP non-200 (except 429)       → tlspreflight.ErrCTUnavailable (wrapped)
//   - Content-Type not JSON           → tlspreflight.ErrCTResponseMalformed
//   - JSON decode error               → tlspreflight.ErrCTResponseMalformed (wrapped)
//   - empty body / empty array        → nil error, empty slice
func (c *Client) Query(ctx context.Context, registeredDomain string) ([]ports.CrtShRecord, error) {
	reqURL := buildURL(c.base, registeredDomain)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", tlspreflight.ErrCTUnavailable, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", tlspreflight.ErrCTUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: HTTP 429", tlspreflight.ErrCTThrottled)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", tlspreflight.ErrCTUnavailable, resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		return nil, fmt.Errorf("%w: Content-Type %q", tlspreflight.ErrCTResponseMalformed, ct)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read body: %v", tlspreflight.ErrCTUnavailable, err)
	}

	// crt.sh returns an empty array "[]" for unknown domains — treat as 0 certs.
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil, nil
	}

	var rows []crtShRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("%w: %v", tlspreflight.ErrCTResponseMalformed, err)
	}

	return mapRows(rows), nil
}

// buildURL constructs the crt.sh query URL for a registered domain.
func buildURL(base, registeredDomain string) string {
	params := url.Values{}
	params.Set("Identity", registeredDomain)
	params.Set("exclude", "expired")
	params.Set("output", "json")
	return base + "/?" + params.Encode()
}

// mapRows converts raw crt.sh JSON rows to ports.CrtShRecord, skipping rows
// where not_before cannot be parsed. Per-row parse errors are silent — a
// single malformed row in a 40-record response must not fail the whole query.
func mapRows(rows []crtShRow) []ports.CrtShRecord {
	out := make([]ports.CrtShRecord, 0, len(rows))
	for _, row := range rows {
		nb, err := parseNotBefore(row.NotBefore)
		if err != nil {
			// Skip silently — per ADR-090 §(b) adapter spec.
			continue
		}
		out = append(out, ports.CrtShRecord{
			NotBefore:  nb,
			IssuerName: row.IssuerName,
			CommonName: row.CommonName,
			NameValue:  row.NameValue,
		})
	}
	return out
}

// parseNotBefore attempts RFC 3339 and then the crt.sh legacy format
// "2006-01-02T15:04:05" (no timezone — treated as UTC).
func parseNotBefore(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty not_before")
	}
	// Try standard RFC 3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// crt.sh sometimes returns timestamps without a timezone suffix.
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse not_before %q", s)
}
