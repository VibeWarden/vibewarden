package site

import "testing"

func TestDefaultGlobalConfig(t *testing.T) {
	t.Parallel()

	g := DefaultGlobalConfig()

	if g.ListenHost != "0.0.0.0" {
		t.Errorf("ListenHost = %q, want %q", g.ListenHost, "0.0.0.0")
	}
	if g.ListenPort != 443 {
		t.Errorf("ListenPort = %d, want %d", g.ListenPort, 443)
	}
	if g.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", g.LogLevel, "info")
	}
	if g.AdminToken != "" {
		t.Errorf("AdminToken should be empty by default, got %q", g.AdminToken)
	}
	if g.ACMEEmail != "" {
		t.Errorf("ACMEEmail should be empty by default, got %q", g.ACMEEmail)
	}
}

func TestGlobalConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     GlobalConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid defaults",
			cfg:     DefaultGlobalConfig(),
			wantErr: false,
		},
		{
			name: "valid with all fields",
			cfg: GlobalConfig{
				AdminToken: "secret-token",
				ListenHost: "127.0.0.1",
				ListenPort: 8443,
				LogLevel:   "debug",
				ACMEEmail:  "admin@example.com",
			},
			wantErr: false,
		},
		{
			name: "valid with empty listen host",
			cfg: GlobalConfig{
				ListenHost: "",
				ListenPort: 443,
				LogLevel:   "info",
			},
			wantErr: false,
		},
		{
			name: "invalid listen host",
			cfg: GlobalConfig{
				ListenHost: "not-an-ip",
				ListenPort: 443,
				LogLevel:   "info",
			},
			wantErr: true,
			errMsg:  "listen_host",
		},
		{
			name: "negative listen port",
			cfg: GlobalConfig{
				ListenHost: "0.0.0.0",
				ListenPort: -1,
				LogLevel:   "info",
			},
			wantErr: true,
			errMsg:  "listen_port",
		},
		{
			name: "listen port too high",
			cfg: GlobalConfig{
				ListenHost: "0.0.0.0",
				ListenPort: 70000,
				LogLevel:   "info",
			},
			wantErr: true,
			errMsg:  "listen_port",
		},
		{
			name: "invalid log level",
			cfg: GlobalConfig{
				ListenHost: "0.0.0.0",
				ListenPort: 443,
				LogLevel:   "trace",
			},
			wantErr: true,
			errMsg:  "log_level",
		},
		{
			name: "invalid acme email no at",
			cfg: GlobalConfig{
				ListenHost: "0.0.0.0",
				ListenPort: 443,
				LogLevel:   "info",
				ACMEEmail:  "not-an-email",
			},
			wantErr: true,
			errMsg:  "acme_email",
		},
		{
			name: "valid acme email",
			cfg: GlobalConfig{
				ListenHost: "0.0.0.0",
				ListenPort: 443,
				LogLevel:   "info",
				ACMEEmail:  "user@example.com",
			},
			wantErr: false,
		},
		{
			name: "port zero is valid",
			cfg: GlobalConfig{
				ListenPort: 0,
				LogLevel:   "info",
			},
			wantErr: false,
		},
		{
			name: "port 65535 is valid",
			cfg: GlobalConfig{
				ListenPort: 65535,
				LogLevel:   "info",
			},
			wantErr: false,
		},
		{
			name: "empty log level is valid",
			cfg: GlobalConfig{
				ListenPort: 443,
				LogLevel:   "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("GlobalConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if got := err.Error(); !contains(got, tt.errMsg) {
					t.Errorf("error %q should contain %q", got, tt.errMsg)
				}
			}
		})
	}
}

// contains reports whether s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
