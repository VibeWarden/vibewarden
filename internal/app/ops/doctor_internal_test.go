package ops

import (
	"testing"
)

func TestExtractHostFromTarget(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "ssh URL with user and host",
			input: "ssh://user@192.0.2.1",
			want:  "192.0.2.1",
		},
		{
			name:  "ssh URL with user, host, and port",
			input: "ssh://user@192.0.2.1:2222",
			want:  "192.0.2.1",
		},
		{
			name:  "ssh URL with hostname",
			input: "ssh://deploy@example.com",
			want:  "example.com",
		},
		{
			name:  "ssh URL with hostname and port",
			input: "ssh://deploy@example.com:22",
			want:  "example.com",
		},
		{
			name:  "ssh URL without user",
			input: "ssh://192.0.2.1",
			want:  "192.0.2.1",
		},
		{
			name:  "ssh URL with IPv6 address",
			input: "ssh://user@[::1]:22",
			want:  "::1",
		},
		{
			name:  "ssh URL with IPv6 no port",
			input: "ssh://user@[2001:db8::1]",
			want:  "2001:db8::1",
		},
		{
			name:  "bare hostname falls through",
			input: "192.0.2.1",
			want:  "192.0.2.1",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostFromTarget(tt.input)
			if got != tt.want {
				t.Errorf("extractHostFromTarget(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "x86_64 maps to amd64",
			input: "x86_64",
			want:  "amd64",
		},
		{
			name:  "aarch64 maps to arm64",
			input: "aarch64",
			want:  "arm64",
		},
		{
			name:  "arm64 maps to arm64",
			input: "arm64",
			want:  "arm64",
		},
		{
			name:  "armv7l maps to arm",
			input: "armv7l",
			want:  "arm",
		},
		{
			name:  "X86_64 case insensitive",
			input: "X86_64",
			want:  "amd64",
		},
		{
			name:  "AARCH64 case insensitive",
			input: "AARCH64",
			want:  "arm64",
		},
		{
			name:  "whitespace trimmed",
			input: "  x86_64\n",
			want:  "amd64",
		},
		{
			name:  "unknown arch passes through",
			input: "riscv64",
			want:  "riscv64",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArch(tt.input)
			if got != tt.want {
				t.Errorf("normalizeArch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
