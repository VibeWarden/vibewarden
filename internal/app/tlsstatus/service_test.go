package tlsstatus_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/vibewarden/vibewarden/internal/app/tlsstatus"
)

// fakeExecutor is a simple test double for ports.RemoteExecutor.
type fakeExecutor struct {
	output string
	err    error
}

func (f *fakeExecutor) Run(_ context.Context, _ string) (string, error) {
	return f.output, f.err
}

func (f *fakeExecutor) RunStream(_ context.Context, _ string, _, _ io.Writer) error {
	return nil
}

func (f *fakeExecutor) Transfer(_ context.Context, _, _ string, _ bool) error {
	return nil
}

func (f *fakeExecutor) TransferFile(_ context.Context, _, _ string) error {
	return nil
}

func (f *fakeExecutor) TransferExcluding(_ context.Context, _, _ string, _ bool, _ []string) error {
	return nil
}

func (f *fakeExecutor) DryRunTransfer(_ context.Context, _, _ string) ([]string, error) {
	return nil, nil
}

const testOpenSSLOutput = `subject=CN = myapp.example.com
issuer=C = US, O = Let's Encrypt, CN = R3
notBefore=Mar 15 00:00:00 2026 GMT
notAfter=Jun 13 00:00:00 2026 GMT
serial=03A1B2C3D4E5F6
X509v3 Subject Alternative Name:
    DNS:myapp.example.com, DNS:www.myapp.example.com`

func TestService_Inspect(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		port        int
		output      string
		execErr     error
		wantErr     bool
		wantSubject string
	}{
		{
			name:        "successful inspection",
			domain:      "myapp.example.com",
			port:        443,
			output:      testOpenSSLOutput,
			wantErr:     false,
			wantSubject: "CN = myapp.example.com",
		},
		{
			name:    "empty domain",
			domain:  "",
			port:    443,
			output:  testOpenSSLOutput,
			wantErr: true,
		},
		{
			name:    "invalid port zero",
			domain:  "example.com",
			port:    0,
			output:  testOpenSSLOutput,
			wantErr: true,
		},
		{
			name:    "invalid port too high",
			domain:  "example.com",
			port:    70000,
			output:  testOpenSSLOutput,
			wantErr: true,
		},
		{
			name:    "executor error",
			domain:  "example.com",
			port:    443,
			output:  "",
			execErr: fmt.Errorf("connection refused"),
			wantErr: true,
		},
		{
			name:    "unparseable output",
			domain:  "example.com",
			port:    443,
			output:  "garbage data that openssl did not produce",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &fakeExecutor{output: tt.output, err: tt.execErr}
			svc := tlsstatus.NewService(exec)

			ci, err := svc.Inspect(context.Background(), tt.domain, tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("Inspect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if ci.Subject() != tt.wantSubject {
				t.Errorf("Subject() = %q, want %q", ci.Subject(), tt.wantSubject)
			}

			// Verify dates are parsed correctly.
			wantBefore := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
			if !ci.NotBefore().Equal(wantBefore) {
				t.Errorf("NotBefore() = %v, want %v", ci.NotBefore(), wantBefore)
			}

			wantAfter := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
			if !ci.NotAfter().Equal(wantAfter) {
				t.Errorf("NotAfter() = %v, want %v", ci.NotAfter(), wantAfter)
			}

			// Verify SANs.
			sans := ci.SANs()
			if len(sans) != 2 {
				t.Fatalf("SANs() len = %d, want 2", len(sans))
			}
			if sans[0] != "myapp.example.com" {
				t.Errorf("SANs()[0] = %q, want %q", sans[0], "myapp.example.com")
			}
			if sans[1] != "www.myapp.example.com" {
				t.Errorf("SANs()[1] = %q, want %q", sans[1], "www.myapp.example.com")
			}
		})
	}
}

func TestService_Inspect_CustomPort(t *testing.T) {
	exec := &fakeExecutor{output: testOpenSSLOutput}
	svc := tlsstatus.NewService(exec)

	ci, err := svc.Inspect(context.Background(), "myapp.example.com", 8443)
	if err != nil {
		t.Fatalf("Inspect() unexpected error: %v", err)
	}

	if ci.Subject() != "CN = myapp.example.com" {
		t.Errorf("Subject() = %q, want %q", ci.Subject(), "CN = myapp.example.com")
	}
}
