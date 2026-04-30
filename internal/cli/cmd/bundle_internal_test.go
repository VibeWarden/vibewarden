package cmd

import (
	"strings"
	"testing"
)

// TestValidatePrintDeployFlags is a pure unit test for the validatePrintDeployFlags
// helper. The nine cases cover the full acceptance/rejection matrix: all flags
// unset (ok), --print-deploy alone (error), each partial set of sub-flags with
// --print-deploy (error), all four set (ok), and each sub-flag without
// --print-deploy (error).
func TestValidatePrintDeployFlags(t *testing.T) {
	tests := []struct {
		name        string
		printDeploy bool
		host        string
		user        string
		path        string
		wantErr     bool
		errContains []string // all strings must be present in the error message
	}{
		{
			name:        "all empty — default behavior, no flags set",
			printDeploy: false,
			host:        "",
			user:        "",
			path:        "",
			wantErr:     false,
		},
		{
			name:        "--print-deploy only — missing all three sub-flags",
			printDeploy: true,
			host:        "",
			user:        "",
			path:        "",
			wantErr:     true,
			errContains: []string{"--print-deploy requires", "--host", "--user", "--path"},
		},
		{
			name:        "--print-deploy + --host only — missing user and path",
			printDeploy: true,
			host:        "h.example",
			user:        "",
			path:        "",
			wantErr:     true,
			errContains: []string{"--print-deploy requires", "--user", "--path"},
		},
		{
			name:        "--print-deploy + --host + --user — missing path",
			printDeploy: true,
			host:        "h.example",
			user:        "alice",
			path:        "",
			wantErr:     true,
			errContains: []string{"--print-deploy requires", "--path"},
		},
		{
			name:        "all four set — valid",
			printDeploy: true,
			host:        "h.example",
			user:        "alice",
			path:        "/opt/myapp",
			wantErr:     false,
		},
		{
			name:        "--host without --print-deploy",
			printDeploy: false,
			host:        "h.example",
			user:        "",
			path:        "",
			wantErr:     true,
			errContains: []string{"require --print-deploy"},
		},
		{
			name:        "--user without --print-deploy",
			printDeploy: false,
			host:        "",
			user:        "alice",
			path:        "",
			wantErr:     true,
			errContains: []string{"require --print-deploy"},
		},
		{
			name:        "--path without --print-deploy",
			printDeploy: false,
			host:        "",
			user:        "",
			path:        "/opt/myapp",
			wantErr:     true,
			errContains: []string{"require --print-deploy"},
		},
		{
			name:        "--host + --user + --path without --print-deploy",
			printDeploy: false,
			host:        "h.example",
			user:        "alice",
			path:        "/opt/myapp",
			wantErr:     true,
			errContains: []string{"require --print-deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePrintDeployFlags(tt.printDeploy, tt.host, tt.user, tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePrintDeployFlags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				for _, want := range tt.errContains {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error missing %q; got: %v", want, err)
					}
				}
			}
		})
	}
}
