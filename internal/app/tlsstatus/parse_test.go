package tlsstatus

import (
	"testing"
	"time"
)

// cannedOpenSSLOutput is a realistic output from:
//
//	echo | openssl s_client -connect example.com:443 -servername example.com 2>/dev/null | \
//	    openssl x509 -noout -subject -issuer -dates -serial -ext subjectAltName
const cannedOpenSSLOutput = `subject=CN = example.com
issuer=C = US, O = Let's Encrypt, CN = R3
notBefore=Mar 15 00:00:00 2026 GMT
notAfter=Jun 13 00:00:00 2026 GMT
serial=03A1B2C3D4E5F6
X509v3 Subject Alternative Name:
    DNS:example.com, DNS:www.example.com`

func TestParseOpenSSLOutput(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantSubject string
		wantIssuer  string
		wantBefore  time.Time
		wantAfter   time.Time
		wantSANs    []string
		wantSerial  string
		wantErr     bool
	}{
		{
			name:        "standard output",
			input:       cannedOpenSSLOutput,
			wantSubject: "CN = example.com",
			wantIssuer:  "C = US, O = Let's Encrypt, CN = R3",
			wantBefore:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			wantAfter:   time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
			wantSANs:    []string{"example.com", "www.example.com"},
			wantSerial:  "03A1B2C3D4E5F6",
			wantErr:     false,
		},
		{
			name: "wildcard SAN",
			input: `subject=CN = *.example.com
issuer=C = US, O = Let's Encrypt, CN = R3
notBefore=Mar 15 00:00:00 2026 GMT
notAfter=Jun 13 00:00:00 2026 GMT
serial=AABBCCDD
X509v3 Subject Alternative Name:
    DNS:*.example.com, DNS:example.com`,
			wantSubject: "CN = *.example.com",
			wantIssuer:  "C = US, O = Let's Encrypt, CN = R3",
			wantBefore:  time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
			wantAfter:   time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC),
			wantSANs:    []string{"*.example.com", "example.com"},
			wantSerial:  "AABBCCDD",
			wantErr:     false,
		},
		{
			name: "no SANs section",
			input: `subject=CN = simple.test
issuer=CN = Test CA
notBefore=Jan  5 10:30:00 2026 GMT
notAfter=Apr 20 10:30:00 2026 GMT
serial=01`,
			wantSubject: "CN = simple.test",
			wantIssuer:  "CN = Test CA",
			wantBefore:  time.Date(2026, 1, 5, 10, 30, 0, 0, time.UTC),
			wantAfter:   time.Date(2026, 4, 20, 10, 30, 0, 0, time.UTC),
			wantSANs:    nil,
			wantSerial:  "01",
			wantErr:     false,
		},
		{
			name:    "empty output",
			input:   "",
			wantErr: true,
		},
		{
			name: "missing subject",
			input: `issuer=CN = Test CA
notBefore=Jan  5 10:30:00 2026 GMT
notAfter=Apr 20 10:30:00 2026 GMT
serial=01`,
			wantErr: true,
		},
		{
			name: "missing issuer",
			input: `subject=CN = test.com
notBefore=Jan  5 10:30:00 2026 GMT
notAfter=Apr 20 10:30:00 2026 GMT
serial=01`,
			wantErr: true,
		},
		{
			name: "missing notBefore",
			input: `subject=CN = test.com
issuer=CN = Test CA
notAfter=Apr 20 10:30:00 2026 GMT
serial=01`,
			wantErr: true,
		},
		{
			name: "missing notAfter",
			input: `subject=CN = test.com
issuer=CN = Test CA
notBefore=Jan  5 10:30:00 2026 GMT
serial=01`,
			wantErr: true,
		},
		{
			name: "invalid date format",
			input: `subject=CN = test.com
issuer=CN = Test CA
notBefore=2026-01-05
notAfter=2026-04-20
serial=01`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ci, err := ParseOpenSSLOutput(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseOpenSSLOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if ci.Subject() != tt.wantSubject {
				t.Errorf("Subject() = %q, want %q", ci.Subject(), tt.wantSubject)
			}
			if ci.Issuer() != tt.wantIssuer {
				t.Errorf("Issuer() = %q, want %q", ci.Issuer(), tt.wantIssuer)
			}
			if !ci.NotBefore().Equal(tt.wantBefore) {
				t.Errorf("NotBefore() = %v, want %v", ci.NotBefore(), tt.wantBefore)
			}
			if !ci.NotAfter().Equal(tt.wantAfter) {
				t.Errorf("NotAfter() = %v, want %v", ci.NotAfter(), tt.wantAfter)
			}
			if ci.Serial() != tt.wantSerial {
				t.Errorf("Serial() = %q, want %q", ci.Serial(), tt.wantSerial)
			}

			gotSANs := ci.SANs()
			if len(gotSANs) != len(tt.wantSANs) {
				t.Errorf("SANs() len = %d, want %d", len(gotSANs), len(tt.wantSANs))
			} else {
				for i, want := range tt.wantSANs {
					if gotSANs[i] != want {
						t.Errorf("SANs()[%d] = %q, want %q", i, gotSANs[i], want)
					}
				}
			}
		})
	}
}

func TestParseSANLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "multiple DNS entries",
			line: "    DNS:example.com, DNS:www.example.com, DNS:*.example.com",
			want: []string{"example.com", "www.example.com", "*.example.com"},
		},
		{
			name: "single entry",
			line: "DNS:only.com",
			want: []string{"only.com"},
		},
		{
			name: "empty line",
			line: "",
			want: nil,
		},
		{
			name: "IP entries ignored",
			line: "DNS:example.com, IP Address:1.2.3.4",
			want: []string{"example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSANLine(tt.line)
			if len(got) != len(tt.want) {
				t.Errorf("parseSANLine(%q) len = %d, want %d; got %v", tt.line, len(got), len(tt.want), got)
				return
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("parseSANLine(%q)[%d] = %q, want %q", tt.line, i, got[i], want)
				}
			}
		})
	}
}

func TestNormalizeSpaces(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Jan  5 10:30:00 2026 GMT", "Jan 5 10:30:00 2026 GMT"},
		{"Apr 20 12:00:00 2026 GMT", "Apr 20 12:00:00 2026 GMT"},
		{"no  extra   spaces", "no extra spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeSpaces(tt.input); got != tt.want {
				t.Errorf("normalizeSpaces(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
