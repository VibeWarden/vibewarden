package deploy_test

import (
	"context"
	"errors"
	"testing"

	deployapp "github.com/vibewarden/vibewarden/internal/app/deploy"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name     string
		executor *fakeExecutor
		wantMode deployapp.Mode
		wantErr  bool
	}{
		{
			name: "marker file exists returns AddSite",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -f ~/vibewarden/.sidecar/global.yaml": {output: "", err: nil},
				},
			},
			wantMode: deployapp.ModeAddSite,
		},
		{
			name: "marker file missing returns FreshInstall",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -f ~/vibewarden/.sidecar/global.yaml": {err: errors.New("exit status 1")},
				},
			},
			wantMode: deployapp.ModeFreshInstall,
		},
		{
			name: "SSH connection error is propagated",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -f ~/vibewarden/.sidecar/global.yaml": {err: errors.New("ssh: connect to host 1.2.3.4 port 22: Connection refused")},
				},
			},
			wantErr: true,
		},
		{
			name: "permission denied error is propagated",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -f ~/vibewarden/.sidecar/global.yaml": {err: errors.New("Permission denied (publickey)")},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := deployapp.Detect(context.Background(), tt.executor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Detect() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if mode != tt.wantMode {
				t.Errorf("Detect() = %v, want %v", mode, tt.wantMode)
			}
		})
	}
}

func TestIsMultiApp(t *testing.T) {
	tests := []struct {
		name     string
		executor *fakeExecutor
		want     bool
		wantErr  bool
	}{
		{
			name: "sites dir exists returns true",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -d ~/vibewarden/sites/": {output: "", err: nil},
				},
			},
			want: true,
		},
		{
			name: "sites dir missing returns false",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -d ~/vibewarden/sites/": {err: errors.New("exit status 1")},
				},
			},
			want: false,
		},
		{
			name: "SSH error is propagated",
			executor: &fakeExecutor{
				runResponses: map[string]runResponse{
					"test -d ~/vibewarden/sites/": {err: errors.New("ssh: connection refused")},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deployapp.IsMultiApp(context.Background(), tt.executor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsMultiApp() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("IsMultiApp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMode_String(t *testing.T) {
	tests := []struct {
		mode deployapp.Mode
		want string
	}{
		{deployapp.ModeFreshInstall, "fresh-install"},
		{deployapp.ModeAddSite, "add-site"},
		{deployapp.Mode(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.mode.String()
			if got != tt.want {
				t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}
