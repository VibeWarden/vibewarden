package cmd

import (
	"bytes"
	"testing"
)

func TestSecretSetCmd_RequiresArgs(t *testing.T) {
	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"secret", "set", "mypath"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when no key=value args provided")
	}
}

func TestSecretSetCmd_Help(t *testing.T) {
	root := NewRootCmd("test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"secret", "set", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("help failed: %v", err)
	}

	output := out.String()
	if !contains(output, "set") {
		t.Error("help output missing 'set'")
	}
}

func TestParseKeyValueArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		wantLen int
	}{
		{"valid single", []string{"key=value"}, false, 1},
		{"valid multi", []string{"a=1", "b=2", "c=3"}, false, 3},
		{"empty value", []string{"key="}, false, 1},
		{"no equals", []string{"invalid"}, true, 0},
		{"equals only", []string{"="}, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseKeyValueArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseKeyValueArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("parseKeyValueArgs() returned %d entries, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestLoadConfigForCLI_MissingFile(t *testing.T) {
	cfg, err := loadConfigForCLI("/nonexistent/path/vibewarden.yaml")
	if err != nil {
		t.Fatalf("loadConfigForCLI should return defaults for missing file, got error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}
}

// contains is a simple substring check (avoids importing strings in test).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 && bytesContains([]byte(s), []byte(substr))
}

func bytesContains(s, sub []byte) bool {
	return bytes.Contains(s, sub)
}
