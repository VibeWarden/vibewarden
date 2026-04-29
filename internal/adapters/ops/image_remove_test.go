package ops_test

import (
	"testing"

	opsadapter "github.com/vibewarden/vibewarden/internal/adapters/ops"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// TestImageRemoveAdapter_InterfaceCompliance verifies the adapter satisfies
// ports.DockerImageRemover at compile time. No docker daemon required.
func TestImageRemoveAdapter_InterfaceCompliance(t *testing.T) {
	var _ ports.DockerImageRemover = opsadapter.NewImageRemoveAdapter()
}

// TestBuildImageRmArgs verifies the exact argument slice passed to docker.
func TestBuildImageRmArgs(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want []string
	}{
		{
			name: "simple tag",
			tag:  "myapp:latest",
			want: []string{"image", "rm", "myapp:latest"},
		},
		{
			name: "compound project tag",
			tag:  "qr-code-blackhole-app:latest",
			want: []string{"image", "rm", "qr-code-blackhole-app:latest"},
		},
		{
			name: "registry prefixed tag",
			tag:  "ghcr.io/org/myapp:v1.2.3",
			want: []string{"image", "rm", "ghcr.io/org/myapp:v1.2.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := opsadapter.BuildImageRmArgsForTest(tt.tag)
			if len(got) != len(tt.want) {
				t.Fatalf("buildImageRmArgs() len = %d, want %d\ngot:  %v\nwant: %v",
					len(got), len(tt.want), got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("arg[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
