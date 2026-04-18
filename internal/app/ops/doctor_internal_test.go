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
