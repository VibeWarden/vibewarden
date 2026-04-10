// Package ssh — white-box tests for internal rsync argument builders.
package ssh

import (
	"strings"
	"testing"
)

func TestRsyncArgs_DirectoryTrailingSlash(t *testing.T) {
	e := NewExecutor(Target{User: "ubuntu", Host: "10.0.0.1"})

	tests := []struct {
		name          string
		localDir      string
		wantSrcSuffix string
	}{
		{"no trailing slash", "/home/user/project", "/home/user/project/"},
		{"with trailing slash", "/home/user/project/", "/home/user/project/"},
		{"nested path", "/a/b/c", "/a/b/c/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := e.rsyncArgs(tt.localDir, "~/vibewarden/project/", false)
			// The source (second-to-last arg) must end with "/".
			src := args[len(args)-2]
			if src != tt.wantSrcSuffix {
				t.Errorf("rsyncArgs source = %q, want %q", src, tt.wantSrcSuffix)
			}
		})
	}
}

func TestRsyncFileArgs_NoTrailingSlash(t *testing.T) {
	e := NewExecutor(Target{User: "ubuntu", Host: "10.0.0.1"})

	tests := []struct {
		name       string
		localFile  string
		remotePath string
	}{
		{
			name:       "yaml config file",
			localFile:  "/home/user/myproject/vibewarden.yaml",
			remotePath: "~/vibewarden/myproject/vibewarden.yaml",
		},
		{
			name:       "file with no directory component",
			localFile:  "vibewarden.yaml",
			remotePath: "~/vibewarden/p/vibewarden.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := e.rsyncFileArgs(tt.localFile, tt.remotePath)

			// Source (second-to-last arg) must equal localFile exactly — no
			// trailing slash appended.
			src := args[len(args)-2]
			if src != tt.localFile {
				t.Errorf("rsyncFileArgs source = %q, want %q", src, tt.localFile)
			}
			if strings.HasSuffix(src, "/") {
				t.Errorf("rsyncFileArgs source must not end with '/', got %q", src)
			}

			// Destination (last arg) must be user@host:remotePath.
			dst := args[len(args)-1]
			wantDst := "ubuntu@10.0.0.1:" + tt.remotePath
			if dst != wantDst {
				t.Errorf("rsyncFileArgs destination = %q, want %q", dst, wantDst)
			}
		})
	}
}

func TestRsyncFileArgs_WithPort(t *testing.T) {
	e := NewExecutor(Target{User: "deploy", Host: "myserver.example.com", Port: 2222})

	args := e.rsyncFileArgs("/local/vibewarden.yaml", "~/vibewarden/p/vibewarden.yaml")

	// Find the -e flag value and confirm it contains the port.
	found := false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) {
			if strings.Contains(args[i+1], "-p 2222") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected '-p 2222' in ssh command within rsync args, got: %v", args)
	}
}

func TestSshArgs_WithKey(t *testing.T) {
	e := NewExecutorWithKey(Target{User: "ubuntu", Host: "10.0.0.1"}, "/home/user/.ssh/deploy_key")

	args := e.sshArgs("echo hello")

	// Confirm -i <keyPath> appears in the argument list.
	found := false
	for i, a := range args {
		if a == "-i" && i+1 < len(args) && args[i+1] == "/home/user/.ssh/deploy_key" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected '-i /home/user/.ssh/deploy_key' in ssh args, got: %v", args)
	}
}

func TestSshArgs_WithoutKey(t *testing.T) {
	e := NewExecutor(Target{User: "ubuntu", Host: "10.0.0.1"})

	args := e.sshArgs("echo hello")

	// Confirm -i does not appear when no key is set.
	for _, a := range args {
		if a == "-i" {
			t.Errorf("did not expect '-i' flag in ssh args without key, got: %v", args)
			break
		}
	}
}

func TestRsyncArgs_WithKey(t *testing.T) {
	e := NewExecutorWithKey(Target{User: "ubuntu", Host: "10.0.0.1"}, "/home/user/.ssh/deploy_key")

	args := e.rsyncArgs("/local/dir", "~/remote/dir/", false)

	// The -e flag value must contain -i <keyPath>.
	found := false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) {
			if strings.Contains(args[i+1], "-i /home/user/.ssh/deploy_key") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected '-i /home/user/.ssh/deploy_key' in rsync -e ssh command, got: %v", args)
	}
}

func TestRsyncFileArgs_WithKey(t *testing.T) {
	e := NewExecutorWithKey(Target{User: "ubuntu", Host: "10.0.0.1"}, "/home/user/.ssh/deploy_key")

	args := e.rsyncFileArgs("/local/file.yaml", "~/remote/file.yaml")

	// The -e flag value must contain -i <keyPath>.
	found := false
	for i, a := range args {
		if a == "-e" && i+1 < len(args) {
			if strings.Contains(args[i+1], "-i /home/user/.ssh/deploy_key") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected '-i /home/user/.ssh/deploy_key' in rsync file -e ssh command, got: %v", args)
	}
}
