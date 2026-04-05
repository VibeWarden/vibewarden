// Package ssh provides a RemoteExecutor that shells out to the system ssh and
// rsync binaries. This means the user's SSH agent, ~/.ssh/config, and any
// ProxyJump rules are honoured automatically, with no Go SSH library required.
package ssh

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
)

// Target holds the parsed components of an ssh:// URL.
type Target struct {
	// User is the remote username (e.g. "ubuntu").
	User string
	// Host is the remote hostname or IP address (e.g. "203.0.113.10").
	Host string
	// Port is the SSH port. When zero the default port 22 is used.
	Port int
}

// ParseTarget parses a target string in ssh://user@host[:port] format.
// The scheme must be "ssh". User and host are required.
func ParseTarget(raw string) (Target, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Target{}, fmt.Errorf("parsing target URL: %w", err)
	}
	if u.Scheme != "ssh" {
		return Target{}, fmt.Errorf("target URL scheme must be ssh, got %q", u.Scheme)
	}
	if u.User == nil || u.User.Username() == "" {
		return Target{}, fmt.Errorf("target URL must include a username (e.g. ssh://user@host)")
	}
	host := u.Hostname()
	if host == "" {
		return Target{}, fmt.Errorf("target URL must include a host (e.g. ssh://user@host)")
	}

	var port int
	if portStr := u.Port(); portStr != "" {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return Target{}, fmt.Errorf("target URL port is not a number: %w", err)
		}
		if port < 1 || port > 65535 {
			return Target{}, fmt.Errorf("target URL port %d is out of range", port)
		}
	}

	return Target{
		User: u.User.Username(),
		Host: host,
		Port: port,
	}, nil
}

// Destination returns user@host as expected by ssh/rsync.
func (t Target) Destination() string {
	return t.User + "@" + t.Host
}

// Executor implements ports.RemoteExecutor by shelling out to the system ssh
// and rsync binaries.
type Executor struct {
	target Target
}

// NewExecutor creates an Executor for the given Target.
func NewExecutor(target Target) *Executor {
	return &Executor{target: target}
}

// Run executes cmd on the remote host via ssh and returns the combined
// stdout+stderr output. A non-zero exit code is wrapped and returned as an
// error that includes the captured output for diagnosis.
func (e *Executor) Run(ctx context.Context, cmd string) (string, error) {
	args := e.sshArgs(cmd)
	//nolint:gosec // cmd is caller-supplied; callers in this codebase use only
	// fixed shell commands (e.g. "which docker"). The linter flag is acceptable
	// here because the alternative (a Go SSH library) is worse for usability.
	c := exec.CommandContext(ctx, "ssh", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return buf.String(), fmt.Errorf("ssh %s: %w\noutput: %s", cmd, err, strings.TrimSpace(buf.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}

// Transfer syncs localDir to remoteDir on the remote host using rsync over SSH.
// When deleteExtra is true, extraneous files in remoteDir are removed.
func (e *Executor) Transfer(ctx context.Context, localDir, remoteDir string, deleteExtra bool) error {
	args := e.rsyncArgs(localDir, remoteDir, deleteExtra)
	//nolint:gosec // localDir is constructed internally from config paths; remoteDir
	// is a fixed pattern (~/vibewarden/<project>/). Safe in this context.
	c := exec.CommandContext(ctx, "rsync", args...)

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	if err := c.Run(); err != nil {
		return fmt.Errorf("rsync %s → %s:%s: %w\noutput: %s",
			localDir, e.target.Destination(), remoteDir, err, strings.TrimSpace(buf.String()))
	}
	return nil
}

// sshArgs builds the ssh argument list for the given command.
func (e *Executor) sshArgs(cmd string) []string {
	args := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "BatchMode=yes",
	}
	if e.target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(e.target.Port))
	}
	args = append(args, e.target.Destination(), cmd)
	return args
}

// rsyncArgs builds the rsync argument list.
func (e *Executor) rsyncArgs(localDir, remoteDir string, deleteExtra bool) []string {
	// Build the ssh command string used as rsync's transport.
	sshCmd := "ssh -o StrictHostKeyChecking=accept-new -o BatchMode=yes"
	if e.target.Port != 0 {
		sshCmd += " -p " + strconv.Itoa(e.target.Port)
	}

	args := []string{
		"-az",
		"--progress",
		"-e", sshCmd,
	}
	if deleteExtra {
		args = append(args, "--delete")
	}
	// Ensure localDir ends with "/" so rsync syncs the contents, not the
	// directory name itself.
	src := strings.TrimSuffix(localDir, "/") + "/"
	dst := e.target.Destination() + ":" + remoteDir
	args = append(args, src, dst)
	return args
}
