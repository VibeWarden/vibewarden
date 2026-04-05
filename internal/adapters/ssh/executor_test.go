package ssh_test

import (
	"testing"

	sshadapter "github.com/vibewarden/vibewarden/internal/adapters/ssh"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    sshadapter.Target
		wantErr bool
	}{
		{
			name:  "user and host",
			input: "ssh://ubuntu@203.0.113.10",
			want:  sshadapter.Target{User: "ubuntu", Host: "203.0.113.10", Port: 0},
		},
		{
			name:  "user host and port",
			input: "ssh://deploy@myserver.example.com:2222",
			want:  sshadapter.Target{User: "deploy", Host: "myserver.example.com", Port: 2222},
		},
		{
			name:  "port 22 explicit",
			input: "ssh://root@10.0.0.1:22",
			want:  sshadapter.Target{User: "root", Host: "10.0.0.1", Port: 22},
		},
		{
			name:    "wrong scheme",
			input:   "http://user@host",
			wantErr: true,
		},
		{
			name:    "missing user",
			input:   "ssh://myhost",
			wantErr: true,
		},
		{
			name:    "missing host",
			input:   "ssh://user@",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			input:   ":::not-a-url",
			wantErr: true,
		},
		{
			name:    "port out of range high",
			input:   "ssh://user@host:99999",
			wantErr: true,
		},
		{
			name:    "port out of range low",
			input:   "ssh://user@host:0",
			wantErr: true,
		},
		{
			name:    "non-numeric port",
			input:   "ssh://user@host:abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sshadapter.ParseTarget(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTarget(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Host != tt.want.Host {
				t.Errorf("Host = %q, want %q", got.Host, tt.want.Host)
			}
			if got.Port != tt.want.Port {
				t.Errorf("Port = %d, want %d", got.Port, tt.want.Port)
			}
		})
	}
}

func TestTarget_Destination(t *testing.T) {
	tests := []struct {
		name   string
		target sshadapter.Target
		want   string
	}{
		{
			name:   "user and host",
			target: sshadapter.Target{User: "ubuntu", Host: "203.0.113.10"},
			want:   "ubuntu@203.0.113.10",
		},
		{
			name:   "deploy user",
			target: sshadapter.Target{User: "deploy", Host: "myserver.example.com", Port: 2222},
			want:   "deploy@myserver.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.target.Destination()
			if got != tt.want {
				t.Errorf("Destination() = %q, want %q", got, tt.want)
			}
		})
	}
}
