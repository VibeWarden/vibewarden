package tlspreflight_test

import (
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/domain/tlspreflight"
)

func TestCountIssuedSince(t *testing.T) {
	now := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	threshold := now.Add(-tlspreflight.BudgetWindow) // 168h ago

	leIssuer := "C=US, O=Let's Encrypt, CN=R3"
	sectionIssuer := "C=US, O=Sectigo Limited, CN=Sectigo RSA Domain Validation Secure Server CA"
	zeroSSLIssuer := "C=AT, O=ZeroSSL, CN=ZeroSSL RSA Domain Validation Secure Server CA"

	// Helper to make a record at a given offset from now.
	rec := func(issuer string, offset time.Duration) tlspreflight.CrtShRecord {
		return tlspreflight.CrtShRecord{
			NotBefore:  now.Add(offset),
			IssuerName: issuer,
		}
	}

	tests := []struct {
		name            string
		records         []tlspreflight.CrtShRecord
		wantCount       int
		wantOldestZero  bool
		wantOldestHours float64 // hours before now of the expected oldest
	}{
		{
			name:           "empty input",
			records:        nil,
			wantCount:      0,
			wantOldestZero: true,
		},
		{
			name: "all outside window",
			records: []tlspreflight.CrtShRecord{
				rec(leIssuer, -200*time.Hour),
				rec(leIssuer, -300*time.Hour),
			},
			wantCount:      0,
			wantOldestZero: true,
		},
		{
			name: "all LE inside window",
			records: []tlspreflight.CrtShRecord{
				rec(leIssuer, -1*time.Hour),
				rec(leIssuer, -24*time.Hour),
				rec(leIssuer, -72*time.Hour),
			},
			wantCount:       3,
			wantOldestZero:  false,
			wantOldestHours: 72,
		},
		{
			name: "mix of LE and Sectigo — Sectigo excluded",
			records: []tlspreflight.CrtShRecord{
				rec(leIssuer, -10*time.Hour),
				rec(sectionIssuer, -20*time.Hour),
				rec(leIssuer, -30*time.Hour),
				rec(zeroSSLIssuer, -5*time.Hour),
			},
			wantCount:       2,
			wantOldestZero:  false,
			wantOldestHours: 30,
		},
		{
			name: "record with zero NotBefore is skipped",
			records: []tlspreflight.CrtShRecord{
				{NotBefore: time.Time{}, IssuerName: leIssuer},
				rec(leIssuer, -5*time.Hour),
			},
			wantCount:       1,
			wantOldestZero:  false,
			wantOldestHours: 5,
		},
		{
			name: "exact boundary — threshold itself is excluded (strictly after)",
			records: []tlspreflight.CrtShRecord{
				{NotBefore: threshold, IssuerName: leIssuer},                  // exactly at threshold — excluded
				{NotBefore: threshold.Add(time.Second), IssuerName: leIssuer}, // one second after — included
			},
			wantCount:       1,
			wantOldestZero:  false,
			wantOldestHours: 168.0, // threshold+1s is approximately 168h before now; tolerance handles the 1s
		},
		{
			name: "oldest in window is the earliest within range",
			records: []tlspreflight.CrtShRecord{
				rec(leIssuer, -5*time.Hour),
				rec(leIssuer, -100*time.Hour), // inside window (168h window)
				rec(leIssuer, -50*time.Hour),
			},
			wantCount:       3,
			wantOldestZero:  false,
			wantOldestHours: 100,
		},
		{
			name: "count above budget capacity — not clamped at parse level",
			records: []tlspreflight.CrtShRecord{
				rec(leIssuer, -1*time.Hour),
				rec(leIssuer, -2*time.Hour),
				rec(leIssuer, -3*time.Hour),
				rec(leIssuer, -4*time.Hour),
				rec(leIssuer, -5*time.Hour),
				rec(leIssuer, -6*time.Hour),
			},
			wantCount:       6,
			wantOldestZero:  false,
			wantOldestHours: 6,
		},
		{
			name: "issuer name matching is case-insensitive",
			records: []tlspreflight.CrtShRecord{
				{NotBefore: now.Add(-10 * time.Hour), IssuerName: "C=US, O=LET'S ENCRYPT, CN=R3"},
			},
			wantCount:       1,
			wantOldestZero:  false,
			wantOldestHours: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, oldest := tlspreflight.CountIssuedSince(tt.records, threshold)
			if count != tt.wantCount {
				t.Errorf("count = %d, want %d", count, tt.wantCount)
			}
			if tt.wantOldestZero {
				if !oldest.IsZero() {
					t.Errorf("oldest = %v, want zero", oldest)
				}
				return
			}
			if oldest.IsZero() {
				t.Fatal("oldest is zero, want non-zero")
			}
			gotHours := now.Sub(oldest).Hours()
			// Allow 1-minute tolerance for the boundary test case.
			if diff := gotHours - tt.wantOldestHours; diff < -0.1 || diff > 0.1 {
				t.Errorf("oldest is %v hours before now, want ~%.1f", gotHours, tt.wantOldestHours)
			}
		})
	}
}
