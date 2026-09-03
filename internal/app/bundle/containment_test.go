package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainsPath(t *testing.T) {
	sep := string(os.PathSeparator)
	root := filepath.Join(sep+"proj", "")

	tests := []struct {
		name     string
		absRoot  string
		resolved string
		want     bool
	}{
		{
			name:     "root itself is contained",
			absRoot:  root,
			resolved: root,
			want:     true,
		},
		{
			name:     "direct child is contained",
			absRoot:  root,
			resolved: filepath.Join(root, "main.go"),
			want:     true,
		},
		{
			name:     "nested descendant is contained",
			absRoot:  root,
			resolved: filepath.Join(root, "internal", "app", "x.go"),
			want:     true,
		},
		{
			name:     "sibling with extended name is not contained",
			absRoot:  root,
			resolved: sep + "proj-secret",
			want:     false,
		},
		{
			name:     "file inside sibling with extended name is not contained",
			absRoot:  root,
			resolved: filepath.Join(sep+"proj-secret", "id_rsa"),
			want:     false,
		},
		{
			name:     "unrelated absolute path is not contained",
			absRoot:  root,
			resolved: filepath.Join(sep+"etc", "passwd"),
			want:     false,
		},
		{
			name:     "parent of root is not contained",
			absRoot:  filepath.Join(sep+"proj", "sub"),
			resolved: root,
			want:     false,
		},
		{
			name:     "empty root contains nothing",
			absRoot:  "",
			resolved: root,
			want:     false,
		},
		{
			name:     "empty resolved is not contained",
			absRoot:  root,
			resolved: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsPath(tt.absRoot, tt.resolved); got != tt.want {
				t.Errorf("containsPath(%q, %q) = %v, want %v", tt.absRoot, tt.resolved, got, tt.want)
			}
		})
	}
}
