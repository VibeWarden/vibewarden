package sites

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGlobal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		yaml      string
		exists    bool
		wantErr   bool
		wantHost  string
		wantPort  int
		wantLevel string
		wantToken string
		wantEmail string
	}{
		{
			name:      "file does not exist returns defaults",
			exists:    false,
			wantErr:   false,
			wantHost:  "0.0.0.0",
			wantPort:  443,
			wantLevel: "info",
		},
		{
			name:      "empty file returns defaults",
			yaml:      "",
			exists:    true,
			wantErr:   false,
			wantHost:  "0.0.0.0",
			wantPort:  443,
			wantLevel: "info",
		},
		{
			name: "full config",
			yaml: `admin_token: secret123
listen_host: 127.0.0.1
listen_port: 8443
log_level: debug
acme_email: admin@example.com
`,
			exists:    true,
			wantErr:   false,
			wantHost:  "127.0.0.1",
			wantPort:  8443,
			wantLevel: "debug",
			wantToken: "secret123",
			wantEmail: "admin@example.com",
		},
		{
			name: "partial config inherits defaults",
			yaml: `admin_token: mytoken
`,
			exists:    true,
			wantErr:   false,
			wantHost:  "0.0.0.0",
			wantPort:  443,
			wantLevel: "info",
			wantToken: "mytoken",
		},
		{
			name: "invalid log level",
			yaml: `log_level: trace
`,
			exists:  true,
			wantErr: true,
		},
		{
			name: "invalid listen host",
			yaml: `listen_host: not-an-ip
`,
			exists:  true,
			wantErr: true,
		},
		{
			name:    "invalid YAML",
			yaml:    `{{{`,
			exists:  true,
			wantErr: true,
		},
		{
			name: "invalid acme email",
			yaml: `acme_email: not-an-email
`,
			exists:  true,
			wantErr: true,
		},
		{
			name: "port zero keeps default",
			yaml: `listen_port: 0
`,
			exists:    true,
			wantErr:   false,
			wantHost:  "0.0.0.0",
			wantPort:  443,
			wantLevel: "info",
		},
		{
			name: "IPv6 listen host",
			yaml: `listen_host: "::1"
`,
			exists:    true,
			wantErr:   false,
			wantHost:  "::1",
			wantPort:  443,
			wantLevel: "info",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "global.yaml")

			if tt.exists {
				if err := os.WriteFile(path, []byte(tt.yaml), 0o644); err != nil {
					t.Fatalf("WriteFile: %v", err)
				}
			}

			g, err := LoadGlobal(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadGlobal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if g.ListenHost != tt.wantHost {
				t.Errorf("ListenHost = %q, want %q", g.ListenHost, tt.wantHost)
			}
			if g.ListenPort != tt.wantPort {
				t.Errorf("ListenPort = %d, want %d", g.ListenPort, tt.wantPort)
			}
			if g.LogLevel != tt.wantLevel {
				t.Errorf("LogLevel = %q, want %q", g.LogLevel, tt.wantLevel)
			}
			if g.AdminToken != tt.wantToken {
				t.Errorf("AdminToken = %q, want %q", g.AdminToken, tt.wantToken)
			}
			if g.ACMEEmail != tt.wantEmail {
				t.Errorf("ACMEEmail = %q, want %q", g.ACMEEmail, tt.wantEmail)
			}
		})
	}
}
