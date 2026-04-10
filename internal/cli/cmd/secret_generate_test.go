package cmd_test

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/vibewarden/vibewarden/internal/cli/cmd"
)

func TestGenerateHexSecret_Length(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLen    int // expected hex string length = 2 * bytes
		wantPrefix string
		wantErr    bool
	}{
		{
			name:    "default 32 bytes produces 64 hex chars",
			args:    []string{"secret", "generate"},
			wantLen: 64,
		},
		{
			name:    "explicit length 16 produces 32 hex chars",
			args:    []string{"secret", "generate", "--length", "16"},
			wantLen: 32,
		},
		{
			name:    "explicit length 64 produces 128 hex chars",
			args:    []string{"secret", "generate", "--length", "64"},
			wantLen: 128,
		},
		{
			name:       "admin-token flag",
			args:       []string{"secret", "generate", "--admin-token"},
			wantLen:    64,
			wantPrefix: "VIBEWARDEN_ADMIN_TOKEN=",
		},
		{
			name:       "fleet-key flag",
			args:       []string{"secret", "generate", "--fleet-key"},
			wantLen:    64,
			wantPrefix: "VIBEWARDEN_FLEET_KEY=",
		},
		{
			name:    "length below minimum returns error",
			args:    []string{"secret", "generate", "--length", "8"},
			wantErr: true,
		},
		{
			name:    "length above maximum returns error",
			args:    []string{"secret", "generate", "--length", "512"},
			wantErr: true,
		},
		{
			name:    "admin-token and fleet-key are mutually exclusive",
			args:    []string{"secret", "generate", "--admin-token", "--fleet-key"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := cmd.NewRootCmd("test")
			var outBuf, errBuf bytes.Buffer
			root.SetOut(&outBuf)
			root.SetErr(&errBuf)
			root.SetArgs(tt.args)

			err := root.Execute()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Execute() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("Execute() unexpected error: %v", err)
			}

			out := strings.TrimSpace(outBuf.String())

			if tt.wantPrefix != "" {
				if !strings.HasPrefix(out, tt.wantPrefix) {
					t.Errorf("output %q does not have prefix %q", out, tt.wantPrefix)
				}
				out = strings.TrimPrefix(out, tt.wantPrefix)
			}

			if len(out) != tt.wantLen {
				t.Errorf("hex token length = %d, want %d (output: %q)", len(out), tt.wantLen, out)
			}

			// Verify the output is valid hex.
			if _, decErr := hex.DecodeString(out); decErr != nil {
				t.Errorf("output %q is not valid hex: %v", out, decErr)
			}
		})
	}
}

func TestSecretGenerate_OutputIsUnique(t *testing.T) {
	// Two separate invocations must produce different tokens.
	root1 := cmd.NewRootCmd("test")
	var out1 bytes.Buffer
	root1.SetOut(&out1)
	root1.SetArgs([]string{"secret", "generate"})
	if err := root1.Execute(); err != nil {
		t.Fatalf("first Execute() error: %v", err)
	}

	root2 := cmd.NewRootCmd("test")
	var out2 bytes.Buffer
	root2.SetOut(&out2)
	root2.SetArgs([]string{"secret", "generate"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("second Execute() error: %v", err)
	}

	tok1 := strings.TrimSpace(out1.String())
	tok2 := strings.TrimSpace(out2.String())

	if tok1 == tok2 {
		t.Errorf("two consecutive secret generate calls produced the same token: %q", tok1)
	}
}

func TestSecretCmd_HelpWhenNoSubcommand(t *testing.T) {
	root := cmd.NewRootCmd("test")
	var outBuf bytes.Buffer
	root.SetOut(&outBuf)
	root.SetArgs([]string{"secret", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	out := outBuf.String()
	if !strings.Contains(out, "generate") {
		t.Errorf("expected help output to mention 'generate', got: %q", out)
	}
}
