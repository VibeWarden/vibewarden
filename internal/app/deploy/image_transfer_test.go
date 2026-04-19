package deploy

import "testing"

func TestIsLocalImage(t *testing.T) {
	tests := []struct {
		name      string
		imageName string
		want      bool
	}{
		{
			name:      "bare name with tag is local",
			imageName: "myapp:latest",
			want:      true,
		},
		{
			name:      "bare name without tag is local",
			imageName: "myapp",
			want:      true,
		},
		{
			name:      "bare name with version tag is local",
			imageName: "myapp:v1.2.3",
			want:      true,
		},
		{
			name:      "ghcr.io registry is not local",
			imageName: "ghcr.io/org/myapp:latest",
			want:      false,
		},
		{
			name:      "docker.io official image is not local",
			imageName: "docker.io/library/nginx:latest",
			want:      false,
		},
		{
			name:      "quay.io registry is not local",
			imageName: "quay.io/org/myapp:v1",
			want:      false,
		},
		{
			name:      "localhost registry is not local",
			imageName: "localhost:5000/myapp:latest",
			want:      false,
		},
		{
			name:      "org slash repo is not local",
			imageName: "myorg/myapp:latest",
			want:      false,
		},
		{
			name:      "empty string is not local",
			imageName: "",
			want:      false,
		},
		{
			name:      "single word no tag",
			imageName: "nginx",
			want:      true,
		},
		{
			name:      "name with port-like tag but no slash",
			imageName: "myapp:8080",
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLocalImage(tt.imageName)
			if got != tt.want {
				t.Errorf("isLocalImage(%q) = %v, want %v", tt.imageName, got, tt.want)
			}
		})
	}
}
