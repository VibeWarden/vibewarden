package archutil_test

import (
	"testing"

	"github.com/vibewarden/vibewarden/internal/archutil"
)

func TestNormalize(t *testing.T) {
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
			name:  "unknown arch passes through lowercase",
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
			got := archutil.Normalize(tt.input)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
